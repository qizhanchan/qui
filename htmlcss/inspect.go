package htmlcss

// DOM inspection surface: an HTML/DevTools-shaped projection of a live
// htmlcss element tree, for the HTML-level agent debugging layer.
//
// The widget-level agent (root qui: accessibility tree + widget-selector
// grammar + actions) addresses the RENDERED widget tree — which, for an
// htmlcss/reactive app, diverges from the HTML the developer wrote:
// inline runs fold into InlineBox atoms, form controls render through
// backing widgets, overflow hosts a ScrollView, <li> becomes a
// [marker|content] row. Debugging business UI against that tree means
// re-deriving the DOM↔widget mapping by hand, and there is no way to see
// the class list or the computed CSS — the two things HTML debugging most
// needs.
//
// This file projects the same live tree back into a DOM the way a browser
// DevTools "Elements" panel shows it, addressable with real CSS selectors
// (class, tag, combinators — matched against each El's retained *Node
// mirror using the engine's own selector matcher) and enriched with the
// computed style + matched-rule provenance the engine already retains
// (El.lastCS + the StyleEngine stylesheet). It is additive: the widget
// layer is unchanged and remains the tool for low-level render debugging.

import (
	"fmt"
	"sort"
	"strings"

	"github.com/qizhanchan/qui"
)

// DOMNode is one element in the projected DOM tree — the DevTools
// "Elements" view of a live El. Plumbing widgets (InlineBox, ScrollView,
// backing controls, marker labels) are collapsed away: a DOMNode maps
// 1:1 to an htmlcss element.
type DOMNode struct {
	Path    string            `json:"path"`         // positional path, e.g. "div/ul/li[2]"
	Tag     string            `json:"tag"`          // element tag, or "#text" / "#document"
	ID      string            `json:"id,omitempty"` // id attribute
	Classes []string          `json:"classes,omitempty"`
	Attrs   map[string]string `json:"attrs,omitempty"` // remaining attrs (id/class/style excluded)
	Text    string            `json:"text,omitempty"`  // own text content (leaf / #text)
	Role    string            `json:"role,omitempty"`
	Display string            `json:"display,omitempty"` // computed display
	State   string            `json:"state,omitempty"`   // AX state bits (focused/disabled/checked/…)
	Bounds  qui.Rect          `json:"bounds"`
	Visible bool              `json:"visible"`
	Layer   string            `json:"layer,omitempty"` // "root" | "overlay[N]" | "modal[N]"
	// TextState is present for <input>/<textarea>: caret / selection / IME
	// preedit / undo availability, read from the backing edit widget — so
	// input behavior is debuggable at the HTML level.
	TextState *qui.TextState `json:"textState,omitempty"`
	// Options is present for <select>: the option set + current selection,
	// read from the backing choice widget — so the choices are inspectable
	// at the HTML level without opening the dropdown.
	Options  []qui.AXOption `json:"options,omitempty"`
	Children []*DOMNode     `json:"children,omitempty"`

	// el is the live element behind this node, used by Query matching,
	// Styles resolution and action targeting. Not serialized.
	el *El
}

// DOMTree is the projected DOM for a window: the main document plus one
// synthetic document fragment per overlay (dialog / portal).
type DOMTree struct {
	Root             *DOMNode   `json:"root"`
	Overlays         []*DOMNode `json:"overlays,omitempty"`
	DevicePixelRatio float32    `json:"devicePixelRatio"`
	WindowSize       qui.Rect   `json:"windowSize"`
}

// InspectWindow projects the window's live htmlcss element tree into a
// DOMTree. It walks the accessibility tree (the single source of truth
// for structure, bounds, state and overlays) and keeps only the El
// nodes, reparenting El descendants across any non-El plumbing to their
// nearest El ancestor. Windows with no htmlcss content yield a tree
// whose #document root simply has no children.
func InspectWindow(w *qui.Window) *DOMTree {
	if w == nil {
		return &DOMTree{}
	}
	ax := w.AccessibilityTreeSynced()
	out := &DOMTree{
		DevicePixelRatio: ax.DevicePixelRatio,
		WindowSize:       qui.Rect{X: ax.WindowSize.X, Y: ax.WindowSize.Y, W: ax.WindowSize.W, H: ax.WindowSize.H},
	}
	out.Root = &DOMNode{Tag: "#document", Layer: "root", Visible: true}
	out.Root.Children = domForest(ax.Root, "root")
	assignPaths(out.Root, "")
	for _, ov := range ax.Overlays {
		layer := ov.Layer
		frag := &DOMNode{Tag: "#document-fragment", Layer: layer, Visible: true}
		frag.Children = domForest(ov, layer)
		assignPaths(frag, "")
		out.Overlays = append(out.Overlays, frag)
	}
	return out
}

