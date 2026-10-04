package agent

// HTML-level (DOM) agent surface. Where the widget endpoints (/tree,
// /act, …) address the rendered widget tree, these address the app the
// way its author wrote it: an HTML DOM with real CSS selectors and a
// DevTools-shaped styles view. Business UIs built on htmlcss/reactive-html
// are debugged far more directly here — by class, by tag, by combinator,
// with "which rule won and why" available — than by hand-mapping the
// widget tree. Backed by htmlcss.InspectWindow; the widget layer is
// untouched and remains the tool for low-level render debugging.

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/qizhanchan/qui"
	"github.com/qizhanchan/qui/htmlcss"
	"github.com/qizhanchan/qui/reactive"
)

// Auto-wait defaults for /dom/act: like Playwright, an action waits for
// its target to appear and become visible before acting, so callers don't
// have to interleave explicit /wait calls. timeoutMs:0 disables the wait
// (single attempt); omitting timeoutMs uses the default.
const (
	domActDefaultWaitMs = 1000
	domActPollInterval  = 25 * time.Millisecond
)

func (s *Server) installDOMRoutes() {
	s.mux.HandleFunc("/dom", s.handleDOM)
	s.mux.HandleFunc("/dom/styles", s.handleDOMStyles)
	s.mux.HandleFunc("/dom/act", s.handleDOMAct)
	s.mux.HandleFunc("/reactive", s.handleReactive)
}

// handleReactive serves the reactive component-tree snapshot: mounted
// components, their hook state (UseState/UseSignal/UseMemo/UseEffect),
// props, pending-render status, and backing widgets. 204 when the window
// isn't driven by a reactive runtime.
func (s *Server) handleReactive(w http.ResponseWriter, _ *http.Request) {
	rt := reactive.RuntimeForWindow(s.window)
	if rt == nil {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	writeJSON(w, http.StatusOK, rt.Inspect())
}

// handleDOM serves the projected DOM tree. With ?selector it returns the
// array of matching subtrees (DevTools $$); ?maxDepth caps recursion.
func (s *Server) handleDOM(w http.ResponseWriter, r *http.Request) {
	dom := htmlcss.InspectWindow(s.window)
	q := r.URL.Query()
	maxDepth := -1
	if v := q.Get("maxDepth"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			maxDepth = n
		}
	}
	if selector := q.Get("selector"); selector != "" {
		nodes, err := dom.Query(selector)
		if err != nil {
			http.Error(w, "bad selector: "+err.Error(), http.StatusBadRequest)
			return
		}
		out := make([]any, 0, len(nodes))
		for _, n := range nodes {
			out = append(out, filterDOM(n, 0, maxDepth))
		}
		writeJSON(w, http.StatusOK, out)
		return
	}
	if maxDepth < 0 {
		writeJSON(w, http.StatusOK, dom)
		return
	}
	resp := map[string]any{
		"devicePixelRatio": dom.DevicePixelRatio,
		"windowSize":       dom.WindowSize,
		"root":             filterDOM(dom.Root, 0, maxDepth),
	}
	if len(dom.Overlays) > 0 {
		ovs := make([]any, 0, len(dom.Overlays))
		for _, ov := range dom.Overlays {
			ovs = append(ovs, filterDOM(ov, 0, maxDepth))
		}
		resp["overlays"] = ovs
	}
	writeJSON(w, http.StatusOK, resp)
}

// filterDOM serializes a DOMNode subtree honoring a depth cap (< 0 =
// unlimited; 0 = the node with no children). The JSON shape matches the
// default *DOMNode encoding.
func filterDOM(n *htmlcss.DOMNode, depth, maxDepth int) any {
	if n == nil {
		return nil
	}
	m := map[string]any{
		"path":    n.Path,
		"tag":     n.Tag,
		"bounds":  n.Bounds,
		"visible": n.Visible,
	}
	if n.ID != "" {
		m["id"] = n.ID
	}
	if len(n.Classes) > 0 {
		m["classes"] = n.Classes
	}
	if len(n.Attrs) > 0 {
		m["attrs"] = n.Attrs
	}
	if n.Text != "" {
		m["text"] = n.Text
	}
	if n.Role != "" {
		m["role"] = n.Role
	}
	if n.Display != "" {
		m["display"] = n.Display
	}
	if n.State != "" {
		m["state"] = n.State
	}
	if n.TextState != nil {
		m["textState"] = n.TextState
	}
	if len(n.Options) > 0 {
		m["options"] = n.Options
	}
	if n.Layer != "" {
		m["layer"] = n.Layer
	}
	if (maxDepth < 0 || depth < maxDepth) && len(n.Children) > 0 {
		kids := make([]any, 0, len(n.Children))
		for _, c := range n.Children {
			kids = append(kids, filterDOM(c, depth+1, maxDepth))
		}
		m["children"] = kids
	}
	return m
}

// handleDOMStyles serves the DevTools "Styles"/"Computed" view for every
// element matching ?selector.
func (s *Server) handleDOMStyles(w http.ResponseWriter, r *http.Request) {
	selector := r.URL.Query().Get("selector")
	if selector == "" {
		http.Error(w, "selector param required", http.StatusBadRequest)
		return
	}
	dom := htmlcss.InspectWindow(s.window)
	styles, err := dom.Styles(selector)
	if err != nil {
		http.Error(w, "bad selector: "+err.Error(), http.StatusBadRequest)
		return
	}
	writeJSON(w, http.StatusOK, styles)
}

