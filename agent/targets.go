package agent

import (
	"fmt"
	"runtime/debug"
	"strings"

	"github.com/qizhanchan/qui"
)

// The transport-level domains: Target (windows, sessions, the event
// buffer), Browser and Schema. They live here rather than in root because
// they describe the server's own sessions and targets.

func init() {
	qui.RegisterProtocolDomain(targetDomain())
	qui.RegisterProtocolDomain(browserDomain())
	qui.RegisterProtocolDomain(schemaDomain())
}

func hostOf(ctx *qui.ProtocolContext) (*Server, *session) {
	sess, _ := ctx.Session.(*session)
	if sess == nil {
		return nil, nil
	}
	return sess.server, sess
}

type targetInfo struct {
	TargetID string `json:"targetId"`
	Type     string `json:"type" desc:"page (a window)"`
	Title    string `json:"title"`
	URL      string `json:"url"`
	Attached bool   `json:"attached"`
}

func infoFor(s *Server, w *qui.Window) targetInfo {
	id := w.TargetID()
	attached := false
	for _, sess := range s.liveSessions() {
		if sess.targetID == id {
			attached = true
			break
		}
	}
	return targetInfo{TargetID: id, Type: "page", Title: w.Title(), URL: "qui://" + id, Attached: attached}
}

// announceTarget buffers a Target lifecycle event and sends it to the
// sessions that asked for target discovery.
func (s *Server) announceTarget(method string, w *qui.Window) {
	var params any
	if method == "Target.targetDestroyed" {
		params = map[string]string{"targetId": w.TargetID()}
	} else {
		params = map[string]any{"targetInfo": infoFor(s, w)}
	}
	s.events.push(w.TargetID(), method, params)
	for _, sess := range s.liveSessions() {
		sess.mu.Lock()
		on := sess.discover
		sess.mu.Unlock()
		if on {
			sess.Emit(method, params)
		}
	}
}

type targetIDParams struct {
	TargetID string `json:"targetId,omitempty"`
}

type targetInfosResult struct {
	TargetInfos []targetInfo `json:"targetInfos"`
}

type targetInfoResult struct {
	TargetInfo targetInfo `json:"targetInfo"`
}

type attachParams struct {
	TargetID string `json:"targetId"`
	Flatten  bool   `json:"flatten,omitempty" desc:"accepted for CDP compatibility; sessions are always flat"`
}

type sessionIDResult struct {
	SessionID string `json:"sessionId"`
}

type detachParams struct {
	SessionID string `json:"sessionId"`
}

type discoverParams struct {
	Discover bool `json:"discover"`
}

type getEventsParams struct {
	Since    int64    `json:"since,omitempty" desc:"return events after this seq (the previous call's lastSeq)"`
	Domains  []string `json:"domains,omitempty" desc:"e.g. [\"Log\", \"Runtime\"]; empty = all"`
	TargetID string   `json:"targetId,omitempty" desc:"only this window's events"`
	Limit    int      `json:"limit,omitempty" desc:"the most recent N"`
}

type getEventsResult struct {
	Events  []eventEntry `json:"events"`
	LastSeq int64        `json:"lastSeq"`
}

func errNoTransport(what string) error {
	return qui.NewProtocolError("unsupported", "%s needs a WebSocket session (one-shot HTTP calls have none)", what)
}

