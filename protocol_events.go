package qui

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"runtime/debug"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// Engine-produced protocol events: Log (application logging), Runtime
// (panics), Page (frames, resizes), DOM (structure changed) and Overlay (the
// on-window inspector). Each producer checks ProtocolEventsWanted first, so
// with no agent attached none of this costs more than an atomic load.

// -------------------------------------------------------------------
// Log

type logEntry struct {
	Source    string         `json:"source" desc:"app (slog / log output routed through qui)"`
	Level     string         `json:"level" desc:"verbose | info | warning | error"`
	Text      string         `json:"text"`
	Timestamp float64        `json:"timestamp" desc:"unix milliseconds"`
	Attrs     map[string]any `json:"attrs,omitempty" desc:"slog attributes"`
}

type logEntryAdded struct {
	Entry logEntry `json:"entry"`
}

// LogHandler returns a slog.Handler that publishes records as
// Log.entryAdded protocol events, forwarding each to next as well (nil
// drops them after publishing). qui never replaces the global logger; an
// app opts in with
//
//	slog.SetDefault(slog.New(qui.LogHandler(slog.Default().Handler())))
func LogHandler(next slog.Handler) slog.Handler {
	return &logHandler{next: next}
}

type logHandler struct {
	next  slog.Handler
	attrs []slog.Attr
	group string
}

func (h *logHandler) Enabled(ctx context.Context, l slog.Level) bool {
	if ProtocolEventsWanted(nil, "Log") {
		return true
	}
	return h.next != nil && h.next.Enabled(ctx, l)
}

func (h *logHandler) Handle(ctx context.Context, r slog.Record) error {
	if ProtocolEventsWanted(nil, "Log") {
		attrs := map[string]any{}
		add := func(a slog.Attr) bool {
			key := a.Key
			if h.group != "" {
				key = h.group + "." + key
			}
			attrs[key] = a.Value.Resolve().Any()
			return true
		}
		for _, a := range h.attrs {
			add(a)
		}
		r.Attrs(add)
		if len(attrs) == 0 {
			attrs = nil
		}
		EmitProtocolEvent(nil, "Log.entryAdded", logEntryAdded{Entry: logEntry{
			Source: "app", Level: logLevel(r.Level), Text: r.Message,
			Timestamp: float64(r.Time.UnixNano()) / 1e6, Attrs: attrs,
		}})
	}
	if h.next != nil && h.next.Enabled(ctx, r.Level) {
		return h.next.Handle(ctx, r)
	}
	return nil
}

func (h *logHandler) WithAttrs(as []slog.Attr) slog.Handler {
	c := *h
	c.attrs = append(append([]slog.Attr(nil), h.attrs...), as...)
	if h.next != nil {
		c.next = h.next.WithAttrs(as)
	}
	return &c
}

func (h *logHandler) WithGroup(name string) slog.Handler {
	c := *h
	if c.group != "" {
		c.group += "." + name
	} else {
		c.group = name
	}
	if h.next != nil {
		c.next = h.next.WithGroup(name)
	}
	return &c
}

func logLevel(l slog.Level) string {
	switch {
	case l >= slog.LevelError:
		return "error"
	case l >= slog.LevelWarn:
		return "warning"
	case l >= slog.LevelInfo:
		return "info"
	}
	return "verbose"
}

// LogWriter returns an io.Writer that publishes each line as an info-level
// Log.entryAdded, for the standard log package:
//
//	log.SetOutput(io.MultiWriter(os.Stderr, qui.LogWriter()))
func LogWriter() io.Writer { return logWriter{} }

type logWriter struct{}

func (logWriter) Write(p []byte) (int, error) {
	if ProtocolEventsWanted(nil, "Log") {
		for _, line := range strings.Split(strings.TrimRight(string(p), "\n"), "\n") {
			EmitProtocolEvent(nil, "Log.entryAdded", logEntryAdded{Entry: logEntry{
				Source: "app", Level: "info", Text: line, Timestamp: float64(time.Now().UnixNano()) / 1e6,
			}})
		}
	}
	return len(p), nil
}

