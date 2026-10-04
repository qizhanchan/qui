package htmlcss

import "testing"

// :has() is parent-facing: a mutation on a deep descendant must restyle the
// matching ancestor, not only the changed element's immediate parent scope.
func TestScopedRestyleDeepHasInvalidatesRoot(t *testing.T) {
	eng, body, _, _, p1, _, _, _ := scopedFixture(t,
		`div:has(.hot) { background: #ff0000 }`)

	before := eng.styled
	p1.SetClass("hot")
	if walked := eng.styled - before; walked != 7 {
		t.Fatalf("deep :has restyle walked %d elements, want full root (7)", walked)
	}
	if got := body.Style().Background; got.R < 0.9 {
		t.Fatalf("ancestor style after descendant mutation = %+v, want red", got)
	}
}
