package htmlcss

import "strings"

// Identity changes only invalidate ancestors if a :has() argument actually
// refers to an old or new class/id. Other selectors keep a local parent walk.
func (eng *StyleEngine) hasIdentityInHas(e *El, name string, previous []string) bool {
	if name != "class" && name != "id" {
		return false
	}
	old := ""
	if len(previous) > 0 {
		old = previous[0]
	}
	value, _ := e.readAttr(name)
	mentions := func(key string) bool {
		if name == "id" {
			return key == old || key == value
		}
		for _, v := range strings.Fields(old + " " + value) {
			if v == key {
				return true
			}
		}
		return false
	}
	var args func([]Selector) bool
	args = func(sels []Selector) bool {
		for _, sel := range sels {
			for _, part := range sel.parts {
				if name == "id" && part.sel.id != "" && mentions(part.sel.id) {
					return true
				}
				if name == "class" {
					for _, class := range part.sel.classes {
						if mentions(class) {
							return true
						}
					}
				}
				for _, pseudo := range part.sel.pseudos {
					if args(pseudo.args) {
						return true
					}
				}
			}
		}
		return false
	}
	var has func([]Selector) bool
	has = func(sels []Selector) bool {
		for _, sel := range sels {
			for _, part := range sel.parts {
				for _, pseudo := range part.sel.pseudos {
					if pseudo.kind == pseudoHas && args(pseudo.args) || has(pseudo.args) {
						return true
					}
				}
			}
		}
		return false
	}
	if eng.sheet != nil {
		for _, rule := range eng.sheet.Rules {
			if has(rule.Selectors) {
				return true
			}
		}
	}
	frameworkCSS.mu.RLock()
	defer frameworkCSS.mu.RUnlock()
	for _, rule := range frameworkCSS.rules {
		if has(rule.Selectors) {
			return true
		}
	}
	return false
}

// hasScope is the restyle scope a change at e needs because of :has(): a
// :has() can only flip on an element its compound could match (hasKeys)
// that is e or an ancestor of e — descendant arguments look down at e — or
// a sibling of one of those — sibling arguments (`:has(+ x)`, `:has(~ x y)`)
// look sideways and down. The scope is the parent of the topmost such
// ancestor, which covers the flipped element, everything it styles through
// descendant and sibling combinators, and e. ok is false when no :has()
// subject can be involved: the caller's ordinary scope stands. Chrome
// narrows :has() invalidation the same way, by the subject compound's
// features.
func (eng *StyleEngine) hasScope(e *El) (scope *El, ok bool) {
	var keys, ctx []*structKeys
	if eng.sheet != nil && eng.sheet.HasHas {
		keys = append(keys, &eng.sheet.hasKeys)
		ctx = append(ctx, &eng.sheet.hasCtxKeys)
	}
	frameworkCSS.mu.RLock()
	if frameworkCSS.has {
		k, c := frameworkCSS.hasKeys, frameworkCSS.hasCtxKeys
		keys = append(keys, &k)
		ctx = append(ctx, &c)
	}
	frameworkCSS.mu.RUnlock()
	match := func(ks []*structKeys, n *El) bool {
		if n == nil || n.node == nil {
			return false
		}
		for _, k := range ks {
			if k.relevant(n.node) {
				return true
			}
		}
		return false
	}
	var top *El
	for a := e; a != nil; a = a.elParent {
		// a is a candidate subject when its compound could match, or when
		// it lies inside a context element of a keyless one.
		hit := match(keys, a) || match(ctx, a.elParent)
		if !hit && a.elParent != nil {
			for _, w := range a.elParent.elementKids {
				if s, isEl := w.(*El); isEl && s != a && match(keys, s) {
					hit = true
					break
				}
			}
		}
		if hit {
			top = a
		}
	}
	if top == nil {
		return nil, false
	}
	if top.elParent != nil {
		return top.elParent, true
	}
	if eng.roots[top] || top.shadowHost != nil {
		return top, true
	}
	return nil, false
}