func logDomain() ProtocolDomain {
	return ProtocolDomain{
		Name:        "Log",
		Description: "Application log output routed through qui.LogHandler (slog) or qui.LogWriter (log). Always buffered: read with Target.getEvents, or Log.enable on a session.",
		Buffered:    true,
		Events: []ProtocolEvent{
			NewProtocolEvent[logEntryAdded]("entryAdded", "One log record."),
		},
	}
}

// -------------------------------------------------------------------
// Runtime: panics

// survivePanics makes the frame loop and event dispatch swallow a panic
// after reporting it (Debug.setFlags {survivePanics: true}) instead of
// crashing, so an agent can keep inspecting the app that just failed.
var survivePanics atomic.Bool

type exceptionDetails struct {
	Text       string `json:"text"`
	Where      string `json:"where" desc:"frame | dispatch | job"`
	Exception  string `json:"exception" desc:"the panic value"`
	StackTrace string `json:"stackTrace" desc:"Go stack of the panicking goroutine"`
	Survived   bool   `json:"survived" desc:"true when Debug.survivePanics kept the app running"`
}

type exceptionThrown struct {
	Timestamp        float64          `json:"timestamp" desc:"unix milliseconds"`
	ExceptionDetails exceptionDetails `json:"exceptionDetails"`
}

var (
	lastPanicMu  sync.Mutex
	lastPanicKey string
)

// reportPanic is deferred around the frame and around dispatch. With an
// agent watching it publishes Runtime.exceptionThrown before the panic
// continues — the deferred call runs on top of the panicking stack, so the
// re-panic keeps the original frames in its trace — or, with survivePanics
// set, ends it.
func (w *Window) reportPanic(where string) {
	if !ProtocolEventsWanted(w, "Runtime") && !survivePanics.Load() {
		return // not recovering at all: the panic propagates untouched
	}
	v := recover()
	if v == nil {
		return
	}
	stack := string(debug.Stack())
	survive := survivePanics.Load()
	key := fmt.Sprint(v)
	lastPanicMu.Lock()
	dup := key == lastPanicKey
	lastPanicKey = key
	lastPanicMu.Unlock()
	if !dup && ProtocolEventsWanted(w, "Runtime") {
		EmitProtocolEvent(w, "Runtime.exceptionThrown", exceptionThrown{
			Timestamp: float64(time.Now().UnixNano()) / 1e6,
			ExceptionDetails: exceptionDetails{
				Text: "panic: " + key, Where: where, Exception: key, StackTrace: stack, Survived: survive,
			},
		})
		flushProtocolSinks(500 * time.Millisecond)
	}
	if !survive {
		panic(v)
	}
	lastPanicMu.Lock()
	lastPanicKey = ""
	lastPanicMu.Unlock()
}

// ProtocolEventFlusher is implemented by sinks that deliver asynchronously;
// Flush waits up to timeout for queued events to go out. Called before a
// reported panic takes the process down.
type ProtocolEventFlusher interface {
	Flush(timeout time.Duration)
}

func flushProtocolSinks(timeout time.Duration) {
	sinkMu.RLock()
	list := append([]ProtocolEventSink(nil), sinks...)
	sinkMu.RUnlock()
	for _, s := range list {
		if f, ok := s.(ProtocolEventFlusher); ok {
			f.Flush(timeout)
		}
	}
}

func runtimeDomain() ProtocolDomain {
	return ProtocolDomain{
		Name:        "Runtime",
		Description: "Go panics in the frame loop, event dispatch and jobs. No JavaScript: qui has no evaluate.",
		Buffered:    true,
		Events: []ProtocolEvent{
			NewProtocolEvent[exceptionThrown]("exceptionThrown", "A panic, with its Go stack. Sent before the process exits — or, with Debug.setFlags {survivePanics:true}, instead of exiting."),
		},
	}
}

// -------------------------------------------------------------------
// Page / DOM frame hooks

// afterPaintProtocol runs at the end of a painted frame.
func (w *Window) afterPaintProtocol(dirty Rect, full bool) {
	w.frameSeq++
	if ProtocolEventsWanted(w, "Page") {
		EmitProtocolEvent(w, "Page.frameCommitted", pageFrameCommitted{
			Frame:       w.frameSeq,
			DirtyRect:   pageRect{X: dirty.X, Y: dirty.Y, Width: dirty.W, Height: dirty.H},
			FullRepaint: full,
		})
	}
	if ProtocolEventsWanted(w, "DOM") {
		h := w.AccessibilityTree().Hash()
		if w.lastDOMHash != "" && h != w.lastDOMHash {
			EmitProtocolEvent(w, "DOM.documentUpdated", empty{})
		}
		w.lastDOMHash = h
	} else {
		w.lastDOMHash = ""
	}
}