func targetDomain() qui.ProtocolDomain {
	return qui.ProtocolDomain{
		Name:        "Target",
		Description: "Windows (targets), sessions, and the event buffer. One window: commands need no target. Several: attach a session, or pass \"target\" per call.",
		Buffered:    true,
		Commands: []qui.ProtocolCommand{
			qui.NewProtocolCommand("getTargets", "Every open window.",
				func(ctx *qui.ProtocolContext, _ struct{}) (targetInfosResult, error) {
					s, _ := hostOf(ctx)
					out := targetInfosResult{TargetInfos: []targetInfo{}}
					for _, w := range s.windows() {
						out.TargetInfos = append(out.TargetInfos, infoFor(s, w))
					}
					return out, nil
				}).WithAppLevel(),
			qui.NewProtocolCommand("getTargetInfo", "One window (default: the session's).",
				func(ctx *qui.ProtocolContext, p targetIDParams) (targetInfoResult, error) {
					s, sess := hostOf(ctx)
					w, err := s.windowFor(sess, p.TargetID)
					if err != nil {
						return targetInfoResult{}, err
					}
					return targetInfoResult{TargetInfo: infoFor(s, w)}, nil
				}).WithAppLevel(),
			qui.NewProtocolCommand("attachToTarget", "Open a session on a window; send its commands with this sessionId.",
				func(ctx *qui.ProtocolContext, p attachParams) (sessionIDResult, error) {
					s, sess := hostOf(ctx)
					if sess.conn == nil {
						return sessionIDResult{}, errNoTransport("attachToTarget")
					}
					if _, err := s.windowFor(sess, p.TargetID); err != nil {
						return sessionIDResult{}, err
					}
					child := sess.conn.attach(p.TargetID)
					return sessionIDResult{SessionID: child.id}, nil
				}).WithAppLevel(),
			qui.NewProtocolCommand("detachFromTarget", "Close a session.",
				func(ctx *qui.ProtocolContext, p detachParams) (struct{}, error) {
					_, sess := hostOf(ctx)
					if sess.conn == nil || !sess.conn.detach(p.SessionID) {
						return struct{}{}, qui.NewProtocolError("target_closed", "no session %q", p.SessionID)
					}
					return struct{}{}, nil
				}).WithAppLevel(),
			qui.NewProtocolCommand("setDiscoverTargets", "Receive targetCreated / targetDestroyed as windows open and close (and targetCreated now for each open one).",
				func(ctx *qui.ProtocolContext, p discoverParams) (struct{}, error) {
					s, sess := hostOf(ctx)
					sess.mu.Lock()
					sess.discover = p.Discover
					sess.mu.Unlock()
					if p.Discover {
						for _, w := range s.windows() {
							sess.Emit("Target.targetCreated", map[string]any{"targetInfo": infoFor(s, w)})
						}
					}
					return struct{}{}, nil
				}).WithAppLevel(),
			qui.NewProtocolCommand("setAutoAttach", "Accepted for CDP compatibility; windows have no child targets.",
				func(ctx *qui.ProtocolContext, _ struct{}) (struct{}, error) { return struct{}{}, nil }).WithAppLevel(),
			qui.NewProtocolCommand("activateTarget", "Raise a window and give it key focus.",
				func(ctx *qui.ProtocolContext, p targetIDParams) (struct{}, error) {
					s, sess := hostOf(ctx)
					w, err := s.windowFor(sess, p.TargetID)
					if err != nil {
						return struct{}{}, err
					}
					return struct{}{}, w.RunSynced(func() error { w.Raise(); return nil })
				}).WithAppLevel(),
			qui.NewProtocolCommand("closeTarget", "Ask a window to close, as its close button does (OnCloseRequest may veto).",
				func(ctx *qui.ProtocolContext, p targetIDParams) (struct{}, error) {
					s, sess := hostOf(ctx)
					w, err := s.windowFor(sess, p.TargetID)
					if err != nil {
						return struct{}{}, err
					}
					return struct{}{}, w.RunSynced(func() error { w.RequestClose(); return nil })
				}).WithAppLevel(),
			qui.NewProtocolCommand("getEvents", "Buffered events since a seq — the polling alternative to a WebSocket subscription. Log, Runtime, Overlay, Target and Agent events are always buffered; others (Page frames, DOM changes) only while a session has their domain enabled.",
				func(ctx *qui.ProtocolContext, p getEventsParams) (getEventsResult, error) {
					s, _ := hostOf(ctx)
					want := map[string]bool{}
					for _, d := range p.Domains {
						want[d] = true
					}
					evs, last := s.events.since(p.Since, func(e eventEntry) bool {
						if p.TargetID != "" && e.TargetID != p.TargetID {
							return false
						}
						d, _, _ := strings.Cut(e.Method, ".")
						return len(want) == 0 || want[d]
					}, p.Limit)
					return getEventsResult{Events: evs, LastSeq: last}, nil
				}).WithAppLevel().WithStrict(),
		},
		Events: []qui.ProtocolEvent{
			qui.NewProtocolEvent[targetInfoResult]("targetCreated", "A window opened."),
			qui.NewProtocolEvent[targetIDParams]("targetDestroyed", "A window closed."),
			qui.NewProtocolEvent[struct {
				Count int64 `json:"count"`
			}]("eventsDropped", "Events were dropped because this client read too slowly."),
		},
	}
}

type versionResult struct {
	ProtocolVersion string `json:"protocolVersion"`
	Product         string `json:"product"`
	Revision        string `json:"revision"`
	UserAgent       string `json:"userAgent"`
	JSVersion       string `json:"jsVersion"`
}

func quiVersion() string {
	if bi, ok := debug.ReadBuildInfo(); ok {
		for _, d := range bi.Deps {
			if d.Path == "github.com/qizhanchan/qui" {
				return d.Version
			}
		}
		if bi.Main.Path == "github.com/qizhanchan/qui" {
			return bi.Main.Version
		}
	}
	return "devel"
}

func versionInfo() versionResult {
	v := quiVersion()
	return versionResult{ProtocolVersion: "1.3", Product: "qui/" + v, Revision: v, UserAgent: "qui/" + v + " (Go)", JSVersion: ""}
}

func browserDomain() qui.ProtocolDomain {
	return qui.ProtocolDomain{
		Name:        "Browser",
		Description: "The process.",
		Commands: []qui.ProtocolCommand{
			qui.NewProtocolCommand("getVersion", "Protocol and qui version.",
				func(*qui.ProtocolContext, struct{}) (versionResult, error) { return versionInfo(), nil }).WithAppLevel(),
		},
	}
}

type schemaDomainInfo struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

func schemaDomain() qui.ProtocolDomain {
	return qui.ProtocolDomain{
		Name:        "Schema",
		Description: "What the protocol offers. The full schema (every command's parameters) is at GET /json/protocol.",
		Commands: []qui.ProtocolCommand{
			qui.NewProtocolCommand("getDomains", "Every domain.",
				func(*qui.ProtocolContext, struct{}) (struct {
					Domains []schemaDomainInfo `json:"domains"`
				}, error) {
					var out struct {
						Domains []schemaDomainInfo `json:"domains"`
					}
					for _, d := range qui.ProtocolDomains() {
						out.Domains = append(out.Domains, schemaDomainInfo{Name: d.Name, Version: "2.0"})
					}
					return out, nil
				}).WithAppLevel(),
		},
	}
}

// devtoolsFrontendURL is the URL that opens Chrome's bundled DevTools on a
// window. The token is a placeholder: the URL gets logged, a secret must
// not.
func (s *Server) devtoolsFrontendURL(hostport, targetID string) string {
	return fmt.Sprintf("devtools://devtools/bundled/inspector.html?ws=%s/devtools/t/$QUI_AGENT_TOKEN/page/%s", hostport, targetID)
}
