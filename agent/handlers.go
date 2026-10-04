package agent

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"image/png"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/qizhanchan/qui"
)

func (s *Server) installRoutes() {
	s.mux.HandleFunc("/llm.txt", s.handleLLM)
	s.mux.HandleFunc("/tree", s.handleTree)
	s.mux.HandleFunc("/screenshot", s.handleScreenshot)
	s.mux.HandleFunc("/diagnostics", s.handleDiagnostics)
	s.mux.HandleFunc("/wait", s.handleWait)
	s.mux.HandleFunc("/console", s.handleConsole)
	s.mux.HandleFunc("/events", s.handleEvents)
	s.mux.HandleFunc("/act", s.handleAct)
	s.mux.HandleFunc("/health", s.handleHealth)
	s.installDOMRoutes()
	s.mux.HandleFunc("/", s.handleRoot)
}

func (s *Server) handleRoot(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"name":      "qui-agent",
		"version":   "1",
		"endpoints": []string{"/llm.txt", "/tree", "/screenshot", "/diagnostics", "/wait", "/console", "/events", "/act", "/health", "/dom", "/dom/styles", "/dom/act", "/reactive"},
	})
}

// handleHealth is a liveness/readiness probe. ok is always true once
// the server is serving; ready means at least one laid-out frame is
// available (the accessibility tree has a Root).
func (s *Server) handleHealth(w http.ResponseWriter, _ *http.Request) {
	ready := s.window.AccessibilityTreeSynced().Root != nil
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "ready": ready})
}

func (s *Server) handleLLM(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	_, _ = w.Write(llmTxt)
}

func (s *Server) handleTree(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	selector := q.Get("selector")
	maxDepth := -1
	if v := q.Get("maxDepth"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			maxDepth = n
		}
	}
	leaves := q.Get("leaves") == "1" || q.Get("leaves") == "true"

	// Default behavior (no query params) is byte-compatible with the
	// original full-tree response.
	if selector == "" && maxDepth < 0 && !leaves {
		writeJSON(w, http.StatusOK, s.window.AccessibilityTreeSynced())
		return
	}

	if selector != "" {
		nodes, err := s.window.FindNodes(selector)
		if err != nil {
			http.Error(w, "bad selector: "+err.Error(), http.StatusBadRequest)
			return
		}
		out := make([]any, 0, len(nodes))
		for _, n := range nodes {
			out = append(out, filterNode(n, maxDepth, leaves))
		}
		writeJSON(w, http.StatusOK, out)
		return
	}

	// No selector but maxDepth / leaves set: return the full tree
	// shape with the same filters applied to Root + each Overlay.
	tree := s.window.AccessibilityTreeSynced()
	resp := map[string]any{
		"devicePixelRatio": tree.DevicePixelRatio,
		"windowSize":       tree.WindowSize,
		"root":             filterNode(tree.Root, maxDepth, leaves),
	}
	if len(tree.Accelerators) > 0 {
		resp["accelerators"] = tree.Accelerators
	}
	if len(tree.Overlays) > 0 {
		ovs := make([]any, 0, len(tree.Overlays))
		for _, ov := range tree.Overlays {
			ovs = append(ovs, filterNode(ov, maxDepth, leaves))
		}
		resp["overlays"] = ovs
	}
	writeJSON(w, http.StatusOK, resp)
}

// filterNode serializes an AXNode subtree honoring an optional depth
// cap (maxDepth < 0 = unlimited) and a leaves-only flag. The JSON
// shape matches the default *AXNode encoding so consumers see the
// same field names. depth 0 is the supplied node.
func filterNode(n *qui.AXNode, maxDepth int, leavesOnly bool) any {
	if leavesOnly {
		var out []any
		var walk func(node *qui.AXNode)
		walk = func(node *qui.AXNode) {
			if node == nil {
				return
			}
			if len(node.Children) == 0 {
				out = append(out, axNodeJSON(node, nil))
				return
			}
			for _, c := range node.Children {
				walk(c)
			}
		}
		walk(n)
		return out
	}
	return buildNodeJSON(n, 0, maxDepth)
}

func buildNodeJSON(n *qui.AXNode, depth, maxDepth int) any {
	if n == nil {
		return nil
	}
	var children []any
	if maxDepth < 0 || depth < maxDepth {
		for _, c := range n.Children {
			if cj := buildNodeJSON(c, depth+1, maxDepth); cj != nil {
				children = append(children, cj)
			}
		}
	}
	return axNodeJSON(n, children)
}

