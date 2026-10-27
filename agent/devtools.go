package agent

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"time"

	"github.com/qizhanchan/qui"
)

// Chrome DevTools frontend compatibility. The frontend is a CDP client: its
// Elements, Styles, Computed and Accessibility panes and the screencast run
// on DOM / CSS / Overlay / Accessibility / Page, which qui implements. What
// it additionally sends on connect — Network.enable, Debugger.enable, … —
// gets an empty success here, so the panes it can use come up instead of
// the connection stalling on an error. There is no JavaScript: Console
// evaluation answers with a pointer to the qui equivalents.

// devtoolsResults are fixed answers for methods the frontend needs a
// specific shape from.
var devtoolsResults = map[string]any{
	"Debugger.enable":               map[string]any{"debuggerId": "qui"},
	"Page.getResourceTree":          nil, // filled per call (needs the target)
	"Page.getFrameTree":             nil,
	"Page.getNavigationHistory":     nil,
	"CSS.getPlatformFontsForNode":   map[string]any{"fonts": []any{}},
	"CSS.getInlineStylesForNode":    map[string]any{},
	"CSS.getBackgroundColors":       map[string]any{"backgroundColors": []any{}},
	"CSS.getMediaQueries":           map[string]any{"medias": []any{}},
	"DOM.getNodesForSubtreeByStyle": map[string]any{"nodeIds": []any{}},
	"Runtime.getIsolateId":          map[string]any{"id": "qui"},
	"Runtime.getHeapUsage":          map[string]any{"usedSize": 0, "totalSize": 0},
	"Storage.getStorageKeyForFrame": map[string]any{"storageKey": "qui://"},
	"Network.getCookies":            map[string]any{"cookies": []any{}},
}

// devtoolsStub answers frontend housekeeping qui does not implement (it is
// consulted only after the registry has no such method): fixed results
// above, and an empty success for enable / disable / set* / start* / stop*
// calls. handled is false for anything else, which then reports
// method-not-found as usual.
func devtoolsStub(method string) (result any, handled bool) {
	if r, ok := devtoolsResults[method]; ok && r != nil {
		return r, true
	}
	_, name, _ := strings.Cut(method, ".")
	for _, p := range []string{"enable", "disable", "set", "start", "stop", "clear", "runIfWaitingForDebugger", "addBinding", "discard", "track", "untrack", "requestEntries"} {
		if strings.HasPrefix(name, p) {
			return struct{}{}, true
		}
	}
	return nil, false
}

// devtoolsTargetResult builds the per-target answers in devtoolsResults.
func devtoolsTargetResult(method string, w *qui.Window) (any, bool) {
	frame := map[string]any{
		"id": w.TargetID(), "loaderId": w.TargetID(), "url": "qui://" + w.TargetID(),
		"domainAndRegistry": "", "securityOrigin": "qui://", "mimeType": "text/html",
		"secureContextType": "Secure", "crossOriginIsolatedContextType": "NotIsolated", "gatedAPIFeatures": []any{},
	}
	switch method {
	case "Page.getResourceTree":
		return map[string]any{"frameTree": map[string]any{"frame": frame, "resources": []any{}}}, true
	case "Page.getFrameTree":
		return map[string]any{"frameTree": map[string]any{"frame": frame}}, true
	case "Page.getNavigationHistory":
		return map[string]any{"currentIndex": 0, "entries": []any{map[string]any{
			"id": 0, "url": frame["url"], "userTypedURL": frame["url"], "title": w.Title(), "transitionType": "typed"}}}, true
	}
	return nil, false
}

// afterEnable runs once a session enabled domain: Runtime announces the
// (JavaScript-less) execution context the frontend's Console expects.
func afterEnable(sess *session, domain string, w *qui.Window) {
	if domain != "Runtime" || w == nil {
		return
	}
	sess.Emit("Runtime.executionContextCreated", map[string]any{"context": map[string]any{
		"id": 1, "origin": "qui://", "name": "qui (no JavaScript)", "uniqueId": "qui-" + w.TargetID(),
		"auxData": map[string]any{"isDefault": true, "type": "default", "frameId": w.TargetID()},
	}})
}

// -------------------------------------------------------------------
// Commands added to root's domains for DevTools: Runtime.evaluate (no JS),
// DOM.pushNodesByBackendIdsToFrontend, and the Page screencast.

