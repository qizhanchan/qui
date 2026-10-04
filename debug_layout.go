package qui

import (
	"fmt"
	"reflect"
	"runtime"
	"strings"
)

// DiagnosticSeverity classifies a layout diagnostic.
type DiagnosticSeverity string

const (
	DiagnosticWarning DiagnosticSeverity = "warning"
	DiagnosticError   DiagnosticSeverity = "error"
)

// DebugOverlayOptions controls the opt-in layout debug overlay.
//
// ToggleShortcut is nil for the platform default shortcut
// (Cmd+Shift+D on macOS, Ctrl+Shift+D elsewhere). A non-nil pointer
// overrides it; a pointer to "" disables keyboard toggling.
//
// The "agent" toggles (ShowIDs / ShowRoles / ShowAccessibleNames /
// ShowFocus) target the AI-driver workflow: render the same data
// SnapshotAnnotated produces, but live in the window. Useful for
// designing widget IDs and verifying the accessibility tree
// matches what the agent will see.
type DebugOverlayOptions struct {
	ShowBounds          bool
	ShowOverflow        bool
	ShowLabels          bool
	ShowDirtyRegion     bool
	ShowIDs             bool
	ShowRoles           bool
	ShowAccessibleNames bool
	ShowFocus           bool
	ToggleShortcut      *string
}

// LayoutDiagnostic describes a likely layout problem found in the
// currently-laid-out widget tree.
type LayoutDiagnostic struct {
	Severity     DiagnosticSeverity
	WidgetType   string
	Path         string
	Bounds       Rect
	ParentBounds Rect
	Message      string
	Hint         string
}

type debugNode struct {
	w      Widget
	parent Widget
	path   string
	depth  int
}

// EnableDebugOverlay turns on the runtime layout overlay for this
// window. Passing a zero-value options struct enables the default useful
// views: bounds, overflow, and labels.
func (w *Window) EnableDebugOverlay(opts DebugOverlayOptions) {
	if w == nil {
		return
	}
	w.assertUIThread("Window.EnableDebugOverlay")
	w.debugOverlayConfigured = true
	w.debugOverlayOptions = normalizeDebugOverlayOptions(opts)
	w.debugOverlayEnabled = true
	w.Invalidate()
}

// DisableDebugOverlay hides the runtime layout overlay. The shortcut
// remains configured so it can toggle the overlay back on.
func (w *Window) DisableDebugOverlay() {
	if w == nil {
		return
	}
	w.assertUIThread("Window.DisableDebugOverlay")
	w.debugOverlayEnabled = false
	w.Invalidate()
}

// IsDebugOverlayEnabled reports whether the debug overlay is
// currently being drawn. Used by tests and by code that wants to
// reflect overlay state in its own UI.
func (w *Window) IsDebugOverlayEnabled() bool {
	if w == nil {
		return false
	}
	w.assertUIThread("Window.IsDebugOverlayEnabled")
	return w.debugOverlayEnabled
}

// ToggleDebugOverlay flips the runtime layout overlay. If the overlay
// has not been configured yet, it uses default options.
func (w *Window) ToggleDebugOverlay() {
	if w == nil {
		return
	}
	w.assertUIThread("Window.ToggleDebugOverlay")
	if !w.debugOverlayConfigured {
		w.debugOverlayConfigured = true
		w.debugOverlayOptions = normalizeDebugOverlayOptions(DebugOverlayOptions{})
	}
	w.debugOverlayEnabled = !w.debugOverlayEnabled
	w.Invalidate()
}

// DebugLayoutDiagnostics returns layout warnings and errors for the
// current widget tree. It is read-only and does not trigger layout.
func (w *Window) DebugLayoutDiagnostics() []LayoutDiagnostic {
	if w == nil {
		return nil
	}
	w.assertUIThread("Window.DebugLayoutDiagnostics")
	var out []LayoutDiagnostic
	for _, root := range w.debugRoots() {
		w.collectLayoutDiagnostics(root, nil, widgetTypeName(root), 0, &out)
	}
	return out
}

// DebugLayoutDiagnosticsSynced captures diagnostics on the window's main
// goroutine. Off-main consumers such as the agent server must use this form
// because the diagnostic walk reads live widget bounds and child slices.
func (w *Window) DebugLayoutDiagnosticsSynced() []LayoutDiagnostic {
	if w == nil {
		return nil
	}
	var diagnostics []LayoutDiagnostic
	_ = w.synchronously(func() error {
		diagnostics = w.DebugLayoutDiagnostics()
		return nil
	})
	return diagnostics
}

