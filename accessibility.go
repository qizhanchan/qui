package qui

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
)

// AccessibilityTree returns a structured snapshot of the window's
// widget tree as it is currently laid out. The result is safe to
// JSON-encode and is the canonical "DOM" the agent layer exposes.
//
// Structure: a single Root subtree (the main widget tree under
// Window.SetRoot) plus a peer-level slice of Overlay subtrees (one
// per Window.PushOverlay). Overlays are NOT nested under Root because
// they are not children of any widget — they paint on top with their
// own coordinate space, and selectors that target "the current
// dialog" must address them without knowing the root path.
//
// Each AXNode carries:
//   - Path: tree-position string (collectDebugNodes scheme:
//     "TypeName[i]/TypeName[j]/..." rooted at the subtree origin)
//   - ID: the developer-assigned ID via SetID, or ""
//   - Role: semantic role (see WidgetRole)
//   - Name: accessible name (see WidgetName)
//   - Value: accessible value (see WidgetValue)
//   - State: bitmask string (focused / hovered / disabled / hidden ...)
//   - Shortcut: keyboard accelerator, if the widget publishes one
//   - HasPopup: true when activating the widget opens further UI
//   - Bounds: logical-pixel rect (origin is the window's logical 0,0)
//   - Visible: non-empty bounds and no hidden/collapsed ancestor
//   - Children: child subtrees in paint order (bottom to top)
//
// Costs are O(N) where N is the laid-out widget count. The walk is
// allocation-light: one AXNode per widget, no closures over per-node
// state. Suitable for sub-millisecond /tree responses on typical UI
// trees (hundreds of widgets).
type AXNode struct {
	Path string `json:"path"`
	ID   string `json:"id,omitempty"`
	Role string `json:"role"`
	Name string `json:"name,omitempty"`
	// NameKey is the message key Name was translated from, when the
	// widget's caption comes from the catalog rather than a literal.
	//
	// It exists because Name is not a stable identifier in a localized
	// app: `[name="Save"]` stops matching the moment the UI renders in
	// German. NameKey does not change with the language, so agent
	// scripts, CI smoke tests and recorded interactions written against
	// `[key=qui.save]` keep working across every locale.
	//
	// Prefer #id where the widget has one; prefer [key=] over [name=]
	// everywhere else.
	NameKey string `json:"nameKey,omitempty"`
	Value   string `json:"value,omitempty"`
	State   string `json:"state,omitempty"`
	// Shortcut is the keyboard accelerator this node publishes (ARIA
	// `aria-keyshortcuts`), in the same display form the widget shows the
	// user — "⌘⇧S". Present only for widgets implementing Shortcutted.
	Shortcut string `json:"shortcut,omitempty"`
	// HasPopup marks a node that opens further UI when activated (ARIA
	// `aria-haspopup`) — a submenu parent, a dropdown trigger. It lets a
	// tree walker discover nested structure without clicking blind:
	// activating a plain item would run its command.
	HasPopup bool   `json:"hasPopup,omitempty"`
	Bounds   AXRect `json:"bounds"`
	Visible  bool   `json:"visible"`
	Layer    string `json:"layer,omitempty"` // "root" | "overlay[N]" | "modal[N]"
	// TextState is present only for text-editing widgets (Input, TextArea):
	// caret / selection / IME preedit / undo availability, for debugging
	// input behavior. Deliberately excluded from Hash — caret and selection
	// change far more often than "structure", and WaitTreeStable should not
	// churn on cursor movement.
	TextState *TextState `json:"textState,omitempty"`
	// Options is present for choice widgets (Select): the full option set
	// and which one is selected, so an agent sees what can be picked
	// without opening the dropdown. Included in Hash (a selection change
	// should invalidate a WaitTreeStable).
	Options  []AXOption `json:"options,omitempty"`
	Children []*AXNode  `json:"children,omitempty"`

	// widget is the back-reference used by selector resolution to map
	// matched AXNodes to live Widget pointers. Not serialized.
	widget Widget `json:"-"`
}

// AXRect is a JSON-friendly mirror of Rect (float32 fields don't
// survive encoding/json cleanly without a custom wrapper).
type AXRect struct {
	X float32 `json:"x"`
	Y float32 `json:"y"`
	W float32 `json:"w"`
	H float32 `json:"h"`
}

func axRect(r Rect) AXRect { return AXRect{X: r.X, Y: r.Y, W: r.W, H: r.H} }

// AccessibilityTree is the top-level result of Window.AccessibilityTree.
// Root is the main tree; Overlays are the per-overlay subtrees, in
// stack order (bottom to top). Both Root and entries in Overlays
// will be nil if the window has no root / no overlays respectively.
//
// DevicePixelRatio is captured at snapshot time so consumers can
// translate logical bounds to physical pixels (matches the screenshot
// scale relationship).
type AccessibilityTree struct {
	Root     *AXNode   `json:"root"`
	Overlays []*AXNode `json:"overlays,omitempty"`
	// Accelerators are the window-level keyboard shortcuts (Window.
	// SetAcceleratorRegistry). They belong on the tree rather than on a node
	// because they have no widget — and a command reachable only by keyboard
	// is still a command an agent should be able to discover.
	Accelerators     []string `json:"accelerators,omitempty"`
	DevicePixelRatio float32  `json:"devicePixelRatio"`
	WindowSize       AXRect   `json:"windowSize"`
}

