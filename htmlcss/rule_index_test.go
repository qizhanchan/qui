package htmlcss

import (
	"reflect"
	"testing"
)

// The rule index hands an element exactly the rules whose subject keys
// off one of its classes, its id, its tag, or nothing — in source order.
func TestCandidateRules(t *testing.T) {
	sheet := ParseCSS(`
		.a { color: red }          /* 0 */
		div { color: blue }        /* 1 */
		#x { color: green }        /* 2 */
		.b.a { color: black }      /* 3: keyed on b */
		* { margin: 0 }            /* 4 */
		[role=x] { margin: 1px }   /* 5: any */
		p .a, span { color: gray } /* 6: a and span */
		.c::before { content: "" } /* 7 */
	`)
	n := &Node{Tag: "div", Attrs: map[string]string{"class": "a b", "id": "x"}}
	if got, want := sheet.candidateRules(n), []int{0, 1, 2, 3, 4, 5, 6}; !reflect.DeepEqual(got, want) {
		t.Fatalf("candidates %v, want %v", got, want)
	}
	span := &Node{Tag: "span", Attrs: map[string]string{}}
	if got, want := sheet.candidateRules(span), []int{4, 5, 6}; !reflect.DeepEqual(got, want) {
		t.Fatalf("span candidates %v, want %v", got, want)
	}
	// Appending rules rebuilds the index.
	sheet.Rules = append(sheet.Rules, ParseCSS(`span { color: red }`).Rules...)
	if got, want := sheet.candidateRules(span), []int{4, 5, 6, 8}; !reflect.DeepEqual(got, want) {
		t.Fatalf("after append %v, want %v", got, want)
	}
	// The cascade still picks by specificity, then order.
	m := mergedDecls(n, sheet, selectorState{})
	if m["color"] != "green" { // #x outranks .b.a
		t.Fatalf("color = %q, want green", m["color"])
	}
}
