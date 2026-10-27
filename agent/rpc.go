package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/qizhanchan/qui"
	"github.com/qizhanchan/qui/agent/internal/ws"
)

// The JSON-RPC layer: message shapes, sessions, dispatch, and event
// routing. Domain logic lives in root's protocol registry; nothing here
// knows a domain by name except the transport-level ones in targets.go.

type rpcRequest struct {
	ID        json.RawMessage `json:"id,omitempty"`
	Method    string          `json:"method"`
	Params    json.RawMessage `json:"params,omitempty"`
	SessionID string          `json:"sessionId,omitempty"`
	// Target picks the window for one call without attaching a session
	// (qui extension; HTTP callers use it, CDP clients attach instead).
	Target string `json:"target,omitempty"`
}

type rpcResponse struct {
	ID        json.RawMessage
	Result    any
	Error     *qui.ProtocolError
	SessionID string
}

func (r rpcResponse) MarshalJSON() ([]byte, error) {
	out := map[string]any{}
	if len(r.ID) > 0 {
		out["id"] = r.ID
	}
	if r.Error != nil {
		out["error"] = r.Error
	} else if r.Result == nil {
		out["result"] = struct{}{}
	} else {
		out["result"] = r.Result
	}
	if r.SessionID != "" {
		out["sessionId"] = r.SessionID
	}
	return json.Marshal(out)
}

type rpcEvent struct {
	Method    string `json:"method"`
	Params    any    `json:"params"`
	SessionID string `json:"sessionId,omitempty"`
}

// session is one protocol client context: a connection's root session, a
// child session from Target.attachToTarget, or the ephemeral session of a
// one-shot HTTP call (conn == nil).
type session struct {
	server   *Server
	conn     *clientConn
	id       string
	targetID string // attached window; "" = none

	mu       sync.Mutex
	enabled  map[string]bool
	discover bool
	cast     *screencast
}

func (s *session) ID() string { return s.id }

// Emit sends an event to this session (dropped for HTTP sessions).
func (s *session) Emit(method string, params any) {
	if s.conn == nil {
		return
	}
	s.conn.send(rpcEvent{Method: method, Params: params, SessionID: s.id})
}

func (s *session) isEnabled(domain string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.enabled[domain]
}

func (s *session) setEnabled(domain string, on bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.enabled == nil {
		s.enabled = map[string]bool{}
	}
	s.enabled[domain] = on
}

// clientConn is one WebSocket client.
type clientConn struct {
	server *Server
	ws     *ws.Conn
	ctx    context.Context
	cancel context.CancelFunc
	out    chan []byte
	drops  atomic.Int64

	mu       sync.Mutex
	root     *session
	children map[string]*session
}

func (c *clientConn) send(v any) {
	b, err := json.Marshal(v)
	if err != nil {
		b, _ = json.Marshal(rpcEvent{Method: "Target.error", Params: map[string]string{"message": err.Error()}})
	}
	select {
	case c.out <- b:
	default:
		// The client is not reading fast enough: drop rather than block
		// the UI goroutine that produced the event.
		c.drops.Add(1)
	}
}

func (c *clientConn) sessions() []*session {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := []*session{c.root}
	for _, s := range c.children {
		out = append(out, s)
	}
	return out
}