// axNodeJSON renders one node with a caller-supplied children slice (the
// depth-limited / leaves-only walks build their own).
//
// It marshals the node itself instead of hand-copying a field list: a
// hand-maintained mirror drifts the moment root grows an AX field, and
// silently — `shortcut` and `hasPopup` were invisible on `/tree?selector=…`
// while the full-tree path had them, which is the worst kind of bug for an
// introspection surface (the data looks authoritative and is incomplete).
// AXNode's own `omitempty` tags decide what appears, so the output shape is
// unchanged.
func axNodeJSON(n *qui.AXNode, children []any) map[string]any {
	shallow := *n
	shallow.Children = nil
	b, err := json.Marshal(&shallow)
	if err != nil {
		return map[string]any{"path": n.Path, "role": n.Role, "error": err.Error()}
	}
	m := map[string]any{}
	if err := json.Unmarshal(b, &m); err != nil {
		return map[string]any{"path": n.Path, "role": n.Role, "error": err.Error()}
	}
	if len(children) > 0 {
		m["children"] = children
	}
	return m
}

func (s *Server) handleDiagnostics(w http.ResponseWriter, _ *http.Request) {
	diags := s.window.DebugLayoutDiagnosticsSynced()
	writeJSON(w, http.StatusOK, diags)
}