// domForest walks an AX subtree and returns the top-level DOMNodes found
// (El widgets), with El descendants nested beneath them regardless of any
// intervening non-El widgets.
func domForest(ax *qui.AXNode, layer string) []*DOMNode {
	var roots []*DOMNode
	var walk func(n *qui.AXNode, parent *DOMNode)
	walk = func(n *qui.AXNode, parent *DOMNode) {
		if n == nil {
			return
		}
		cur := parent
		if el, ok := n.Widget().(*El); ok {
			d := projectEl(el, n, layer)
			if parent != nil {
				parent.Children = append(parent.Children, d)
			} else {
				roots = append(roots, d)
			}
			cur = d
		}
		for _, c := range n.Children {
			walk(c, cur)
		}
	}
	walk(ax, nil)
	return roots
}

// projectEl builds a DOMNode from a live El, taking structural facts
// (bounds, visibility, state) from the AX node so the DOM view stays
// consistent with the widget-level /tree, and DOM facts (tag, classes,
// attributes, text, computed display) from the element itself.
func projectEl(el *El, ax *qui.AXNode, layer string) *DOMNode {
	d := &DOMNode{
		Tag:     el.tag,
		Role:    ax.Role,
		State:   ax.State,
		Bounds:  qui.Rect{X: ax.Bounds.X, Y: ax.Bounds.Y, W: ax.Bounds.W, H: ax.Bounds.H},
		Visible: ax.Visible,
		Layer:   layer,
		el:      el,
	}
	if el.node != nil {
		d.ID = el.node.ID()
		d.Classes = el.node.Classes()
		// A snapshot, not the live map: this walk runs on the agent's
		// goroutine while the app keeps re-rendering (see El.attrMu).
		for k, v := range el.attrSnapshot() {
			if k == "id" || k == "class" || k == "style" {
				continue
			}
			if d.Attrs == nil {
				d.Attrs = map[string]string{}
			}
			d.Attrs[k] = v
		}
	}
	if el.isTextSeg() {
		d.Text = collapseText(el.text)
	} else if len(el.elementKids) == 0 && el.text != "" {
		d.Text = el.text
	}
	if el.lastCS != nil {
		d.Display = el.lastCS.Display
	}
	// Form controls render through a backing edit widget that the
	// projection collapses away; surface its editing state on the
	// element's own DOM node. Read it from the AX subtree (already
	// computed, and populated regardless of how the control was
	// constructed) rather than el.backing, so /dom and /tree agree.
	d.TextState = backingTextState(ax)
	d.Options = backingOptions(ax)
	if el.displayNone {
		d.Visible = false
		d.Display = "none"
	}
	return d
}

// backingTextState finds this element's own backing edit widget's
// TextState by scanning the AX subtree, stopping at any nested element
// so a child <input>'s state is never pulled onto an ancestor.
func backingTextState(ax *qui.AXNode) *qui.TextState {
	for _, c := range ax.Children {
		if _, isEl := c.Widget().(*El); isEl {
			continue // a nested element owns its own backing
		}
		if c.TextState != nil {
			return c.TextState
		}
		if ts := backingTextState(c); ts != nil {
			return ts
		}
	}
	return nil
}

// backingOptions finds this element's own backing choice widget's option
// set by scanning the AX subtree, stopping at any nested element so a
// child <select>'s options are never pulled onto an ancestor. Mirrors
// backingTextState.
func backingOptions(ax *qui.AXNode) []qui.AXOption {
	for _, c := range ax.Children {
		if _, isEl := c.Widget().(*El); isEl {
			continue // a nested element owns its own backing
		}
		if len(c.Options) > 0 {
			return c.Options
		}
		if o := backingOptions(c); len(o) > 0 {
			return o
		}
	}
	return nil
}

