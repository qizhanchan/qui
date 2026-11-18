package htmlcss

import (
	"maps"
)

type customPropPair struct{ a, b *byte }

// CSS variable tables are immutable and usually shared by thousands of
// descendants and state variants. Compare each table pair once per pass;
// reflect.DeepEqual otherwise traverses the entire theme on every node.
func (eng *StyleEngine) stylesEqual(a, b *ComputedStyle) bool {
	if a == b {
		return true
	}
	if a == nil || b == nil {
		return false
	}
	if !maps.Equal(a.raw, b.raw) {
		return false
	}
	if a.customPropsID != b.customPropsID || a.customPropsID == nil {
		pair := customPropPair{a.customPropsID, b.customPropsID}
		same, known := eng.customEqual[pair]
		if !known || pair.a == nil || pair.b == nil {
			same = maps.Equal(a.customProps, b.customProps)
			if pair.a != nil && pair.b != nil {
				if eng.customEqual == nil {
					eng.customEqual = map[customPropPair]bool{}
				}
				eng.customEqual[pair] = same
			}
		}
		if !same {
			return false
		}
	}
	as := []*ComputedStyle{a.Hover, a.Focus, a.Active, a.FocusVisible, a.AncestorHover, a.AncestorFocus, a.AncestorActive}
	bs := []*ComputedStyle{b.Hover, b.Focus, b.Active, b.FocusVisible, b.AncestorHover, b.AncestorFocus, b.AncestorActive}
	if !stylesEqualFields(a, b) {
		return false
	}
	for i := range as {
		if !eng.stylesEqual(as[i], bs[i]) {
			return false
		}
	}
	return true
}
