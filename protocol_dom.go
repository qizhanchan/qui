package qui

import (
	"fmt"
	"html"
	"strings"
	"sync/atomic"
)

// The DOM domain. One implementation over a pluggable projection: root
// projects the widget tree (an element per widget, its type as the tag), and
// htmlcss registers a projector that shows the app as its author wrote it —
// HTML elements with classes and attributes, plumbing widgets collapsed —
// for windows that have htmlcss content. Either way the node IDs are the
// same NodeIDs the AX tree and selectors use.

// DOM node types (the DOM spec's nodeType values, as CDP uses them).
const (
	DOMElementNode          = 1
	DOMTextNode             = 3
	DOMDocumentNode         = 9
	DOMDocumentFragmentNode = 11
)

// DOMViewNode is one node of a window's DOM projection.
type DOMViewNode struct {
	NodeID    NodeID
	NodeType  int
	LocalName string // "div"; "" for text / document nodes
	NodeValue string // text content of a text node
	// Attributes alternate name, value — CDP's flat form.
	Attributes []string
	Children   []*DOMViewNode
	// AX is the accessibility node behind an element (bounds, role, state,
	// text state, options); nil for text and document nodes.
	AX *AXNode
	// Display is the computed CSS display, when the projector knows it.
	Display string
	// BoxModel, when set, reports the CSS box (border-box rect plus
	// padding / border / margin); otherwise the AX bounds stand in.
	BoxModel func() (borderBox Rect, padding, border, margin Insets)
}

// NodeName is the DOM nodeName: the upper-cased tag, "#text",
// "#document" or "#document-fragment".
func (n *DOMViewNode) NodeName() string {
	switch n.NodeType {
	case DOMTextNode:
		return "#text"
	case DOMDocumentNode:
		return "#document"
	case DOMDocumentFragmentNode:
		return "#document-fragment"
	}
	return strings.ToUpper(n.LocalName)
}

// Attr returns the value of attribute name.
func (n *DOMViewNode) Attr(name string) (string, bool) {
	for i := 0; i+1 < len(n.Attributes); i += 2 {
		if n.Attributes[i] == name {
			return n.Attributes[i+1], true
		}
	}
	return "", false
}

// DOMProjector builds a window's DOM from its already-captured AX tree,
// returning the #document node, or nil to defer to the next projector. It
// runs on the UI goroutine.
type DOMProjector func(w *Window, tree *AccessibilityTree) *DOMViewNode

var domProjector atomic.Pointer[DOMProjector]

// RegisterDOMProjector installs the projector consulted before the
// built-in widget-tree projection. htmlcss registers one from init that
// handles windows with htmlcss content and returns nil for the rest.
func RegisterDOMProjector(p DOMProjector) {
	if p == nil {
		domProjector.Store(nil)
		return
	}
	domProjector.Store(&p)
}

// DOMView projects the window's DOM. UI goroutine only.
func (w *Window) DOMView() *DOMViewNode {
	return w.domViewOf(w.AccessibilityTree())
}

func (w *Window) domViewOf(tree *AccessibilityTree) *DOMViewNode {
	if p := domProjector.Load(); p != nil {
		if doc := (*p)(w, tree); doc != nil {
			return doc
		}
	}
	return widgetDOM(w, tree)
}