// assignPaths fills each node's positional Path: the parent path plus the
// node's tag, suffixed with [n] when it isn't the only element of that
// tag among its siblings (DevTools-style, 1-indexed among same-tag peers).
func assignPaths(n *DOMNode, prefix string) {
	byTag := map[string]int{}
	total := map[string]int{}
	for _, c := range n.Children {
		total[c.Tag]++
	}
	for _, c := range n.Children {
		seg := c.Tag
		if total[c.Tag] > 1 {
			byTag[c.Tag]++
			seg = fmt.Sprintf("%s[%d]", c.Tag, byTag[c.Tag])
		}
		if prefix == "" {
			c.Path = seg
		} else {
			c.Path = prefix + "/" + seg
		}
		assignPaths(c, c.Path)
	}
}

// flatten returns every DOMNode in the tree (main document then each
// overlay fragment) in document order, skipping the synthetic #document
// wrapper nodes.
func (t *DOMTree) flatten() []*DOMNode {
	var out []*DOMNode
	var walk func(n *DOMNode)
	walk = func(n *DOMNode) {
		if n.el != nil {
			out = append(out, n)
		}
		for _, c := range n.Children {
			walk(c)
		}
	}
	if t.Root != nil {
		walk(t.Root)
	}
	for _, ov := range t.Overlays {
		walk(ov)
	}
	return out
}

// Query returns the DOMNodes matching a CSS selector, in document order.
// The full engine selector grammar applies (tag, #id, .class, [attr],
// :nth-child, combinators > + ~, descendant), matched against each
// element's retained *Node mirror — i.e. the exact structure the cascade
// sees. Interactive-state pseudos (:hover/:focus/:active) are evaluated
// at the resting state and so match nothing here; query the state via the
// State field instead.
//
// Additionally supports the Playwright-style text locator :has-text("…"):
// the matched (subject) element is kept only when its text content — its
// own text plus every descendant's, whitespace-normalized — contains the
// given string (case-insensitive substring). Multiple :has-text clauses
// AND together. So `button:has-text("Save")` finds the Save button
// regardless of class/id. NOTE: :has-text filters the SUBJECT element; it
// is not (yet) a per-compound constraint, so prefer putting it on the
// element you want to match.
func (t *DOMTree) Query(selector string) ([]*DOMNode, error) {
	cleaned, needles := extractHasText(selector)
	sels := parseSelectorList(cleaned)
	if len(sels) == 0 {
		return nil, fmt.Errorf("htmlcss: empty or unparseable selector %q", selector)
	}
	var out []*DOMNode
	for _, d := range t.flatten() {
		if d.el == nil || d.el.node == nil {
			continue
		}
		matched := false
		for _, s := range sels {
			if s.matches(d.el.node, selectorState{}) {
				matched = true
				break
			}
		}
		if !matched || !matchesText(d, needles) {
			continue
		}
		out = append(out, d)
	}
	// With a text locator, keep only the innermost matches — an ancestor's
	// text content includes its descendants', so a bare or loosely-qualified
	// :has-text would otherwise also match every enclosing container and an
	// action would resolve to the outermost one. This mirrors Playwright's
	// text-engine "smallest enclosing element" behavior.
	if len(needles) > 0 && len(out) > 1 {
		set := make(map[*Node]bool, len(out))
		for _, d := range out {
			set[d.el.node] = true
		}
		inner := out[:0:0]
		for _, d := range out {
			if !nodeHasMatchedDescendant(d.el.node, set) {
				inner = append(inner, d)
			}
		}
		out = inner
	}
	return out, nil
}

// nodeHasMatchedDescendant reports whether any strict descendant of n is
// present in set.
func nodeHasMatchedDescendant(n *Node, set map[*Node]bool) bool {
	for _, c := range n.Children {
		if set[c] || nodeHasMatchedDescendant(c, set) {
			return true
		}
	}
	return false
}