func init() {
	extend := func(domain string, cmds ...qui.ProtocolCommand) {
		d, ok := qui.LookupProtocolDomain(domain)
		if !ok {
			return
		}
		d.Commands = append(d.Commands, cmds...)
		qui.RegisterProtocolDomain(d)
	}
	extend("Runtime", qui.NewProtocolCommand("evaluate", "qui runs no JavaScript. Answers DevTools' Console with where to look instead.",
		func(ctx *qui.ProtocolContext, p struct {
			Expression string `json:"expression"`
		}) (map[string]any, error) {
			msg := "qui has no JavaScript runtime. Use Agent.snapshot, DOM.querySelectorAll, CSS.getNodeStyles or Reactive.getComponentTree."
			return map[string]any{
				"result":           map[string]any{"type": "string", "value": msg},
				"exceptionDetails": map[string]any{"exceptionId": 1, "text": msg, "lineNumber": 0, "columnNumber": 0},
			}, nil
		}).WithAppLevel())
	extend("DOM", qui.NewProtocolCommand("pushNodesByBackendIdsToFrontend", "CDP compatibility: nodeId == backendNodeId in qui.",
		func(ctx *qui.ProtocolContext, p struct {
			BackendNodeIDs []qui.NodeID `json:"backendNodeIds"`
		}) (map[string]any, error) {
			if p.BackendNodeIDs == nil {
				p.BackendNodeIDs = []qui.NodeID{}
			}
			return map[string]any{"nodeIds": p.BackendNodeIDs}, nil
		}),
		qui.NewProtocolCommand("setInspectedNode", "CDP compatibility; no-op.",
			func(*qui.ProtocolContext, struct{}) (struct{}, error) { return struct{}{}, nil }))
	extend("Page",
		qui.NewProtocolCommand("startScreencast", "Stream frames as Page.screencastFrame events, one per painted frame after each ack (screencastFrameAck).",
			startScreencast),
		qui.NewProtocolCommand("stopScreencast", "Stop the screencast.",
			func(ctx *qui.ProtocolContext, _ struct{}) (struct{}, error) {
				_, sess := hostOf(ctx)
				if sess != nil {
					sess.mu.Lock()
					sess.cast = nil
					sess.mu.Unlock()
				}
				return struct{}{}, nil
			}),
		qui.NewProtocolCommand("screencastFrameAck", "Acknowledge a frame; the next one follows the next paint.",
			func(ctx *qui.ProtocolContext, p struct {
				SessionID int `json:"sessionId"`
			}) (struct{}, error) {
				_, sess := hostOf(ctx)
				if sess != nil {
					sess.mu.Lock()
					if c := sess.cast; c != nil && c.seq == p.SessionID {
						c.awaiting = false
						if c.pending {
							c.pending = false
							go sess.castFrame(ctx.Window, c)
						}
					}
					sess.mu.Unlock()
				}
				return struct{}{}, nil
			}))
}

// screencast is a session's screencast state.
type screencast struct {
	format   string
	quality  int
	scale    float64
	seq      int
	awaiting bool // a frame is out and not yet acked
	pending  bool // a paint happened while awaiting
}

type screencastParams struct {
	Format    string `json:"format,omitempty" desc:"jpeg (default) | png"`
	Quality   int    `json:"quality,omitempty"`
	MaxWidth  int    `json:"maxWidth,omitempty"`
	MaxHeight int    `json:"maxHeight,omitempty"`
}

var castMu sync.Mutex

func startScreencast(ctx *qui.ProtocolContext, p screencastParams) (struct{}, error) {
	_, sess := hostOf(ctx)
	if sess == nil || sess.conn == nil {
		return struct{}{}, errNoTransport("Page.startScreencast")
	}
	if p.Format == "" {
		p.Format = "jpeg"
	}
	scale := 1.0
	var m struct {
		CSSLayoutViewport struct{ ClientWidth, ClientHeight float64 } `json:"cssLayoutViewport"`
	}
	if raw, err := callJSON(ctx, "Page.getLayoutMetrics", nil); err == nil {
		_ = json.Unmarshal(raw, &m)
		w, h := m.CSSLayoutViewport.ClientWidth, m.CSSLayoutViewport.ClientHeight
		if p.MaxWidth > 0 && w > float64(p.MaxWidth) {
			scale = float64(p.MaxWidth) / w
		}
		if p.MaxHeight > 0 && h*scale > float64(p.MaxHeight) {
			scale = float64(p.MaxHeight) / h
		}
	}
	c := &screencast{format: p.Format, quality: p.Quality, scale: scale}
	sess.mu.Lock()
	sess.cast = c
	sess.mu.Unlock()
	sess.setEnabled("Page", true) // frameCommitted drives the frames
	go sess.castFrame(ctx.Window, c)
	return struct{}{}, nil
}

// onFrame is called for a session's Page.frameCommitted.
func (s *session) onFrame(w *qui.Window) {
	s.mu.Lock()
	c := s.cast
	if c == nil {
		s.mu.Unlock()
		return
	}
	if c.awaiting {
		c.pending = true
		s.mu.Unlock()
		return
	}
	s.mu.Unlock()
	go s.castFrame(w, c)
}

func (s *session) castFrame(w *qui.Window, c *screencast) {
	castMu.Lock()
	defer castMu.Unlock()
	s.mu.Lock()
	if s.cast != c || c.awaiting {
		s.mu.Unlock()
		return
	}
	c.awaiting = true
	c.seq++
	seq := c.seq
	s.mu.Unlock()
	pctx := &qui.ProtocolContext{Context: context.Background(), Window: w, Session: s}
	raw, err := callJSON(pctx, "Page.captureScreenshot", map[string]any{"format": c.format, "quality": c.quality, "scale": c.scale})
	if err != nil {
		s.mu.Lock()
		c.awaiting = false
		s.mu.Unlock()
		return
	}
	var shot struct {
		Data          string
		Width, Height int
	}
	_ = json.Unmarshal(raw, &shot)
	s.Emit("Page.screencastFrame", map[string]any{
		"data":      shot.Data,
		"sessionId": seq,
		"metadata": map[string]any{
			"offsetTop": 0, "pageScaleFactor": 1, "scrollOffsetX": 0, "scrollOffsetY": 0,
			"deviceWidth": float64(shot.Width) / c.scale, "deviceHeight": float64(shot.Height) / c.scale,
			"timestamp": float64(time.Now().UnixNano()) / 1e9,
		},
	})
}

// callJSON runs another registered command in the same context.
func callJSON(ctx *qui.ProtocolContext, method string, params any) (json.RawMessage, error) {
	cmd, ok := qui.LookupProtocolCommand(method)
	if !ok {
		return nil, qui.NewProtocolError("unsupported", "no %s", method)
	}
	raw, _ := json.Marshal(params)
	res, err := cmd.Call(ctx, raw)
	if err != nil {
		return nil, err
	}
	return json.Marshal(res)
}