type domActRequest struct {
	Action         string   `json:"action"`
	Target         string   `json:"target,omitempty"`
	Text           string   `json:"text,omitempty"`
	Key            string   `json:"key,omitempty"`
	Chord          string   `json:"chord,omitempty"`
	Modifiers      []string `json:"modifiers,omitempty"`
	DX             float32  `json:"dx,omitempty"`
	DY             float32  `json:"dy,omitempty"`
	ScrollIntoView *bool    `json:"scrollIntoView,omitempty"`
	Raw            bool     `json:"raw,omitempty"`
	// TimeoutMs bounds the auto-wait for the target to appear+become
	// visible. nil = default (domActDefaultWaitMs); 0 = no wait (single
	// resolution attempt, the pre-auto-wait behavior).
	TimeoutMs *int `json:"timeoutMs,omitempty"`
}

// domActWaitTimeout resolves the effective auto-wait duration.
func domActWaitTimeout(ms *int) time.Duration {
	if ms == nil {
		return domActDefaultWaitMs * time.Millisecond
	}
	if *ms <= 0 {
		return 0
	}
	return time.Duration(*ms) * time.Millisecond
}

// resolveWithWait resolves target to a live widget, retrying until a
// VISIBLE match appears or timeout elapses. It only retries the safe
// cases — target absent (ErrNoMatch) or present-but-not-yet-visible —
// which occur before any event is dispatched, so no action is ever
// partially applied across retries. A selector parse error returns
// immediately. On timeout it returns the last resolution (a hidden match
// or ErrNoMatch) so the caller surfaces a precise error.
func (s *Server) resolveWithWait(target string, timeout time.Duration) (qui.Widget, *htmlcss.DOMNode, error) {
	deadline := time.Now().Add(timeout)
	for {
		dom := htmlcss.InspectWindow(s.window)
		widget, node, err := dom.ResolveWidget(target)
		switch {
		case err == nil && node.Visible:
			return widget, node, nil
		case err != nil && !errors.Is(err, qui.ErrNoMatch):
			return nil, nil, err // parse error — retrying won't help
		}
		if timeout <= 0 || !time.Now().Before(deadline) {
			return widget, node, err // final: hidden match or no_match
		}
		time.Sleep(domActPollInterval)
	}
}

// handleDOMAct performs an action against the first element matching a CSS
// selector. Resolution goes through the DOM (class/tag/combinator aware);
// the action itself reuses the window's widget-targeted action variants,
// so modal-blocking, scroll-into-view and undo-coalescing behave exactly
// as on the widget /act path.
func (s *Server) handleDOMAct(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "POST only", http.StatusMethodNotAllowed)
		return
	}
	var req domActRequest
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
	action := strings.ToLower(req.Action)

	var resolved *htmlcss.DOMNode
	var actErr error

	if err := requireTarget(action, req.Target, false); err != nil {
		writeJSON(w, http.StatusOK, map[string]any{
			"ok":        false,
			"error":     err.Error(),
			"errorCode": "no_match",
		})
		return
	}

	// blur needs no target; everything else resolves the CSS selector.
	if action == "blur" {
		actErr = s.window.Blur()
	} else {
		widget, node, err := s.resolveWithWait(req.Target, domActWaitTimeout(req.TimeoutMs))
		if err != nil {
			actErr = err
		} else {
			resolved = node
			clickOpts := qui.ClickOptions{Modifiers: mods, ScrollIntoView: siv}
			switch action {
			case "click":
				actErr = s.window.ClickWidget(widget, clickOpts)
			case "doubleclick":
				actErr = s.window.DoubleClickWidget(widget, clickOpts)
			case "rightclick":
				actErr = s.window.RightClickWidget(widget, clickOpts)
			case "hover":
				actErr = s.window.HoverWidget(widget, clickOpts)
			case "focus":
				actErr = s.window.FocusWidget(htmlcss.EditableTarget(widget))
			case "type":
				actErr = s.window.TypeWidget(htmlcss.EditableTarget(widget), req.Text, qui.TypeOptions{Raw: req.Raw, ScrollIntoView: siv})
			case "scroll":
				actErr = s.window.ScrollWidget(widget, req.DX, req.DY)
			case "sendkey":
				var key qui.Key
				var parsedMods qui.Modifiers
				key, parsedMods, actErr = qui.ParseShortcut(req.Key)
				if actErr == nil {
					if actErr = s.window.FocusWidget(htmlcss.EditableTarget(widget)); actErr == nil {
						actErr = s.window.SendKey("", key, mods|parsedMods)
					}
				}
			case "sendchord":
				if actErr = s.window.FocusWidget(htmlcss.EditableTarget(widget)); actErr == nil {
					actErr = s.window.SendChord("", req.Chord)
				}
			default:
				actErr = errors.New("unknown action: " + req.Action)
			}
		}
	}

	postTree := s.window.AccessibilityTreeSynced()
	resp := map[string]any{
		"ok":          actErr == nil,
		"treeHash":    postTree.Hash(),
		"newOverlays": diffLayers(preOverlays, overlayLayers(postTree)),
		"focusChange": focusDelta(preFocus, focusedPath(postTree)),
		"idleState":   s.window.IdleStateSynced(),
	}
	if resolved != nil {
		resp["matched"] = map[string]any{"path": resolved.Path, "selector": resolved.Selector()}
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
		} else {
			resp["errorCode"] = "error"
		}
	}
	writeJSON(w, http.StatusOK, resp)
}