// extractHasText pulls every :has-text("…") / :has-text('…') / :has-text(…)
// clause out of a selector, returning the selector with those clauses
// removed and the list of required substrings. An empty remainder means
// the selector was ONLY text locators, which we treat as "any element"
// (*), so `:has-text("Save")` alone matches any element containing "Save".
func extractHasText(sel string) (string, []string) {
	const tok = ":has-text("
	var needles []string
	var out strings.Builder
	i := 0
	for i < len(sel) {
		if i+len(tok) <= len(sel) && strings.EqualFold(sel[i:i+len(tok)], tok) {
			j := i + len(tok)
			for j < len(sel) && sel[j] == ' ' {
				j++
			}
			if j < len(sel) && (sel[j] == '"' || sel[j] == '\'') {
				q := sel[j]
				j++
				start := j
				for j < len(sel) && sel[j] != q {
					j++
				}
				needles = append(needles, sel[start:j])
				if j < len(sel) {
					j++ // closing quote
				}
			} else {
				start := j
				for j < len(sel) && sel[j] != ')' {
					j++
				}
				needles = append(needles, strings.TrimSpace(sel[start:j]))
			}
			for j < len(sel) && sel[j] != ')' {
				j++
			}
			if j < len(sel) {
				j++ // closing paren
			}
			i = j
			continue
		}
		out.WriteByte(sel[i])
		i++
	}
	cleaned := strings.TrimSpace(out.String())
	if cleaned == "" {
		cleaned = "*"
	}
	return cleaned, needles
}

// matchesText reports whether d's normalized text content contains every
// needle (case-insensitive). No needles → always true.
func matchesText(d *DOMNode, needles []string) bool {
	if len(needles) == 0 {
		return true
	}
	hay := strings.ToLower(normalizeWS(d.textContent()))
	for _, n := range needles {
		if !strings.Contains(hay, strings.ToLower(normalizeWS(n))) {
			return false
		}
	}
	return true
}

// textContent concatenates the element's own text and all descendants',
// space-separated (DOM textContent, modulo whitespace normalization by
// the caller).
func (d *DOMNode) textContent() string {
	var sb strings.Builder
	var walk func(n *DOMNode)
	walk = func(n *DOMNode) {
		if n.Text != "" {
			sb.WriteString(n.Text)
			sb.WriteByte(' ')
		}
		for _, c := range n.Children {
			walk(c)
		}
	}
	walk(d)
	return sb.String()
}

// normalizeWS collapses runs of whitespace to single spaces and trims,
// so text matching ignores markup indentation / line breaks.
func normalizeWS(s string) string { return strings.Join(strings.Fields(s), " ") }

// ResolveWidget maps a CSS selector to a single live widget for action
// targeting, preferring the first visible match (falling back to the
// first match of any). Returns qui.ErrNoMatch when nothing matches.
func (t *DOMTree) ResolveWidget(selector string) (qui.Widget, *DOMNode, error) {
	nodes, err := t.Query(selector)
	if err != nil {
		return nil, nil, err
	}
	for _, n := range nodes {
		if n.Visible {
			return n.el, n, nil
		}
	}
	if len(nodes) > 0 {
		return nodes[0].el, nodes[0], nil
	}
	return nil, nil, qui.ErrNoMatch
}

// EditableTarget unwraps a form-control element to the backing widget that
// should receive text actions (type / focus / key input): a text
// <input>/<textarea> edits through an Input/TextArea, a <select> picks
// through a Select. The element itself only owns styling and identity, so
// addressing it for a text action has to hop to the control — see
// El.TextTarget, which the root action layer applies on its own; this is
// the same hop for callers that need the resolved widget up front (focus
// before a key chord). Returns w unchanged for elements with no such
// control (buttons, checkboxes, plain elements).
func EditableTarget(w qui.Widget) qui.Widget {
	if el, ok := w.(*El); ok {
		if inner := el.TextTarget(); inner != nil {
			return inner
		}
	}
	return w
}

