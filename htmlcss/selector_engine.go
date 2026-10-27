package htmlcss

import (
	"fmt"

	"github.com/qizhanchan/qui"
)

// The CSS engine is the root selector layer's fallback: a selector the
// widget grammar rejects (a tag, a .class, a combinator, :has-text) resolves
// here, so Window.Find / Click / WaitFor and every agent action take one
// selector string for both the widget and the HTML surface.
func init() {
	qui.RegisterSelectorEngine(resolveCSS)
}

func resolveCSS(w *qui.Window, tree *qui.AccessibilityTree, selector string) ([]*qui.AXNode, error) {
	dom := projectTree(w, tree)
	matched, err := dom.Query(selector)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", qui.ErrInvalidSelector, err)
	}
	if len(matched) == 0 {
		return nil, nil
	}
	byID := make(map[qui.NodeID]*qui.AXNode, len(matched))
	for _, d := range matched {
		byID[d.NodeID] = nil
	}
	for _, n := range tree.Flatten() {
		if _, want := byID[n.NodeID]; want && n.NodeID != 0 {
			byID[n.NodeID] = n
		}
	}
	out := make([]*qui.AXNode, 0, len(matched))
	for _, d := range matched {
		if n := byID[d.NodeID]; n != nil {
			out = append(out, n)
		}
	}
	return out, nil
}