func normalizeDebugOverlayOptions(opts DebugOverlayOptions) DebugOverlayOptions {
	anySet := opts.ShowBounds || opts.ShowOverflow || opts.ShowLabels ||
		opts.ShowDirtyRegion || opts.ShowIDs || opts.ShowRoles ||
		opts.ShowAccessibleNames || opts.ShowFocus
	if !anySet {
		opts.ShowBounds = true
		opts.ShowOverflow = true
		opts.ShowLabels = true
	}
	return opts
}

func (w *Window) handleDebugOverlayShortcut(ke KeyEvent) bool {
	if w == nil || !w.debugOverlayConfigured {
		return false
	}
	shortcut := w.debugOverlayOptions.ToggleShortcut
	if shortcut != nil && strings.TrimSpace(*shortcut) == "" {
		return false
	}
	wantKey, wantMods, err := defaultDebugOverlayShortcut()
	if shortcut != nil {
		wantKey, wantMods, err = ParseShortcut(*shortcut)
	}
	if err != nil {
		return false
	}
	if ke.Key != wantKey || ke.Mods != wantMods {
		return false
	}
	w.ToggleDebugOverlay()
	return true
}

func defaultDebugOverlayShortcut() (Key, Modifiers, error) {
	if runtime.GOOS == "darwin" {
		return KeyD, ModSuper | ModShift, nil
	}
	return KeyD, ModControl | ModShift, nil
}

func (w *Window) drawDebugOverlay(canvas Canvas) {
	if w == nil || canvas == nil {
		return
	}
	opts := w.debugOverlayOptions
	nodes := w.debugNodes()
	diags := w.DebugLayoutDiagnostics()
	overflow := map[Widget]bool{}
	for _, d := range diags {
		if d.Message == "widget overflows parent bounds" || d.Message == "absolute bottom-anchored widget is outside the window" {
			if n := findDebugNode(nodes, d.Path); n != nil {
				overflow[n.w] = true
			}
		}
	}
	for _, n := range nodes {
		b := n.w.Bounds()
		if b.IsEmpty() {
			continue
		}
		if opts.ShowBounds {
			canvas.StrokeRect(b, debugDepthColor(n.depth), 1)
		}
		if opts.ShowOverflow && overflow[n.w] {
			canvas.FillRect(b, Color{R: 1, G: 0.1, B: 0.1, A: 0.16})
			canvas.StrokeRect(b, Color{R: 1, G: 0.05, B: 0.05, A: 1}, 2)
		}
		if opts.ShowFocus && w.focused == n.w {
			canvas.StrokeRect(
				Rect{X: b.X - 2, Y: b.Y - 2, W: b.W + 4, H: b.H + 4},
				Color{R: 0.2, G: 0.85, B: 1, A: 1}, 2)
		}
		var labelParts []string
		if opts.ShowLabels {
			labelParts = append(labelParts, fmt.Sprintf("%s %.0f,%.0f %.0fx%.0f", widgetTypeName(n.w), b.X, b.Y, b.W, b.H))
		}
		if opts.ShowIDs {
			if id := WidgetID(n.w); id != "" {
				labelParts = append(labelParts, "#"+id)
			}
		}
		if opts.ShowRoles {
			labelParts = append(labelParts, "@"+WidgetRole(n.w))
		}
		if opts.ShowAccessibleNames {
			if name := WidgetName(n.w); name != "" {
				labelParts = append(labelParts, "\""+name+"\"")
			}
		}
		if len(labelParts) > 0 {
			label := strings.Join(labelParts, " ")
			canvas.FillRect(Rect{X: b.X + 2, Y: b.Y + 2, W: 280, H: 16}, Color{R: 0, G: 0, B: 0, A: 0.65})
			canvas.DrawText(label, Rect{X: b.X + 5, Y: b.Y + 3, W: 274, H: 14}, ColorWhite, Font{Size: 10})
		}
	}
	if opts.ShowDirtyRegion && !w.dirtyRegion.IsEmpty() {
		canvas.StrokeRect(w.dirtyRegion, Color{R: 1, G: 0.9, B: 0.1, A: 1}, 2)
	}
}

// EnableAgentOverlay turns on the AI-friendly debug overlay
// (bounds + IDs + roles + accessible names + current focus). Lets
// developers see what the agent would see while iterating on
// addressing decisions.
func (w *Window) EnableAgentOverlay() {
	w.EnableDebugOverlay(agentOverlayPreset())
}