// Hash returns a stable content hash of the tree shape and per-node
// state. Two trees that differ in any visible attribute produce
// different hashes; identical trees produce identical hashes. Used by
// the agent /act endpoint to surface "did anything change?" without
// shipping the full tree on every action reply.
//
// The hash covers Path, ID, Role, Name, NameKey, Value, State, Shortcut,
// HasPopup, Bounds, Layer, and the recursive shape of Children.
// DevicePixelRatio is included at the top level so a Retina change
// invalidates the hash.
//
// Name and NameKey are both included even though NameKey is derived:
// swapping the language changes Name (and usually Bounds), which SHOULD
// register as a tree change, while a catalog edit that rekeys a caption
// without changing its text should too.
func (t *AccessibilityTree) Hash() string {
	if t == nil {
		return ""
	}
	h := sha256.New()
	fmt.Fprintf(h, "accels=%s\n", strings.Join(t.Accelerators, ","))
	fmt.Fprintf(h, "dpr=%g\n", t.DevicePixelRatio)
	fmt.Fprintf(h, "size=%g,%g\n", t.WindowSize.W, t.WindowSize.H)
	var walk func(n *AXNode)
	walk = func(n *AXNode) {
		if n == nil {
			h.Write([]byte("<nil>\n"))
			return
		}
		fmt.Fprintf(h, "p=%s id=%s r=%s n=%s nk=%s v=%s s=%s k=%s pop=%t vis=%t l=%s b=%g,%g,%g,%g c=%d\n",
			n.Path, n.ID, n.Role, n.Name, n.NameKey, n.Value, n.State, n.Shortcut, n.HasPopup,
			n.Visible, n.Layer, n.Bounds.X, n.Bounds.Y, n.Bounds.W, n.Bounds.H, len(n.Children))
		for _, o := range n.Options {
			fmt.Fprintf(h, "o=%s sel=%t\n", o.Label, o.Selected)
		}
		for _, c := range n.Children {
			walk(c)
		}
	}
	walk(t.Root)
	for _, ov := range t.Overlays {
		walk(ov)
	}
	sum := h.Sum(nil)
	return "ax-" + hex.EncodeToString(sum[:16])
}

// AccessibilityTree builds the AX tree for this window using the
// current laid-out state. Safe to call any time after the first
// Step has produced a layout; before the first Step it returns a
// tree with zero-sized bounds.
//
// The walk is read-only — no layout / repaint side effects — but
// read-only is not the same as concurrency-safe: see
// AccessibilityTreeSynced for off-main-goroutine callers.
func (w *Window) AccessibilityTree() *AccessibilityTree {
	if w == nil {
		return &AccessibilityTree{}
	}
	w.assertUIThread("Window.AccessibilityTree")
	out := &AccessibilityTree{
		Accelerators:     w.AcceleratorShortcuts(),
		DevicePixelRatio: w.DevicePixelRatio(),
		WindowSize:       axRect(w.Bounds()),
	}
	if w.root != nil {
		out.Root = w.buildAXSubtree(w.root, "", "root", true, true)
	}
	modalIdx := -1
	for i := len(w.overlays) - 1; i >= 0; i-- {
		if m, ok := w.overlays[i].(modalOverlay); ok && m.Modal() {
			modalIdx = i
			break
		}
	}
	for i, ov := range w.overlays {
		layer := fmt.Sprintf("overlay[%d]", i)
		if i == modalIdx {
			layer = fmt.Sprintf("modal[%d]", i)
		}
		subtree := w.buildAXSubtree(ov, "", layer, true, true)
		if subtree != nil {
			out.Overlays = append(out.Overlays, subtree)
		}
	}
	return out
}

// AccessibilityTreeSynced is AccessibilityTree serialized through the
// main-thread job queue — the same discipline SnapshotScaled and the
// Click/Type actions use, and the one every agent-goroutine caller
// must follow.
//
// The walk touches live widget state: attributes, child slices, text.
// A frame in progress (or a job-driven re-render) is writing exactly
// that state, so an unsynchronized walk from an HTTP handler is a
// data race — and where the state is a map, Go turns it into a fatal
// "concurrent map read and map write" that takes the process down
// rather than returning a stale value. Waiting for one job costs at
// most a frame.
//
// Do NOT call this from the main goroutine of a live window (event
// handlers, animators, a render pass): it would block waiting for a
// job only that goroutine can run. Use AccessibilityTree there.
func (w *Window) AccessibilityTreeSynced() *AccessibilityTree {
	if w == nil {
		return &AccessibilityTree{}
	}
	var tree *AccessibilityTree
	_ = w.synchronously(func() error {
		tree = w.AccessibilityTree()
		return nil
	})
	if tree == nil {
		return &AccessibilityTree{} // the loop never answered (timeout)
	}
	return tree
}

