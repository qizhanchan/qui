package htmlcss

import "testing"

// Skipping a variant cascade must give the same style as computing it.
func TestStateVariantShortcutMatchesFullCascade(t *testing.T) {
	sheet := ParseCSS(`.row:hover .del { color: red } .btn:hover { color: blue } .btn:active { color: green }
.f:focus-visible { outline: 2px solid red } .wrap:focus-within .lbl { color: purple } .plain { color: gray }`)
	doc := ParseHTML(`<div class="row wrap"><a id="d" class="del lbl"></a><b id="b" class="btn"></b><i id="p" class="plain"></i><input id="f" class="f"></div>`)
	for _, id := range []string{"d", "b", "p", "f"} {
		n := findByID(doc, id)
		cs := computeNode(n, sheet, nil)
		check := func(name string, got *ComputedStyle, st selectorState) {
			if got == nil {
				return
			}
			want := interpret(n, mergedDecls(n, sheet, st), nil)
			if got.Color != want.Color || got.OutlineWidth != want.OutlineWidth {
				t.Errorf("#%s %s: color %v outline %v, full cascade gives %v / %v",
					id, name, got.Color, got.OutlineWidth, want.Color, want.OutlineWidth)
			}
		}
		check("Hover", cs.Hover, selectorState{hover: true})
		check("Focus", cs.Focus, selectorState{focus: true})
		check("FocusVisible", cs.FocusVisible, selectorState{focus: true, focusVisible: true})
		check("Active", cs.Active, selectorState{hover: true, active: true})
		check("AncestorHover", cs.AncestorHover, selectorState{ancestorHover: true})
		check("AncestorFocus", cs.AncestorFocus, selectorState{ancestorFocus: true})
		check("AncestorActive", cs.AncestorActive, selectorState{ancestorActive: true})
	}
}