// widgetDOM is the default projection: every AX node an element named for
// its widget type, under one #document; overlays become fragments.
func widgetDOM(w *Window, tree *AccessibilityTree) *DOMViewNode {
	doc := &DOMViewNode{NodeID: w.DocumentNodeID(), NodeType: DOMDocumentNode}
	var conv func(n *AXNode) *DOMViewNode
	conv = func(n *AXNode) *DOMViewNode {
		tag := "node"
		if n.widget != nil && WidgetNodeID(n.widget) == n.NodeID {
			tag = strings.ToLower(widgetTypeName(n.widget))
			if i := strings.LastIndexByte(tag, '.'); i >= 0 {
				tag = tag[i+1:]
			}
		}
		e := &DOMViewNode{NodeID: n.NodeID, NodeType: DOMElementNode, LocalName: tag, AX: n}
		if n.ID != "" {
			e.Attributes = append(e.Attributes, "id", n.ID)
		}
		if n.Role != "" {
			e.Attributes = append(e.Attributes, "role", n.Role)
		}
		if n.Name != "" {
			e.Attributes = append(e.Attributes, "aria-label", n.Name)
		}
		if n.Value != "" {
			e.Attributes = append(e.Attributes, "value", n.Value)
		}
		for _, c := range n.Children {
			e.Children = append(e.Children, conv(c))
		}
		return e
	}
	if tree.Root != nil {
		doc.Children = append(doc.Children, conv(tree.Root))
	}
	for _, ov := range tree.Overlays {
		frag := &DOMViewNode{NodeID: WidgetSyntheticNodeID(ov.widget, "#document-fragment"), NodeType: DOMDocumentFragmentNode,
			Attributes: []string{"layer", ov.Layer}}
		frag.Children = []*DOMViewNode{conv(ov)}
		doc.Children = append(doc.Children, frag)
	}
	return doc
}

// find returns the node with id in the subtree, and its parent.
func (n *DOMViewNode) find(id NodeID) (node, parent *DOMViewNode) {
	if n.NodeID == id {
		return n, nil
	}
	for _, c := range n.Children {
		if f, p := c.find(id); f != nil {
			if p == nil {
				p = n
			}
			return f, p
		}
	}
	return nil, nil
}

func (n *DOMViewNode) walk(fn func(*DOMViewNode)) {
	fn(n)
	for _, c := range n.Children {
		c.walk(fn)
	}
}

// voidElements have no closing tag in HTML serialization.
var voidElements = map[string]bool{"area": true, "base": true, "br": true, "col": true, "embed": true,
	"hr": true, "img": true, "input": true, "link": true, "meta": true, "source": true, "track": true, "wbr": true}

// OuterHTML serializes the node and its subtree as HTML — the live tree as
// the app rendered it, for an agent to read back what it built.
func (n *DOMViewNode) OuterHTML() string {
	var b strings.Builder
	n.writeHTML(&b)
	return b.String()
}

func (n *DOMViewNode) writeHTML(b *strings.Builder) {
	switch n.NodeType {
	case DOMTextNode:
		b.WriteString(html.EscapeString(n.NodeValue))
		return
	case DOMDocumentNode, DOMDocumentFragmentNode:
		for _, c := range n.Children {
			c.writeHTML(b)
		}
		return
	}
	b.WriteByte('<')
	b.WriteString(n.LocalName)
	for i := 0; i+1 < len(n.Attributes); i += 2 {
		fmt.Fprintf(b, ` %s="%s"`, n.Attributes[i], html.EscapeString(n.Attributes[i+1]))
	}
	b.WriteByte('>')
	if voidElements[n.LocalName] {
		return
	}
	for _, c := range n.Children {
		c.writeHTML(b)
	}
	b.WriteString("</")
	b.WriteString(n.LocalName)
	b.WriteByte('>')
}

// -------------------------------------------------------------------
// Wire shapes