// ToggleAgentOverlay flips the agent overlay on/off. When the
// overlay is currently showing the agent preset, this turns it off;
// otherwise it switches to the agent preset and shows it. Unlike
// ToggleDebugOverlay, this does not require any prior Enable call —
// the Cmd+Shift+A shortcut wires straight to it.
func (w *Window) ToggleAgentOverlay() {
	if w == nil {
		return
	}
	w.assertUIThread("Window.ToggleAgentOverlay")
	if w.debugOverlayEnabled && isAgentOverlayOptions(w.debugOverlayOptions) {
		w.DisableDebugOverlay()
		return
	}
	w.EnableDebugOverlay(agentOverlayPreset())
}

func agentOverlayPreset() DebugOverlayOptions {
	return DebugOverlayOptions{
		ShowBounds:          true,
		ShowIDs:             true,
		ShowRoles:           true,
		ShowAccessibleNames: true,
		ShowFocus:           true,
	}
}

func isAgentOverlayOptions(opts DebugOverlayOptions) bool {
	return opts.ShowBounds && opts.ShowIDs && opts.ShowRoles &&
		opts.ShowAccessibleNames && opts.ShowFocus
}

// handleAgentOverlayShortcut intercepts Cmd+Shift+A (macOS) /
// Ctrl+Shift+A (other platforms) regardless of whether the debug
// overlay has been "configured" — pressing the shortcut is itself
// the opt-in. Returns true when the event is consumed.
func (w *Window) handleAgentOverlayShortcut(ke KeyEvent) bool {
	if w == nil {
		return false
	}
	wantKey, wantMods, err := defaultAgentOverlayShortcut()
	if err != nil {
		return false
	}
	if ke.Key != wantKey || ke.Mods != wantMods {
		return false
	}
	w.ToggleAgentOverlay()
	return true
}

func defaultAgentOverlayShortcut() (Key, Modifiers, error) {
	if runtime.GOOS == "darwin" {
		return KeyA, ModSuper | ModShift, nil
	}
	return KeyA, ModControl | ModShift, nil
}

func (w *Window) collectLayoutDiagnostics(widget, parent Widget, path string, depth int, out *[]LayoutDiagnostic) {
	if widget == nil {
		return
	}
	bounds := widget.Bounds()
	// A scrolling container's content is SUPPOSED to be bigger than the
	// viewport — that is what there is to scroll. Reporting it as an
	// overflow turns every scrolled panel or list into a layout error and
	// buries the real ones. ScrollIntoViewable is the existing contract for
	// "this parent scrolls its children" (the agent layer uses it before
	// clicking an offscreen target), so it is what identifies them here.
	_, parentScrolls := parent.(ScrollIntoViewable)
	if parent != nil && !parentScrolls {
		parentBounds := parent.Bounds()
		contentBounds := debugParentContentBounds(parent)
		if !rectContainsRect(parentBounds, bounds) || !rectContainsRect(contentBounds, bounds) {
			*out = append(*out, LayoutDiagnostic{
				Severity:     DiagnosticError,
				WidgetType:   widgetTypeName(widget),
				Path:         path,
				Bounds:       bounds,
				ParentBounds: parentBounds,
				Message:      "widget overflows parent bounds",
				Hint:         "Check parent layout constraints, min size, FlexItem basis/shrink, or absolute anchors.",
			})
		}
	}
	pos := AbsolutePositionOf(widget)
	if pos.Anchor&AnchorBottom != 0 {
		windowBounds := w.Bounds()
		if !windowBounds.IsEmpty() && bounds.Y+bounds.H > windowBounds.Y+windowBounds.H+0.5 {
			*out = append(*out, LayoutDiagnostic{
				Severity:     DiagnosticWarning,
				WidgetType:   widgetTypeName(widget),
				Path:         path,
				Bounds:       bounds,
				ParentBounds: windowBounds,
				Message:      "absolute bottom-anchored widget is outside the window",
				Hint:         "Check whether an ancestor is larger than its visible Flex allocation; call SetFlex(1) on children meant to fill remaining space.",
			})
		}
	}
	w.collectFlexDiagnostics(widget, path, out)

	if l, ok := widget.(childLister); ok {
		for i, child := range l.ChildList() {
			childPath := fmt.Sprintf("%s/%s[%d]", path, widgetTypeName(child), i)
			w.collectLayoutDiagnostics(child, widget, childPath, depth+1, out)
		}
	}
}