// fullRepaintClip reports whether a frame's clip covered the whole viewport.
func fullRepaintClip(clip Rect, viewport Size) bool {
	return clip.X <= 0 && clip.Y <= 0 && clip.X+clip.W >= viewport.W && clip.Y+clip.H >= viewport.H
}

func (w *Window) noteResizeProtocol(size Size) {
	if ProtocolEventsWanted(w, "Page") {
		EmitProtocolEvent(w, "Page.windowResized", pageWindowResized{Width: size.W, Height: size.H})
	}
}

// -------------------------------------------------------------------
// Overlay: highlight + inspect mode

type overlayHighlight struct {
	id     NodeID
	widget Widget // the node's widget (the host, for a self-drawn child)
	rect   Rect   // bounds at highlight time, for self-drawn children
	label  string
}

func (w *Window) setHighlight(n *AXNode) {
	old := w.highlight
	if n == nil {
		w.highlight = nil
	} else {
		label := n.NodeID.String() + " " + n.Role
		if n.Name != "" {
			label += " " + fmt.Sprintf("%q", n.Name)
		}
		label += fmt.Sprintf(" %.0f×%.0f", n.Bounds.W, n.Bounds.H)
		w.highlight = &overlayHighlight{id: n.NodeID, widget: n.widget,
			rect: Rect{X: n.Bounds.X, Y: n.Bounds.Y, W: n.Bounds.W, H: n.Bounds.H}, label: label}
	}
	if old != nil {
		w.InvalidateRect(highlightPaintRect(old.currentRect()))
	}
	if w.highlight != nil {
		w.InvalidateRect(highlightPaintRect(w.highlight.currentRect()))
	}
}

func (h *overlayHighlight) currentRect() Rect {
	if h.widget != nil && WidgetNodeID(h.widget) == h.id {
		return InteractionBoundsOf(h.widget)
	}
	return h.rect
}

// highlightPaintRect covers the box and its label.
func highlightPaintRect(r Rect) Rect {
	return Rect{X: r.X - 2, Y: r.Y - 20, W: max(r.W, 240) + 4, H: r.H + 24}
}

func (w *Window) drawHighlight(c Canvas) {
	h := w.highlight
	if h == nil {
		return
	}
	r := h.currentRect()
	c.FillRect(r, Color{R: 0.435, G: 0.659, B: 0.863, A: 0.4})
	c.StrokeRect(r, Color{R: 0.18, G: 0.45, B: 0.85, A: 1}, 1)
	ly := r.Y - 18
	if ly < 0 {
		ly = r.Y + r.H + 2
	}
	c.FillRect(Rect{X: r.X, Y: ly, W: 240, H: 16}, Color{R: 0.1, G: 0.1, B: 0.12, A: 0.85})
	c.DrawText(h.label, Rect{X: r.X + 4, Y: ly + 1, W: 234, H: 14}, ColorWhite, Font{Size: 10})
}

type inspectNodeRequested struct {
	BackendNodeID NodeID `json:"backendNodeId"`
	NodeID        NodeID `json:"nodeId" desc:"qui: same as backendNodeId"`
	Role          string `json:"role"`
	Name          string `json:"name,omitempty"`
}