// DOMNodeJSON is CDP's DOM.Node plus qui's layout/state extensions.
type DOMNodeJSON struct {
	NodeID         NodeID         `json:"nodeId"`
	BackendNodeID  NodeID         `json:"backendNodeId"`
	ParentID       NodeID         `json:"parentId,omitempty"`
	NodeType       int            `json:"nodeType"`
	NodeName       string         `json:"nodeName"`
	LocalName      string         `json:"localName"`
	NodeValue      string         `json:"nodeValue"`
	ChildNodeCount int            `json:"childNodeCount"`
	Children       []*DOMNodeJSON `json:"children,omitempty"`
	Attributes     []string       `json:"attributes,omitempty"`
	DocumentURL    string         `json:"documentURL,omitempty"`
	BaseURL        string         `json:"baseURL,omitempty"`
	// qui extensions.
	Role      string     `json:"role,omitempty"`
	State     string     `json:"state,omitempty"`
	Layer     string     `json:"layer,omitempty"`
	Display   string     `json:"display,omitempty"`
	Bounds    *AXRect    `json:"bounds,omitempty"`
	Visible   *bool      `json:"visible,omitempty"`
	TextState *TextState `json:"textState,omitempty"`
	Options   []AXOption `json:"options,omitempty"`
}

// domJSON serializes n to maxDepth levels of children (< 0 = all).
func domJSON(n *DOMViewNode, parent NodeID, depth, maxDepth int) *DOMNodeJSON {
	j := &DOMNodeJSON{
		NodeID:         n.NodeID,
		BackendNodeID:  n.NodeID,
		ParentID:       parent,
		NodeType:       n.NodeType,
		NodeName:       n.NodeName(),
		LocalName:      n.LocalName,
		NodeValue:      n.NodeValue,
		ChildNodeCount: len(n.Children),
		Attributes:     n.Attributes,
		Display:        n.Display,
	}
	if n.NodeType == DOMElementNode && j.Attributes == nil {
		j.Attributes = []string{}
	}
	if n.NodeType == DOMDocumentNode {
		j.DocumentURL, j.BaseURL = "qui://window", "qui://window"
	}
	if ax := n.AX; ax != nil {
		j.Role, j.State, j.Layer = ax.Role, ax.State, ax.Layer
		b, v := ax.Bounds, ax.Visible
		j.Bounds, j.Visible = &b, &v
		j.TextState, j.Options = ax.TextState, ax.Options
	}
	if maxDepth < 0 || depth < maxDepth {
		j.Children = make([]*DOMNodeJSON, 0, len(n.Children))
		for _, c := range n.Children {
			j.Children = append(j.Children, domJSON(c, n.NodeID, depth+1, maxDepth))
		}
	}
	return j
}

// quad is CDP's 8-number rectangle: four corners clockwise from top-left.
func quad(r Rect) []float32 {
	return []float32{r.X, r.Y, r.X + r.W, r.Y, r.X + r.W, r.Y + r.H, r.X, r.Y + r.H}
}

func inset(r Rect, in Insets) Rect {
	return Rect{X: r.X + in.Left, Y: r.Y + in.Top, W: r.W - in.Left - in.Right, H: r.H - in.Top - in.Bottom}
}

func outset(r Rect, in Insets) Rect {
	return Rect{X: r.X - in.Left, Y: r.Y - in.Top, W: r.W + in.Left + in.Right, H: r.H + in.Top + in.Bottom}
}

type domBoxModel struct {
	Content []float32 `json:"content"`
	Padding []float32 `json:"padding"`
	Border  []float32 `json:"border"`
	Margin  []float32 `json:"margin"`
	Width   float32   `json:"width"`
	Height  float32   `json:"height"`
}

func boxModelOf(n *DOMViewNode) (domBoxModel, bool) {
	var border Rect
	var pad, bw, mar Insets
	switch {
	case n.BoxModel != nil:
		border, pad, bw, mar = n.BoxModel()
	case n.AX != nil:
		border = Rect{X: n.AX.Bounds.X, Y: n.AX.Bounds.Y, W: n.AX.Bounds.W, H: n.AX.Bounds.H}
	default:
		return domBoxModel{}, false
	}
	padding := inset(border, bw)
	content := inset(padding, pad)
	return domBoxModel{
		Content: quad(content), Padding: quad(padding), Border: quad(border), Margin: quad(outset(border, mar)),
		Width: border.W, Height: border.H,
	}, true
}

// -------------------------------------------------------------------
// Commands

