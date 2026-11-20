package htmlcss

import (
	"reflect"
	"testing"
)

// The rule index hands an element exactly the rules whose subject keys
// off one of its classes, its id, its tag, an attribute it has, :root, or
// nothing — in source order.
func TestCandidateRules(t *testing.T) {
	sheet := ParseCSS(`
		.a { color: red }          /* 0 */
		div { color: blue }        /* 1 */
		#x { color: green }        /* 2 */
		.b.a { color: black }      /* 3: keyed on b */
		* { margin: 0 }            /* 4 */
		[role=x] { margin: 1px }   /* 5: keyed on the role attribute */
		p .a, span { color: gray } /* 6: a and span */
		.c::before { content: "" } /* 7 */
	`)
	n := &Node{Tag: "div", Attrs: map[string]string{"class": "a b", "id": "x"}}
	if got, want := sheet.candidateRules(n), []int{0, 1, 2, 3, 4, 6}; !reflect.DeepEqual(got, want) {
		t.Fatalf("candidates %v, want %v", got, want)
	}
	// Same tag, id and class, plus the attribute: a separate signature.
	withRole := &Node{Tag: "div", Attrs: map[string]string{"class": "a b", "id": "x", "role": "y"}}
	if got, want := sheet.candidateRules(withRole), []int{0, 1, 2, 3, 4, 5, 6}; !reflect.DeepEqual(got, want) {
		t.Fatalf("with role %v, want %v", got, want)
	}
	span := &Node{Tag: "span", Attrs: map[string]string{}}
	if got, want := sheet.candidateRules(span), []int{4, 6}; !reflect.DeepEqual(got, want) {
		t.Fatalf("span candidates %v, want %v", got, want)
	}
	// Appending rules rebuilds the index.
	sheet.Rules = append(sheet.Rules, ParseCSS(`span { color: red }`).Rules...)
	if got, want := sheet.candidateRules(span), []int{4, 6, 8}; !reflect.DeepEqual(got, want) {
		t.Fatalf("after append %v, want %v", got, want)
	}
	// The cascade still picks by specificity, then order.
	m := mergedDecls(n, sheet, selectorState{})
	if m["color"] != "green" { // #x outranks .b.a
		t.Fatalf("color = %q, want green", m["color"])
	}
}

// :root and keyless :is() / :where() subjects are indexed too: the root
// bucket only reaches the document element, and an :is() whose arguments
// all key off something files the rule under each of them.
func TestCandidateRulesRootAndIs(t *testing.T) {
	sheet := ParseCSS(`
		:root { color: red }                 /* 0 */
		:where(.a, button) { color: blue }   /* 1 */
		:is(.b, :hover) { color: green }     /* 2: any */
		[data-x] .c { color: black }         /* 3: keyed on c */
	`)
	doc := &Node{Tag: "#document"}
	html := &Node{Tag: "html", Type: ElementNode, Parent: doc, Attrs: map[string]string{}}
	body := &Node{Tag: "body", Type: ElementNode, Parent: html, Attrs: map[string]string{}}
	if got, want := sheet.candidateRules(html), []int{0, 2}; !reflect.DeepEqual(got, want) {
		t.Fatalf("html %v, want %v", got, want)
	}
	if got, want := sheet.candidateRules(body), []int{2}; !reflect.DeepEqual(got, want) {
		t.Fatalf("body %v, want %v", got, want)
	}
	btn := &Node{Tag: "button", Type: ElementNode, Parent: body, Attrs: map[string]string{}}
	if got, want := sheet.candidateRules(btn), []int{1, 2}; !reflect.DeepEqual(got, want) {
		t.Fatalf("button %v, want %v", got, want)
	}
}