func (s *Server) handleScreenshot(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	scale := float32(1)
	if v := q.Get("scale"); v != "" {
		if f, err := strconv.ParseFloat(v, 32); err == nil && f > 0 {
			scale = float32(f)
		}
	}
	annotate := q.Get("annotate") == "1" || q.Get("annotate") == "true"
	var img any
	if rgn := q.Get("region"); rgn != "" {
		var x, y, ww, h float32
		_, err := fmt.Sscanf(rgn, "%g,%g,%g,%g", &x, &y, &ww, &h)
		if err != nil {
			http.Error(w, "bad region: "+err.Error(), http.StatusBadRequest)
			return
		}
		img = s.window.SnapshotRegion(qui.Rect{X: x, Y: y, W: ww, H: h}, scale)
	} else if annotate {
		opts := qui.AnnotateOptions{Scale: scale}
		if h := q.Get("highlight"); h != "" {
			opts.Highlight = h
		}
		if q.Get("onlyLeaves") == "1" {
			opts.OnlyLeaves = true
		}
		if q.Get("onlyFocusable") == "1" {
			opts.OnlyFocusable = true
		}
		img = s.window.SnapshotAnnotated(opts)
	} else {
		img = s.window.SnapshotScaled(scale)
	}
	rgba, ok := img.(image.Image)
	if img == nil || !ok || rgba == nil {
		http.Error(w, "no frame available", http.StatusServiceUnavailable)
		return
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, rgba); err != nil {
		http.Error(w, "png encode: "+err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "image/png")
	_, _ = w.Write(buf.Bytes())
}

func (s *Server) handleWait(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	timeoutMs, _ := strconv.Atoi(q.Get("timeoutMs"))
	if timeoutMs <= 0 {
		timeoutMs = 2000
	}
	timeout := time.Duration(timeoutMs) * time.Millisecond
	what := q.Get("for")
	var err error
	switch what {
	case "", "idle":
		err = s.window.WaitIdle(timeout)
	case "overlayAppears":
		err = s.window.WaitOverlay(q.Get("pattern"), true, timeout)
	case "overlayDisappears":
		err = s.window.WaitOverlay(q.Get("pattern"), false, timeout)
	case "selector":
		sel := q.Get("selector")
		if sel == "" {
			http.Error(w, "for=selector requires selector param", http.StatusBadRequest)
			return
		}
		appear := q.Get("state") != "gone"
		err = s.window.WaitFor(sel, appear, timeout)
	case "stable":
		err = s.window.WaitTreeStable(timeout)
	default:
		http.Error(w, "unknown for="+what, http.StatusBadRequest)
		return
	}
	resp := map[string]any{"ok": err == nil, "idleState": s.window.IdleStateSynced()}
	if err != nil {
		resp["error"] = err.Error()
	}
	writeJSON(w, http.StatusOK, resp)
}

func (s *Server) handleConsole(w http.ResponseWriter, r *http.Request) {
	since := r.URL.Query().Get("since")
	events := s.recordedEvents.Since(since)
	writeJSON(w, http.StatusOK, events)
}

// handleEvents serves Server-Sent Events. Each ring buffer entry
// is emitted as a single "event" line; new entries pushed to the
// ring while the connection is open are streamed live.
func (s *Server) handleEvents(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	// Subscribe to a live channel.
	ch := s.recordedEvents.Subscribe()
	defer s.recordedEvents.Unsubscribe(ch)

	// Drain initial backlog so a fresh client sees recent history.
	for _, ev := range s.recordedEvents.All() {
		if !writeSSE(w, flusher, ev) {
			return
		}
	}
	ctx := r.Context()
	for {
		select {
		case <-ctx.Done():
			return
		case ev, ok := <-ch:
			if !ok {
				return
			}
			if !writeSSE(w, flusher, ev) {
				return
			}
		}
	}
}

func writeSSE(w http.ResponseWriter, fl http.Flusher, ev EventRecord) bool {
	buf, err := json.Marshal(ev)
	if err != nil {
		return false
	}
	if _, err := fmt.Fprintf(w, "data: %s\n\n", buf); err != nil {
		return false
	}
	fl.Flush()
	return true
}

type actRequest struct {
	Action         string           `json:"action"`
	Target         string           `json:"target,omitempty"`
	Text           string           `json:"text,omitempty"`
	Chord          string           `json:"chord,omitempty"`
	Key            string           `json:"key,omitempty"`
	Modifiers      []string         `json:"modifiers,omitempty"`
	DX             float32          `json:"dx,omitempty"`
	DY             float32          `json:"dy,omitempty"`
	Scale          float32          `json:"scale,omitempty"`
	From           string           `json:"from,omitempty"`
	To             string           `json:"to,omitempty"`
	Steps          int              `json:"steps,omitempty"`
	At             *json.RawMessage `json:"at,omitempty"`
	ScrollIntoView *bool            `json:"scrollIntoView,omitempty"`
	Raw            bool             `json:"raw,omitempty"`
	X              *float32         `json:"x,omitempty"`
	Y              *float32         `json:"y,omitempty"`
	X2             *float32         `json:"x2,omitempty"`
	Y2             *float32         `json:"y2,omitempty"`
}

func (s *Server) handleAct(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "POST only", http.StatusMethodNotAllowed)
		return
	}
	var req actRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "bad json: "+err.Error(), http.StatusBadRequest)
		return
	}

	preTree := s.window.AccessibilityTreeSynced()
	preFocus := focusedPath(preTree)
	preOverlays := overlayLayers(preTree)

	mods := parseMods(req.Modifiers)
	siv := true
	if req.ScrollIntoView != nil {
		siv = *req.ScrollIntoView
	}
	var actErr error
	action := strings.ToLower(req.Action)
	if err := requireTarget(action, req.Target, req.X != nil && req.Y != nil); err != nil {
		writeJSON(w, http.StatusOK, map[string]any{
			"ok":        false,
			"error":     err.Error(),
			"errorCode": "no_match",
		})
		return
	}
	switch action {
	case "click":
		if req.Target == "" && req.X != nil && req.Y != nil {
			actErr = s.window.ClickAtWith(*req.X, *req.Y, qui.ClickOptions{Modifiers: mods})
		} else {
			actErr = s.window.Click(req.Target, qui.ClickOptions{Modifiers: mods, ScrollIntoView: siv})
		}
	case "doubleclick":
		actErr = s.window.DoubleClick(req.Target, qui.ClickOptions{Modifiers: mods, ScrollIntoView: siv})
	case "rightclick":
		actErr = s.window.RightClick(req.Target, qui.ClickOptions{Modifiers: mods, ScrollIntoView: siv})
	case "hover":
		actErr = s.window.Hover(req.Target, qui.ClickOptions{Modifiers: mods, ScrollIntoView: siv})
	case "focus":
		actErr = s.window.Focus(req.Target)
	case "blur":
		actErr = s.window.Blur()
	case "type":
		actErr = s.window.Type(req.Target, req.Text, qui.TypeOptions{Raw: req.Raw, ScrollIntoView: siv})
	case "sendkey":
		key, parsedMods, perr := qui.ParseShortcut(req.Key)
		if perr != nil {
			actErr = perr
			break
		}
		actErr = s.window.SendKey(req.Target, key, mods|parsedMods)
	case "sendchord":
		actErr = s.window.SendChord(req.Target, req.Chord)
	case "scroll":
		actErr = s.window.Scroll(req.Target, req.DX, req.DY)
	case "pinch":
		actErr = s.window.Pinch(req.Target, req.Scale)
	case "drag":
		// Point escape hatch (mirrors click's): with x/y + x2/y2 and no
		// from/to selectors, drag between raw window points — the only way
		// to reach drag handles that aren't addressable widgets (ruler
		// markers, canvas grips).
		if req.From == "" && req.To == "" && req.X != nil && req.Y != nil && req.X2 != nil && req.Y2 != nil {
			actErr = s.window.DragAt(*req.X, *req.Y, *req.X2, *req.Y2, req.Steps)
		} else {
			actErr = s.window.Drag(req.From, req.To, qui.DragOptions{Steps: req.Steps, Modifiers: mods})
		}
	default:
		actErr = errors.New("unknown action: " + req.Action)
	}

	postTree := s.window.AccessibilityTreeSynced()
	resp := map[string]any{
		"ok":          actErr == nil,
		"treeHash":    postTree.Hash(),
		"newOverlays": diffLayers(preOverlays, overlayLayers(postTree)),
		"focusChange": focusDelta(preFocus, focusedPath(postTree)),
		"diagnostics": s.window.DebugLayoutDiagnosticsSynced(),
		"idleState":   s.window.IdleStateSynced(),
	}
	if actErr != nil {
		resp["error"] = actErr.Error()
		var modal *qui.ErrModalBlocked
		if errors.As(actErr, &modal) {
			resp["errorCode"] = "modal_blocked"
			resp["blockingPath"] = modal.BlockingPath
		} else if errors.Is(actErr, qui.ErrNoMatch) {
			resp["errorCode"] = "no_match"
		} else if errors.Is(actErr, qui.ErrActionTimeout) {
			resp["errorCode"] = "timeout"
		} else if errors.Is(actErr, qui.ErrNotTextTarget) {
			resp["errorCode"] = "not_text_target"
		} else if errors.Is(actErr, qui.ErrNotVisible) {
			resp["errorCode"] = "not_visible"
		} else if errors.Is(actErr, qui.ErrWindowClosed) {
			resp["errorCode"] = "window_closed"
		} else if errors.Is(actErr, qui.ErrInvalidSelector) {
			resp["errorCode"] = "bad_selector"
		} else {
			resp["errorCode"] = "error"
		}
	}
	writeJSON(w, http.StatusOK, resp)
}