type domGetDocumentParams struct {
	Depth  *int `json:"depth,omitempty" desc:"levels of children to include; -1 = whole tree; default 1 (as in CDP)"`
	Pierce bool `json:"pierce,omitempty"`
}

type domDocumentResult struct {
	Root *DOMNodeJSON `json:"root"`
}

type domNodeParams struct {
	NodeID        NodeID `json:"nodeId,omitempty"`
	BackendNodeID NodeID `json:"backendNodeId,omitempty"`
}

func (p domNodeParams) id() NodeID {
	if p.NodeID != 0 {
		return p.NodeID
	}
	return p.BackendNodeID
}

type domDescribeParams struct {
	NodeID        NodeID `json:"nodeId,omitempty"`
	BackendNodeID NodeID `json:"backendNodeId,omitempty"`
	Depth         *int   `json:"depth,omitempty" desc:"levels of children; -1 = all; default 0"`
	Pierce        bool   `json:"pierce,omitempty"`
}

type domNodeResult struct {
	Node *DOMNodeJSON `json:"node"`
}

type domQueryParams struct {
	NodeID   NodeID `json:"nodeId,omitempty" desc:"scope: search this node's subtree; 0 or the document = everything"`
	Selector string `json:"selector" desc:"CSS or widget selector, or @ref"`
}

type domQueryResult struct {
	NodeID NodeID `json:"nodeId" desc:"0 when nothing matched (as in CDP)"`
}

type domQueryAllResult struct {
	NodeIDs []NodeID `json:"nodeIds"`
}

type domBoxResult struct {
	Model domBoxModel `json:"model"`
}

type domLocationParams struct {
	X float32 `json:"x"`
	Y float32 `json:"y"`
}

type domLocationResult struct {
	BackendNodeID NodeID `json:"backendNodeId"`
	NodeID        NodeID `json:"nodeId"`
}

type domOuterHTMLResult struct {
	OuterHTML string `json:"outerHTML"`
}

type domAttributesResult struct {
	Attributes []string `json:"attributes"`
}

type domRequestChildNodesParams struct {
	NodeID NodeID `json:"nodeId"`
	Depth  *int   `json:"depth,omitempty"`
	Pierce bool   `json:"pierce,omitempty"`
}

type domSetChildNodes struct {
	ParentID NodeID         `json:"parentId"`
	Nodes    []*DOMNodeJSON `json:"nodes"`
}

// withDOM captures the AX tree and DOM view on the UI goroutine and runs fn
// there.
func withDOM(w *Window, fn func(tree *AccessibilityTree, doc *DOMViewNode) error) error {
	return w.synchronously(func() error {
		tree := w.AccessibilityTree()
		return fn(tree, w.domViewOf(tree))
	})
}

func domLookup(doc *DOMViewNode, id NodeID) (*DOMViewNode, *DOMViewNode, error) {
	if id == 0 {
		return nil, nil, &ProtocolError{Code: ProtocolInvalidParams, Reason: "invalid_params", Message: "nodeId (or backendNodeId) is required"}
	}
	n, p := doc.find(id)
	if n == nil {
		return nil, nil, fmt.Errorf("%w: %s", ErrStaleNode, id)
	}
	return n, p, nil
}

// domMatches resolves selector (any grammar FindNodes accepts) to DOM nodes
// under scope, in document order. An AX match that is not itself in the DOM
// view (a backing widget inside an element) maps to its nearest projected
// ancestor.
func domMatches(w *Window, tree *AccessibilityTree, doc *DOMViewNode, scope NodeID, selector string) ([]NodeID, error) {
	nodes, err := w.findNodesIn(tree, selector)
	if err != nil {
		return nil, err
	}
	root := doc
	if scope != 0 && scope != doc.NodeID {
		if root, _, err = domLookup(doc, scope); err != nil {
			return nil, err
		}
	}
	inScope := map[NodeID]bool{}
	root.walk(func(n *DOMViewNode) {
		if n != root {
			inScope[n.NodeID] = true
		}
	})
	seen := map[NodeID]bool{}
	var out []NodeID
	for _, n := range nodes {
		id := n.NodeID
		if !inScope[id] {
			for p := n.widget; p != nil; p = p.Parent() {
				if pid := WidgetNodeID(p); inScope[pid] {
					id = pid
					break
				}
			}
		}
		if inScope[id] && !seen[id] {
			seen[id] = true
			out = append(out, id)
		}
	}
	return out, nil
}