// DeclInfo is one CSS declaration within a matched rule. Active is false
// when a higher-cascade declaration overrides this property (DevTools
// renders these struck-through).
type DeclInfo struct {
	Property  string `json:"property"`
	Value     string `json:"value"`
	Important bool   `json:"important,omitempty"`
	Active    bool   `json:"active"`
}

// MatchedRule is a stylesheet rule that matched the element, with its
// origin and specificity so the cascade winner is explainable.
type MatchedRule struct {
	Selector     string     `json:"selector"`
	Origin       string     `json:"origin"` // "user-agent" | "author" | "inline"
	Specificity  [3]int     `json:"specificity,omitempty"`
	Declarations []DeclInfo `json:"declarations"`
}

// BoxModel is the resolved CSS box model in logical pixels, mirroring the
// DevTools box-model diagram.
type BoxModel struct {
	Content qui.Rect   `json:"content"` // border-box bounds (window coords)
	Padding qui.Insets `json:"padding"`
	Border  qui.Insets `json:"border"`
	Margin  qui.Insets `json:"margin"`
}

// ElementStyles is the "Styles" + "Computed" DevTools view for one
// element: the winning declaration map, every rule that matched (most
// specific first, inline highest), and the box model.
type ElementStyles struct {
	Path         string            `json:"path"`
	Selector     string            `json:"selector"` // tag#id.class descriptor of the element
	Computed     map[string]string `json:"computed"` // final cascade: property → winning value
	MatchedRules []MatchedRule     `json:"matchedRules"`
	BoxModel     BoxModel          `json:"boxModel"`
}

// Styles resolves the cascade for every element matching selector and
// returns the DevTools-style styles view for each.
func (t *DOMTree) Styles(selector string) ([]*ElementStyles, error) {
	nodes, err := t.Query(selector)
	if err != nil {
		return nil, err
	}
	out := make([]*ElementStyles, 0, len(nodes))
	for _, n := range nodes {
		out = append(out, n.styles())
	}
	return out, nil
}

func (d *DOMNode) styles() *ElementStyles {
	es := &ElementStyles{
		Path:     d.Path,
		Selector: d.descriptor(),
		Computed: map[string]string{},
	}
	el := d.el
	if el == nil || el.node == nil {
		return es
	}
	var sheet *Stylesheet
	if el.engine != nil {
		sheet = el.engine.sheet
	}
	if sheet != nil {
		es.Computed = mergedDecls(el.node, sheet, selectorState{})
		es.MatchedRules = matchedRulesFor(el.node, sheet, es.Computed)
	}
	es.BoxModel = boxModelFor(d)
	return es
}

// Selector renders a compact CSS-ish identity for the element, e.g.
// "button#save.primary" — a human/agent-readable label, not guaranteed
// unique. Exported for the agent layer's action-result reporting.
func (d *DOMNode) Selector() string { return d.descriptor() }

// descriptor renders a compact CSS-ish identity for the element, e.g.
// "button#save.primary".
func (d *DOMNode) descriptor() string {
	var sb strings.Builder
	sb.WriteString(d.Tag)
	if d.ID != "" {
		sb.WriteString("#" + d.ID)
	}
	for _, c := range d.Classes {
		sb.WriteString("." + c)
	}
	return sb.String()
}