// buildAXSubtree recursively builds an AXNode for widget under the
// given path prefix. The first call passes "" and the widget is
// labeled as the subtree root; recursive calls extend the path with
// "/TypeName[index]" segments matching collectDebugNodes' scheme.
func (w *Window) buildAXSubtree(widget Widget, parentPath, layer string, isRoot, parentVisible bool) *AXNode {
	if widget == nil {
		return nil
	}
	typeName := widgetTypeName(widget)
	path := typeName
	if !isRoot {
		path = parentPath
	}
	bounds := InteractionBoundsOf(widget)
	visible := parentVisible && !isWidgetHidden(widget) && !bounds.IsEmpty()
	node := &AXNode{
		Path:     path,
		ID:       WidgetID(widget),
		Role:     WidgetRole(widget),
		Name:     WidgetName(widget),
		NameKey:  WidgetNameKey(widget),
		Value:    WidgetValue(widget),
		State:    WidgetAccessibleState(widget, w.focused).String(),
		Shortcut: WidgetAccessibleShortcut(widget),
		HasPopup: WidgetAccessibleHasPopup(widget),
		Bounds:   axRect(bounds),
		Visible:  visible,
		Layer:    layer,
		widget:   widget,
	}
	if ts, ok := WidgetTextState(widget); ok {
		node.TextState = &ts
	}
	if opts := WidgetOptions(widget); len(opts) > 0 {
		node.Options = opts
	}
	// Self-drawn interactive content (AccessibleChildProvider) becomes leaf
	// nodes owned by this widget: they carry their own bounds/role/state, and
	// keep `widget` pointing at the host so actions dispatch a real event
	// through it at the child's rect.
	for i, vc := range WidgetAccessibleChildren(widget) {
		vp := fmt.Sprintf("%s/#%d", path, i)
		if vc.ID != "" {
			vp = fmt.Sprintf("%s/#%s", path, vc.ID)
		}
		node.Children = append(node.Children, &AXNode{
			Path:    vp,
			ID:      vc.ID,
			Role:    vc.Role,
			Name:    vc.Name,
			Value:   vc.Value,
			State:   vc.State.String(),
			Bounds:  axRect(InteractionRectFor(widget, vc.Bounds)),
			Visible: visible && !vc.Bounds.IsEmpty(),
			Layer:   layer,
			widget:  widget,
		})
	}
	if cl, ok := widget.(childLister); ok {
		for i, child := range ChildrenInPaintOrder(cl.ChildList()) {
			childPath := fmt.Sprintf("%s/%s[%d]", path, widgetTypeName(child), i)
			if sub := w.buildAXSubtree(child, childPath, layer, false, visible); sub != nil {
				sub.Path = childPath
				node.Children = append(node.Children, sub)
			}
		}
	}
	return node
}

// Widget returns the live *Widget reference behind this node, or
// nil if the node was constructed in isolation (e.g. via JSON).
func (n *AXNode) Widget() Widget {
	if n == nil {
		return nil
	}
	return n.widget
}

// Flatten returns every node in the tree (Root + every Overlay
// subtree) in pre-order. Exported wrapper for the agent layer's
// path lookup.
func (t *AccessibilityTree) Flatten() []*AXNode { return t.flatten() }

// flattenAX collects every node in a subtree into a flat slice in
// pre-order (parent before children). Used by selector resolution
// where matching against the whole subtree is more natural than
// recursive walking.
func flattenAX(root *AXNode) []*AXNode {
	if root == nil {
		return nil
	}
	out := make([]*AXNode, 0, 16)
	var walk func(*AXNode)
	walk = func(n *AXNode) {
		out = append(out, n)
		for _, c := range n.Children {
			walk(c)
		}
	}
	walk(root)
	return out
}

// flatten returns every node in the tree (Root + every Overlay
// subtree) in pre-order. The order matches a depth-first walk of
// each subtree, with the main root walked first, then overlays in
// stack order (bottom first).
func (t *AccessibilityTree) flatten() []*AXNode {
	if t == nil {
		return nil
	}
	out := flattenAX(t.Root)
	for _, ov := range t.Overlays {
		out = append(out, flattenAX(ov)...)
	}
	return out
}

// topmostModalLayer returns the layer string of the highest-z modal
// overlay, or "" if none. Used by the :layer(modal) pseudo to
// resolve to the right node set.
func (t *AccessibilityTree) topmostModalLayer() string {
	if t == nil {
		return ""
	}
	for i := len(t.Overlays) - 1; i >= 0; i-- {
		if strings.HasPrefix(t.Overlays[i].Layer, "modal[") {
			return t.Overlays[i].Layer
		}
	}
	return ""
}