func depthOr(d *int, def int) int {
	if d == nil {
		return def
	}
	return *d
}

func domDomain() ProtocolDomain {
	return ProtocolDomain{
		Name: "DOM",
		Description: "The window as a DOM. For htmlcss content: HTML elements with classes and attributes; otherwise one element per widget. " +
			"nodeId == backendNodeId == the qui NodeID (also @<id> in selectors). Selectors accept CSS, the widget grammar ([role=…][name=…]) and @refs.",
		Commands: []ProtocolCommand{
			NewProtocolCommand("getDocument", "The #document node, with children to depth (default 1; -1 = the whole tree).",
				func(ctx *ProtocolContext, p domGetDocumentParams) (out domDocumentResult, err error) {
					err = withDOM(ctx.Window, func(_ *AccessibilityTree, doc *DOMViewNode) error {
						out.Root = domJSON(doc, 0, 0, depthOr(p.Depth, 1))
						return nil
					})
					return out, err
				}),
			NewProtocolCommand("requestChildNodes", "Emit DOM.setChildNodes with the children of nodeId (DevTools-style lazy expansion).",
				func(ctx *ProtocolContext, p domRequestChildNodesParams) (empty, error) {
					var ev domSetChildNodes
					err := withDOM(ctx.Window, func(_ *AccessibilityTree, doc *DOMViewNode) error {
						n, _, err := domLookup(doc, p.NodeID)
						if err != nil {
							return err
						}
						ev.ParentID = n.NodeID
						depth := depthOr(p.Depth, 1)
						sub := depth - 1
						if depth < 0 {
							sub = -1
						}
						ev.Nodes = []*DOMNodeJSON{}
						for _, c := range n.Children {
							ev.Nodes = append(ev.Nodes, domJSON(c, n.NodeID, 0, sub))
						}
						return nil
					})
					if err == nil && ctx.Session != nil {
						ctx.Session.Emit("DOM.setChildNodes", ev)
					}
					return empty{}, err
				}),
			NewProtocolCommand("describeNode", "One node, with children to depth (default 0).",
				func(ctx *ProtocolContext, p domDescribeParams) (out domNodeResult, err error) {
					err = withDOM(ctx.Window, func(_ *AccessibilityTree, doc *DOMViewNode) error {
						id := domNodeParams{p.NodeID, p.BackendNodeID}.id()
						n, parent, err := domLookup(doc, id)
						if err != nil {
							return err
						}
						var pid NodeID
						if parent != nil {
							pid = parent.NodeID
						}
						out.Node = domJSON(n, pid, 0, depthOr(p.Depth, 0))
						return nil
					})
					return out, err
				}),
			NewProtocolCommand("querySelector", "The first node matching selector within nodeId's subtree; nodeId 0 when none.",
				func(ctx *ProtocolContext, p domQueryParams) (out domQueryResult, err error) {
					err = withDOM(ctx.Window, func(tree *AccessibilityTree, doc *DOMViewNode) error {
						ids, err := domMatches(ctx.Window, tree, doc, p.NodeID, p.Selector)
						if len(ids) > 0 {
							out.NodeID = ids[0]
						}
						return err
					})
					return out, err
				}),
			NewProtocolCommand("querySelectorAll", "Every node matching selector within nodeId's subtree, in document order.",
				func(ctx *ProtocolContext, p domQueryParams) (out domQueryAllResult, err error) {
					out.NodeIDs = []NodeID{}
					err = withDOM(ctx.Window, func(tree *AccessibilityTree, doc *DOMViewNode) error {
						ids, err := domMatches(ctx.Window, tree, doc, p.NodeID, p.Selector)
						if ids != nil {
							out.NodeIDs = ids
						}
						return err
					})
					return out, err
				}),
			NewProtocolCommand("getBoxModel", "Content / padding / border / margin quads, in logical window coordinates.",
				func(ctx *ProtocolContext, p domNodeParams) (out domBoxResult, err error) {
					err = withDOM(ctx.Window, func(_ *AccessibilityTree, doc *DOMViewNode) error {
						n, _, err := domLookup(doc, p.id())
						if err != nil {
							return err
						}
						m, ok := boxModelOf(n)
						if !ok {
							return NewProtocolError("error", "node %s has no box", n.NodeID)
						}
						out.Model = m
						return nil
					})
					return out, err
				}),
			NewProtocolCommand("getNodeForLocation", "The innermost DOM node at a window point.",
				func(ctx *ProtocolContext, p domLocationParams) (out domLocationResult, err error) {
					err = withDOM(ctx.Window, func(_ *AccessibilityTree, doc *DOMViewNode) error {
						hit := ctx.Window.hitTestAll(Point{X: p.X, Y: p.Y})
						for wd := hit; wd != nil; wd = wd.Parent() {
							if n, _ := doc.find(WidgetNodeID(wd)); n != nil && n != doc {
								out.NodeID, out.BackendNodeID = n.NodeID, n.NodeID
								return nil
							}
						}
						return NewProtocolError("no_match", "no node at (%g, %g)", p.X, p.Y)
					})
					return out, err
				}),
			NewProtocolCommand("getOuterHTML", "The node serialized as HTML — read back exactly what the app rendered.",
				func(ctx *ProtocolContext, p domNodeParams) (out domOuterHTMLResult, err error) {
					err = withDOM(ctx.Window, func(_ *AccessibilityTree, doc *DOMViewNode) error {
						n := doc
						if id := p.id(); id != 0 {
							if n, _, err = domLookup(doc, id); err != nil {
								return err
							}
						}
						out.OuterHTML = n.OuterHTML()
						return nil
					})
					return out, err
				}),
			NewProtocolCommand("getAttributes", "The node's attributes as a flat name, value list.",
				func(ctx *ProtocolContext, p domNodeParams) (out domAttributesResult, err error) {
					err = withDOM(ctx.Window, func(_ *AccessibilityTree, doc *DOMViewNode) error {
						n, _, err := domLookup(doc, p.id())
						if err == nil {
							out.Attributes = append([]string{}, n.Attributes...)
						}
						return err
					})
					return out, err
				}),
			NewProtocolCommand("scrollIntoViewIfNeeded", "Scroll the node's nearest scrolling ancestor so it is visible.",
				func(ctx *ProtocolContext, p domNodeParams) (empty, error) {
					return empty{}, ctx.Window.synchronously(func() error {
						n, err := ctx.Window.NodeByID(p.id())
						if err != nil {
							return err
						}
						ctx.Window.scrollAncestorsIntoView(n.widget)
						return nil
					})
				}),
			NewProtocolCommand("focus", "Give the node keyboard focus (a form element hands it to its control).",
				func(ctx *ProtocolContext, p domNodeParams) (empty, error) {
					return empty{}, ctx.Window.Focus(p.id().String())
				}),
		},
		Events: []ProtocolEvent{
			NewProtocolEvent[domSetChildNodes]("setChildNodes", "Children of a node, sent in answer to requestChildNodes."),
			NewProtocolEvent[empty]("documentUpdated", "The DOM changed structurally; re-fetch with getDocument. Sent while a session has DOM enabled."),
		},
	}
}
