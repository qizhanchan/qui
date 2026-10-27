package htmlcss

import (
	"sort"
	"strings"

	"github.com/qizhanchan/qui"
)

// htmlcss's part of the inspection protocol: the DOM projection (so the
// protocol's DOM domain shows HTML elements, classes and attributes instead
// of widget types for windows with htmlcss content) and the CSS domain (the
// DevTools Styles / Computed panes).

func init() {
	qui.RegisterDOMProjector(projectDOMView)
	qui.RegisterProtocolDomain(cssDomain())
}

// projectDOMView is the htmlcss DOM projector. It declines (nil) windows
// with no htmlcss content so the widget-tree projection covers them.
func projectDOMView(w *qui.Window, tree *qui.AccessibilityTree) *qui.DOMViewNode {
	dom := projectTree(w, tree)
	has := len(dom.Root.Children) > 0
	for _, ov := range dom.Overlays {
		has = has || len(ov.Children) > 0
	}
	if !has {
		return nil
	}
	doc := &qui.DOMViewNode{NodeID: dom.Root.NodeID, NodeType: qui.DOMDocumentNode}
	for _, c := range dom.Root.Children {
		doc.Children = append(doc.Children, domViewOf(c))
	}
	for _, ov := range dom.Overlays {
		frag := &qui.DOMViewNode{NodeID: ov.NodeID, NodeType: qui.DOMDocumentFragmentNode, Attributes: []string{"layer", ov.Layer}}
		for _, c := range ov.Children {
			frag.Children = append(frag.Children, domViewOf(c))
		}
		doc.Children = append(doc.Children, frag)
	}
	return doc
}

func domViewOf(d *DOMNode) *qui.DOMViewNode {
	if d.el != nil && d.el.isTextSeg() {
		return &qui.DOMViewNode{NodeID: d.NodeID, NodeType: qui.DOMTextNode, NodeValue: d.Text}
	}
	v := &qui.DOMViewNode{NodeID: d.NodeID, NodeType: qui.DOMElementNode, LocalName: d.Tag, AX: d.ax, Display: d.Display}
	if d.ID != "" {
		v.Attributes = append(v.Attributes, "id", d.ID)
	}
	if len(d.Classes) > 0 {
		v.Attributes = append(v.Attributes, "class", strings.Join(d.Classes, " "))
	}
	keys := make([]string, 0, len(d.Attrs))
	for k := range d.Attrs {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		v.Attributes = append(v.Attributes, k, d.Attrs[k])
	}
	if d.el != nil {
		node := d
		v.BoxModel = func() (qui.Rect, qui.Insets, qui.Insets, qui.Insets) {
			bm := boxModelFor(node)
			return bm.Content, bm.Padding, bm.Border, bm.Margin
		}
	}
	// A leaf element's own text is a text child in the DOM; htmlcss keeps
	// it on the element, so give it a stable synthetic node.
	if len(d.Children) == 0 && d.Text != "" && d.el != nil {
		v.Children = append(v.Children, &qui.DOMViewNode{
			NodeID: d.el.SyntheticNodeID("#text"), NodeType: qui.DOMTextNode, NodeValue: d.Text,
		})
	}
	for _, c := range d.Children {
		v.Children = append(v.Children, domViewOf(c))
	}
	return v
}

// -------------------------------------------------------------------
// CSS domain

type cssNodeParams struct {
	NodeID qui.NodeID `json:"nodeId"`
}

type cssProperty struct {
	Name      string `json:"name"`
	Value     string `json:"value"`
	Important bool   `json:"important,omitempty"`
	// qui: true when a higher-cascade declaration wins (DevTools strikes
	// these through).
	Overridden bool `json:"overridden,omitempty"`
}

type cssStyle struct {
	CSSProperties    []cssProperty `json:"cssProperties"`
	ShorthandEntries []any         `json:"shorthandEntries"`
}

type cssSelectorList struct {
	Selectors []struct {
		Text string `json:"text"`
	} `json:"selectors"`
	Text string `json:"text"`
}

type cssRule struct {
	SelectorList cssSelectorList `json:"selectorList"`
	Origin       string          `json:"origin" desc:"regular (author) | user-agent"`
	Style        cssStyle        `json:"style"`
	Specificity  [3]int          `json:"specificity" desc:"qui: (ids, classes, tags)"`
}

type cssRuleMatch struct {
	Rule              cssRule `json:"rule"`
	MatchingSelectors []int   `json:"matchingSelectors"`
}

type cssMatchedResult struct {
	InlineStyle     *cssStyle      `json:"inlineStyle,omitempty"`
	MatchedCSSRules []cssRuleMatch `json:"matchedCSSRules" desc:"lowest precedence first, as in CDP"`
	Inherited       []any          `json:"inherited"`
}