// requireTarget rejects a targetless action before it reaches the action
// layer. Every action but blur (and a coordinate-only click) addresses a
// widget, and a missing "target" is nearly always a request that named the
// field something else — a mistake worth reporting as such rather than
// letting an empty selector go looking for a match.
func requireTarget(action, target string, hasPoint bool) error {
	switch action {
	case "blur", "":
		return nil
	case "drag":
		return nil // uses from / to, validated by the action layer
	case "click":
		if hasPoint {
			return nil
		}
	}
	if strings.TrimSpace(target) == "" {
		return fmt.Errorf("action %q needs a widget selector in the \"target\" field", action)
	}
	return nil
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func parseMods(strs []string) qui.Modifiers {
	var m qui.Modifiers
	for _, s := range strs {
		switch strings.ToLower(s) {
		case "shift":
			m |= qui.ModShift
		case "ctrl", "control":
			m |= qui.ModControl
		case "alt", "option":
			m |= qui.ModAlt
		case "cmd", "super", "meta":
			m |= qui.ModSuper
		}
	}
	return m
}

func focusedPath(tree *qui.AccessibilityTree) string {
	if tree == nil {
		return ""
	}
	for _, n := range tree.Flatten() {
		for _, state := range strings.Split(n.State, "|") {
			if state == "focused" {
				return n.Path
			}
		}
	}
	return ""
}

func overlayLayers(tree *qui.AccessibilityTree) []string {
	out := make([]string, 0, len(tree.Overlays))
	for _, ov := range tree.Overlays {
		out = append(out, ov.Layer)
	}
	return out
}

func diffLayers(before, after []string) []string {
	beforeSet := map[string]bool{}
	for _, b := range before {
		beforeSet[b] = true
	}
	var fresh []string
	for _, a := range after {
		if !beforeSet[a] {
			fresh = append(fresh, a)
		}
	}
	return fresh
}

func focusDelta(before, after string) map[string]string {
	if before == after {
		return nil
	}
	return map[string]string{"from": before, "to": after}
}
