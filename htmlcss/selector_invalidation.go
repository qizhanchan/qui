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