type cssComputedProperty struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

type cssComputedResult struct {
	ComputedStyle []cssComputedProperty `json:"computedStyle"`
}

type cssNodeStylesParams struct {
	NodeID   qui.NodeID `json:"nodeId,omitempty"`
	Selector string     `json:"selector,omitempty" desc:"CSS selector; every match is reported"`
}

type cssNodeStylesResult struct {
	Elements []*ElementStyles `json:"elements"`
}

func cssStyleOf(decls []DeclInfo) cssStyle {
	st := cssStyle{CSSProperties: []cssProperty{}, ShorthandEntries: []any{}}
	for _, d := range decls {
		st.CSSProperties = append(st.CSSProperties, cssProperty{Name: d.Property, Value: d.Value, Important: d.Important, Overridden: !d.Active})
	}
	return st
}

// withElement finds the DOM node with id on the UI goroutine and runs fn.
func withElement(w *qui.Window, id qui.NodeID, fn func(d *DOMNode) error) error {
	return InspectWindowDo(w, func(t *DOMTree) error {
		for _, d := range t.flatten() {
			if d.NodeID == id {
				return fn(d)
			}
		}
		return qui.NewProtocolError("stale_node", "no element %s (CSS applies to htmlcss elements only)", id)
	})
}

func cssDomain() qui.ProtocolDomain {
	return qui.ProtocolDomain{
		Name:        "CSS",
		Description: "The cascade for htmlcss elements: which rules matched, which declarations won, the computed values.",
		Commands: []qui.ProtocolCommand{
			qui.NewProtocolCommand("getMatchedStylesForNode", "Rules that matched the element (CDP order: lowest precedence first) and its inline style; overridden declarations are flagged.",
				func(ctx *qui.ProtocolContext, p cssNodeParams) (out cssMatchedResult, err error) {
					out.MatchedCSSRules, out.Inherited = []cssRuleMatch{}, []any{}
					err = withElement(ctx.Window, p.NodeID, func(d *DOMNode) error {
						rules := d.styles().MatchedRules
						for i := len(rules) - 1; i >= 0; i-- {
							r := rules[i]
							if r.Origin == "inline" {
								st := cssStyleOf(r.Declarations)
								out.InlineStyle = &st
								continue
							}
							origin := "regular"
							if r.Origin == "user-agent" {
								origin = "user-agent"
							}
							rule := cssRule{Origin: origin, Style: cssStyleOf(r.Declarations), Specificity: r.Specificity}
							rule.SelectorList.Text = r.Selector
							rule.SelectorList.Selectors = append(rule.SelectorList.Selectors, struct {
								Text string `json:"text"`
							}{r.Selector})
							out.MatchedCSSRules = append(out.MatchedCSSRules, cssRuleMatch{Rule: rule, MatchingSelectors: []int{0}})
						}
						return nil
					})
					return out, err
				}),
			qui.NewProtocolCommand("getComputedStyleForNode", "The final value of every property the cascade set on the element.",
				func(ctx *qui.ProtocolContext, p cssNodeParams) (out cssComputedResult, err error) {
					out.ComputedStyle = []cssComputedProperty{}
					err = withElement(ctx.Window, p.NodeID, func(d *DOMNode) error {
						computed := d.styles().Computed
						names := make([]string, 0, len(computed))
						for k := range computed {
							names = append(names, k)
						}
						sort.Strings(names)
						for _, k := range names {
							out.ComputedStyle = append(out.ComputedStyle, cssComputedProperty{Name: k, Value: computed[k]})
						}
						return nil
					})
					return out, err
				}),
			qui.NewProtocolCommand("getNodeStyles", "qui: Styles + Computed + box model in one readable object per element — the answer to \"why is this styled this way / why didn't my rule apply\". Address by nodeId or CSS selector.",
				func(ctx *qui.ProtocolContext, p cssNodeStylesParams) (out cssNodeStylesResult, err error) {
					out.Elements = []*ElementStyles{}
					if p.NodeID != 0 {
						err = withElement(ctx.Window, p.NodeID, func(d *DOMNode) error {
							out.Elements = append(out.Elements, d.styles())
							return nil
						})
						return out, err
					}
					if p.Selector == "" {
						return out, qui.NewProtocolError("invalid_params", `give "nodeId" or "selector"`)
					}
					err = InspectWindowDo(ctx.Window, func(t *DOMTree) error {
						styles, err := t.Styles(p.Selector)
						if err != nil {
							return qui.NewProtocolError("bad_selector", "%v", err)
						}
						out.Elements = styles
						return nil
					})
					return out, err
				}).WithStrict(),
		},
	}
}