// matchedRulesFor collects the UA + author + inline rules that apply to n,
// ordered the way DevTools stacks them (inline first, then author by
// descending specificity/order, then user-agent). Each declaration is
// flagged Active when its value equals the cascade winner for that
// property.
func matchedRulesFor(n *Node, sheet *Stylesheet, winning map[string]string) []MatchedRule {
	var author []MatchedRule
	for _, rule := range sheet.Rules {
		best := -1
		var bestSel Selector
		var ba, bb, bc int
		for _, sel := range rule.Selectors {
			if sel.matches(n, selectorState{}) {
				a, b, c := sel.specificity()
				if score := a*10000 + b*100 + c; score > best {
					best, bestSel, ba, bb, bc = score, sel, a, b, c
				}
			}
		}
		if best < 0 {
			continue
		}
		author = append(author, MatchedRule{
			Selector:     selectorText(bestSel),
			Origin:       "author",
			Specificity:  [3]int{ba, bb, bc},
			Declarations: declInfos(rule.Declarations, winning),
		})
	}
	// Most specific author rules first; stable so source order breaks ties.
	sort.SliceStable(author, func(i, j int) bool {
		si := author[i].Specificity
		sj := author[j].Specificity
		return si[0]*10000+si[1]*100+si[2] > sj[0]*10000+sj[1]*100+sj[2]
	})

	var out []MatchedRule
	if inline, ok := n.Attr("style"); ok {
		if decls := parseDeclarations(inline); len(decls) > 0 {
			out = append(out, MatchedRule{
				Selector:     "element.style",
				Origin:       "inline",
				Declarations: declInfos(decls, winning),
			})
		}
	}
	out = append(out, author...)

	uaTag := n.Tag
	if n.Tag == "input" {
		switch n.AttrOr("type", "") {
		case "submit", "reset", "button", "file", "color":
			uaTag = "button"
		}
	}
	if ua := uaDeclarations(uaTag); len(ua) > 0 {
		out = append(out, MatchedRule{
			Selector:     n.Tag,
			Origin:       "user-agent",
			Declarations: declInfos(ua, winning),
		})
	}
	return out
}

func declInfos(decls []Declaration, winning map[string]string) []DeclInfo {
	out := make([]DeclInfo, 0, len(decls))
	for _, d := range decls {
		out = append(out, DeclInfo{
			Property:  d.Property,
			Value:     d.Value,
			Important: d.Important,
			Active:    winning[d.Property] == d.Value,
		})
	}
	return out
}

func boxModelFor(d *DOMNode) BoxModel {
	bm := BoxModel{Content: d.Bounds}
	if d.el != nil && d.el.lastCS != nil {
		cs := d.el.lastCS
		bm.Padding = cs.Padding
		bm.Margin = cs.Margin
		if cs.HasSideBorders {
			bm.Border = cs.SideWidths
		} else if cs.BorderWidth > 0 {
			bm.Border = qui.Insets{Top: cs.BorderWidth, Right: cs.BorderWidth, Bottom: cs.BorderWidth, Left: cs.BorderWidth}
		}
	}
	return bm
}

// selectorText renders a parsed Selector back to readable CSS. Used for
// matched-rule provenance in the styles view.
func selectorText(s Selector) string {
	var sb strings.Builder
	for i, part := range s.parts {
		if i > 0 {
			switch part.comb {
			case combChild:
				sb.WriteString(" > ")
			case combAdjacent:
				sb.WriteString(" + ")
			case combSibling:
				sb.WriteString(" ~ ")
			default:
				sb.WriteString(" ")
			}
		}
		sb.WriteString(simpleSelectorText(part.sel))
	}
	return sb.String()
}

func simpleSelectorText(ss simpleSelector) string {
	var sb strings.Builder
	if ss.tag != "" && ss.tag != "*" {
		sb.WriteString(ss.tag)
	} else if ss.id == "" && len(ss.classes) == 0 && len(ss.attrs) == 0 && len(ss.pseudos) == 0 && ss.pseudoElem == "" {
		sb.WriteString("*")
	}
	if ss.id != "" {
		sb.WriteString("#" + ss.id)
	}
	for _, c := range ss.classes {
		sb.WriteString("." + c)
	}
	for _, a := range ss.attrs {
		sb.WriteString(attrSelectorText(a))
	}
	for _, p := range ss.pseudos {
		if p.raw != "" {
			sb.WriteString(":" + p.raw)
		}
	}
	if ss.pseudoElem != "" {
		sb.WriteString("::" + ss.pseudoElem)
	}
	return sb.String()
}

func attrSelectorText(a attrSelector) string {
	op := ""
	switch a.op {
	case attrEquals:
		op = "="
	case attrPrefix:
		op = "^="
	case attrSuffix:
		op = "$="
	case attrSubstr:
		op = "*="
	case attrWord:
		op = "~="
	case attrDashHyph:
		op = "|="
	}
	if a.op == attrExists {
		return "[" + a.name + "]"
	}
	suffix := ""
	if a.ci {
		suffix = " i"
	}
	return fmt.Sprintf("[%s%s%q%s]", a.name, op, a.val, suffix)
}