func (c *clientConn) session(id string) *session {
	if id == "" {
		return c.root
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.children[id]
}

var sessionSeq atomic.Uint64

func (c *clientConn) attach(targetID string) *session {
	s := &session{server: c.server, conn: c, targetID: targetID,
		id: "S" + strconv.FormatUint(sessionSeq.Add(1), 10)}
	c.mu.Lock()
	if c.children == nil {
		c.children = map[string]*session{}
	}
	c.children[s.id] = s
	c.mu.Unlock()
	return s
}

func (c *clientConn) detach(id string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	if _, ok := c.children[id]; !ok {
		return false
	}
	delete(c.children, id)
	return true
}

// serve runs the connection: a writer goroutine drains out; each request
// runs in its own goroutine, so a long Agent.waitFor does not stall others.
func (c *clientConn) serve() {
	defer c.cancel()
	go func() {
		tick := time.NewTicker(time.Second)
		defer tick.Stop()
		for {
			select {
			case <-c.ctx.Done():
				c.ws.Close()
				return
			case b := <-c.out:
				if err := c.ws.WriteMessage(ws.OpText, b); err != nil {
					c.cancel()
					return
				}
			case <-tick.C:
				if n := c.drops.Swap(0); n > 0 {
					b, _ := json.Marshal(rpcEvent{Method: "Target.eventsDropped", Params: map[string]int64{"count": n}})
					if err := c.ws.WriteMessage(ws.OpText, b); err != nil {
						c.cancel()
						return
					}
				}
			}
		}
	}()
	for {
		_, data, err := c.ws.ReadMessage()
		if err != nil {
			return
		}
		var req rpcRequest
		if err := json.Unmarshal(data, &req); err != nil {
			c.send(rpcResponse{Error: &qui.ProtocolError{Code: qui.ProtocolParseError, Reason: "invalid_params", Message: "bad JSON: " + err.Error()}})
			continue
		}
		sess := c.session(req.SessionID)
		if sess == nil {
			c.send(rpcResponse{ID: req.ID, SessionID: req.SessionID, Error: qui.NewProtocolError("target_closed", "no session %q", req.SessionID)})
			continue
		}
		go func() {
			res, err := c.server.dispatch(c.ctx, sess, req)
			resp := rpcResponse{ID: req.ID, Result: res, SessionID: req.SessionID}
			if err != nil {
				resp.Error = qui.ProtocolErrorFor(err)
			}
			c.send(resp)
		}()
	}
}

// -------------------------------------------------------------------
// Dispatch

// dispatch resolves and runs one request for sess.
func (s *Server) dispatch(ctx context.Context, sess *session, req rpcRequest) (any, error) {
	domain, name, ok := strings.Cut(req.Method, ".")
	if !ok || domain == "" || name == "" {
		return nil, &qui.ProtocolError{Code: qui.ProtocolInvalidRequest, Reason: "invalid_params", Message: fmt.Sprintf("method %q is not Domain.command", req.Method)}
	}
	cmd, found := qui.LookupProtocolCommand(req.Method)
	if !found {
		// Domain.enable / disable: generic subscription switches for any
		// registered domain without its own.
		if _, isDomain := qui.LookupProtocolDomain(domain); isDomain && (name == "enable" || name == "disable") {
			sess.setEnabled(domain, name == "enable")
			if name == "enable" {
				w, _ := s.windowFor(sess, req.Target)
				afterEnable(sess, domain, w)
			}
			return struct{}{}, nil
		}
		if w, err := s.windowFor(sess, req.Target); err == nil {
			if res, ok := devtoolsTargetResult(req.Method, w); ok {
				return res, nil
			}
		}
		if res, handled := devtoolsStub(req.Method); handled {
			return res, nil
		}
		return nil, &qui.ProtocolError{Code: qui.ProtocolMethodNotFound, Reason: "unsupported",
			Message: fmt.Sprintf("'%s' wasn't found", req.Method), Data: map[string]any{"hint": "GET /json/protocol lists every method"}}
	}
	pctx := &qui.ProtocolContext{Context: ctx, Session: sess}
	if !cmd.AppLevel {
		w, err := s.windowFor(sess, req.Target)
		if err != nil {
			return nil, err
		}
		pctx.Window = w
	}
	return cmd.Call(pctx, req.Params)
}

// windowFor picks the target window: the session's attached target, an
// explicit target, or the only window.
func (s *Server) windowFor(sess *session, explicit string) (*qui.Window, error) {
	id := explicit
	if id == "" {
		id = sess.targetID
	}
	wins := s.windows()
	if id != "" {
		for _, w := range wins {
			if w.TargetID() == id {
				return w, nil
			}
		}
		return nil, qui.NewProtocolError("target_closed", "no target %q", id)
	}
	switch len(wins) {
	case 0:
		return nil, qui.NewProtocolError("target_closed", "no window is open")
	case 1:
		return wins[0], nil
	}
	ids := make([]string, 0, len(wins))
	for _, w := range wins {
		ids = append(ids, w.TargetID())
	}
	e := qui.NewProtocolError("ambiguous_target", "%d windows are open; pass \"target\" (or attach a session)", len(wins))
	e.Data = map[string]any{"targets": ids, "hint": "Target.getTargets lists them with titles"}
	return nil, e
}

// -------------------------------------------------------------------
// Events

// eventEntry is one buffered event.
type eventEntry struct {
	Seq      int64  `json:"seq"`
	TS       int64  `json:"ts" desc:"unix milliseconds"`
	TargetID string `json:"targetId,omitempty"`
	Method   string `json:"method"`
	Params   any    `json:"params"`
}

// eventBuffer is a bounded ring of recent events, for polling clients.
type eventBuffer struct {
	mu    sync.Mutex
	cap   int
	items []eventEntry
	seq   int64
}

func (b *eventBuffer) push(targetID, method string, params any) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.seq++
	if len(b.items) >= b.cap {
		copy(b.items, b.items[1:])
		b.items = b.items[:len(b.items)-1]
	}
	b.items = append(b.items, eventEntry{Seq: b.seq, TS: time.Now().UnixMilli(), TargetID: targetID, Method: method, Params: params})
}

