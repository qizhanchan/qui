package htmlcss

// Per-element inspection for in-app DevTools: the same Styles + box model
// view the agent's DOMTree.Styles serves, for one live element without
// projecting the whole window through the accessibility tree.

// InspectElement returns el's DevTools styles view: the cascade's winning
// declarations, every matched rule (inline, author by specificity, then
// user-agent) and the box model. BoxModel.Content is left zero: the
// caller knows where the element sits on screen. UI goroutine only.
func InspectElement(el *El) *ElementStyles {
	if el == nil {
		return &ElementStyles{Computed: map[string]string{}}
	}
	d := &DOMNode{Tag: el.tag, el: el}
	if el.node != nil {
		d.ID = el.node.ID()
		d.Classes = el.node.Classes()
	}
	return d.styles()
}