// handleInspectEvent consumes pointer input while inspect mode is on:
// moving highlights the node under the pointer, clicking picks it (emits
// Overlay.inspectNodeRequested and leaves the mode), Escape cancels.
func (w *Window) handleInspectEvent(ev Event) bool {
	if !w.inspectMode {
		// The release of the picking click belongs to the picker too.
		if me, ok := ev.(MouseEvent); ok && w.inspectEatUp && me.eventType == EventMouseUp {
			w.inspectEatUp = false
			return true
		}
		return false
	}
	switch e := ev.(type) {
	case MouseEvent:
		n := w.nodeAt(Point{X: e.X, Y: e.Y})
		switch e.eventType {
		case EventMouseMove:
			if n == nil || w.highlight == nil || w.highlight.id != n.NodeID {
				w.setHighlight(n)
			}
		case EventMouseDown:
			if n != nil {
				EmitProtocolEvent(w, "Overlay.inspectNodeRequested", inspectNodeRequested{
					BackendNodeID: n.NodeID, NodeID: n.NodeID, Role: n.Role, Name: n.Name,
				})
			}
			w.inspectMode = false
			w.inspectEatUp = true
		}
		return true
	case KeyEvent:
		if e.Key == KeyEscape {
			w.inspectMode = false
			w.setHighlight(nil)
			return true
		}
	}
	return false
}

// nodeAt is the innermost AX node under p: self-drawn children first, then
// the deepest widget node.
func (w *Window) nodeAt(p Point) *AXNode {
	tree := w.AccessibilityTree()
	var best *AXNode
	var walk func(n *AXNode)
	walk = func(n *AXNode) {
		if !n.Visible {
			return
		}
		if (Rect{X: n.Bounds.X, Y: n.Bounds.Y, W: n.Bounds.W, H: n.Bounds.H}).Contains(p) {
			best = n
		}
		for _, c := range n.Children {
			walk(c)
		}
	}
	roots, _ := axRoots(tree, 0)
	for _, r := range roots {
		walk(r)
	}
	return best
}

type overlayHighlightParams struct {
	NodeID        NodeID `json:"nodeId,omitempty"`
	BackendNodeID NodeID `json:"backendNodeId,omitempty"`
	Selector      string `json:"selector,omitempty" desc:"qui: highlight the first match instead"`
}

type overlayInspectParams struct {
	Mode string `json:"mode" desc:"searchForNode (pick with the mouse) | none"`
}

func overlayDomain() ProtocolDomain {
	return ProtocolDomain{
		Name: "Overlay",
		Description: "Draw on the window: highlight a node, or let the user point at one. setInspectMode searchForNode turns the pointer into a picker — " +
			"the user clicks the thing that is wrong and the agent receives its nodeId.",
		Buffered: true,
		Commands: []ProtocolCommand{
			NewProtocolCommand("highlightNode", "Outline a node with its @ref, role and size until hideHighlight.",
				func(ctx *ProtocolContext, p overlayHighlightParams) (empty, error) {
					w := ctx.Window
					return empty{}, w.synchronously(func() error {
						var n *AXNode
						var err error
						switch {
						case p.Selector != "":
							var nodes []*AXNode
							if nodes, err = w.FindNodes(p.Selector); err == nil {
								if len(nodes) == 0 {
									err = noMatchFor(p.Selector)
								} else {
									n = nodes[0]
								}
							}
						default:
							id := p.NodeID
							if id == 0 {
								id = p.BackendNodeID
							}
							n, err = w.NodeByID(id)
						}
						if err != nil {
							return err
						}
						w.setHighlight(n)
						return nil
					})
				}),
			NewProtocolCommand("hideHighlight", "Remove the highlight.",
				func(ctx *ProtocolContext, _ empty) (empty, error) {
					return empty{}, ctx.Window.synchronously(func() error { ctx.Window.setHighlight(nil); return nil })
				}),
			NewProtocolCommand("setInspectMode", "searchForNode: pointer moves highlight, a click emits inspectNodeRequested (the click does not reach the app) and ends the mode; Escape cancels. none: leave the mode.",
				func(ctx *ProtocolContext, p overlayInspectParams) (empty, error) {
					w := ctx.Window
					on := p.Mode == "searchForNode" || p.Mode == "searchForUAShadowDOM"
					if !on && p.Mode != "none" && p.Mode != "" {
						return empty{}, &ProtocolError{Code: ProtocolInvalidParams, Reason: "invalid_params", Message: "mode must be searchForNode or none"}
					}
					return empty{}, w.synchronously(func() error {
						w.inspectMode = on
						if !on {
							w.setHighlight(nil)
						}
						return nil
					})
				}),
		},
		Events: []ProtocolEvent{
			NewProtocolEvent[inspectNodeRequested]("inspectNodeRequested", "The user clicked a node in inspect mode."),
		},
	}
}