func (b *eventBuffer) since(seq int64, filter func(eventEntry) bool, limit int) ([]eventEntry, int64) {
	b.mu.Lock()
	defer b.mu.Unlock()
	out := []eventEntry{}
	for _, e := range b.items {
		if e.Seq > seq && filter(e) {
			out = append(out, e)
		}
	}
	if limit > 0 && len(out) > limit {
		out = out[len(out)-limit:]
	}
	return out, b.seq
}

// WantsEvents implements qui.ProtocolEventSink.
func (s *Server) WantsEvents(w *qui.Window, domain string) bool {
	if !s.running.Load() || !s.servesWindow(w) {
		return false
	}
	if d, ok := qui.LookupProtocolDomain(domain); ok && d.Buffered {
		return true
	}
	for _, sess := range s.liveSessions() {
		if sess.isEnabled(domain) && (w == nil || sess.targetID == "" || sess.targetID == w.TargetID()) {
			return true
		}
	}
	return false
}

// HandleEvent implements qui.ProtocolEventSink: buffer the event and send
// it to every session that enabled its domain for that window.
func (s *Server) HandleEvent(w *qui.Window, method string, params any) {
	if !s.servesWindow(w) {
		return
	}
	domain, _, _ := strings.Cut(method, ".")
	targetID := ""
	if w != nil {
		targetID = w.TargetID()
	}
	s.events.push(targetID, method, params)
	for _, sess := range s.liveSessions() {
		if !sess.isEnabled(domain) {
			continue
		}
		if w != nil && sess.targetID != "" && sess.targetID != targetID {
			continue
		}
		if w != nil && sess.targetID == "" && len(s.windows()) > 1 {
			// An unattached root session only hears its target's events
			// when there is exactly one; with several it must attach.
			continue
		}
		if method == "Page.frameCommitted" {
			sess.onFrame(w)
		}
		sess.Emit(method, params)
	}
}

func (s *Server) liveSessions() []*session {
	s.connsMu.Lock()
	conns := make([]*clientConn, 0, len(s.conns))
	for c := range s.conns {
		conns = append(conns, c)
	}
	s.connsMu.Unlock()
	var out []*session
	for _, c := range conns {
		out = append(out, c.sessions()...)
	}
	return out
}

// servesWindow reports whether events from w belong to this server.
func (s *Server) servesWindow(w *qui.Window) bool {
	if w == nil {
		return true
	}
	for _, x := range s.windows() {
		if x == w {
			return true
		}
	}
	return false
}

// Flush implements qui.ProtocolEventFlusher: wait (bounded) until every
// connection's outbound queue is drained — a reported panic is about to
// take the process down.
func (s *Server) Flush(timeout time.Duration) {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		pending := 0
		s.connsMu.Lock()
		for c := range s.conns {
			pending += len(c.out)
		}
		s.connsMu.Unlock()
		if pending == 0 {
			// The writer may still hold the last message; give it a beat.
			time.Sleep(20 * time.Millisecond)
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
}