func (w *Window) collectFlexDiagnostics(widget Widget, path string, out *[]LayoutDiagnostic) {
	c, ok := widget.(*Container)
	if !ok || c == nil || c.ChildCount() < 2 {
		return
	}
	fl, ok := c.LayoutEngine.(FlexLayout)
	if !ok {
		return
	}
	content := c.Bounds().Inset(c.Style().Padding)
	mainAvail := content.W
	if fl.Direction == Vertical {
		mainAvail = content.H
	}
	if mainAvail <= 0 {
		return
	}
	totalGap := fl.Gap * float32(c.ChildCount()-1)
	var basisSum float32
	type candidate struct {
		w    Widget
		path string
		main float32
	}
	var candidates []candidate
	for i, child := range c.ChildList() {
		fx := widgetFlexItem(child)
		nat := measureWithConstraints(child, Size{W: content.W, H: content.H})
		main := nat.W
		if fl.Direction == Vertical {
			main = nat.H
		}
		basis := fx.Basis
		if basis <= 0 {
			basis = main
		}
		basisSum += basis
		if fx.Grow > 0 && fx.Shrink <= 0 && fx.Basis <= 0 && main >= mainAvail*0.8 {
			candidates = append(candidates, candidate{
				w:    child,
				path: fmt.Sprintf("%s/%s[%d]", path, widgetTypeName(child), i),
				main: main,
			})
		}
	}
	if basisSum+totalGap <= mainAvail+0.5 {
		return
	}
	for _, c := range candidates {
		*out = append(*out, LayoutDiagnostic{
			Severity:     DiagnosticWarning,
			WidgetType:   widgetTypeName(c.w),
			Path:         c.path,
			Bounds:       c.w.Bounds(),
			ParentBounds: widget.Bounds(),
			Message:      "flex grow child has a full-size measured basis",
			Hint:         "Grow child with full-size measured basis; call SetFlex(1) instead — it sets Grow + zeroes Basis (CSS `flex: 1`).",
		})
	}
}

func (w *Window) debugRoots() []Widget {
	if w == nil {
		return nil
	}
	var roots []Widget
	if w.root != nil {
		roots = append(roots, w.root)
	}
	roots = append(roots, w.overlays...)
	return roots
}

func (w *Window) debugNodes() []debugNode {
	var nodes []debugNode
	for _, root := range w.debugRoots() {
		collectDebugNodes(root, nil, widgetTypeName(root), 0, &nodes)
	}
	return nodes
}

func collectDebugNodes(widget, parent Widget, path string, depth int, out *[]debugNode) {
	if widget == nil {
		return
	}
	*out = append(*out, debugNode{w: widget, parent: parent, path: path, depth: depth})
	if l, ok := widget.(childLister); ok {
		for i, child := range l.ChildList() {
			collectDebugNodes(child, widget, fmt.Sprintf("%s/%s[%d]", path, widgetTypeName(child), i), depth+1, out)
		}
	}
}

func findDebugNode(nodes []debugNode, path string) *debugNode {
	for i := range nodes {
		if nodes[i].path == path {
			return &nodes[i]
		}
	}
	return nil
}

func debugParentContentBounds(widget Widget) Rect {
	if c, ok := widget.(*Container); ok && c != nil {
		return c.Bounds().Inset(c.Style().Padding)
	}
	return widget.Bounds()
}

func rectContainsRect(outer, inner Rect) bool {
	if inner.IsEmpty() {
		return true
	}
	return inner.X >= outer.X &&
		inner.Y >= outer.Y &&
		inner.X+inner.W <= outer.X+outer.W+0.5 &&
		inner.Y+inner.H <= outer.Y+outer.H+0.5
}

func debugDepthColor(depth int) Color {
	palette := []Color{
		{R: 0.3, G: 0.65, B: 1, A: 1},
		{R: 0.2, G: 0.9, B: 0.55, A: 1},
		{R: 1, G: 0.7, B: 0.2, A: 1},
		{R: 0.85, G: 0.45, B: 1, A: 1},
	}
	return palette[depth%len(palette)]
}

func widgetTypeName(widget Widget) string {
	if widget == nil {
		return "<nil>"
	}
	t := reflect.TypeOf(widget)
	if t.Kind() == reflect.Ptr {
		t = t.Elem()
	}
	if t.PkgPath() == "" {
		return t.Name()
	}
	parts := strings.Split(t.PkgPath(), "/")
	return parts[len(parts)-1] + "." + t.Name()
}
