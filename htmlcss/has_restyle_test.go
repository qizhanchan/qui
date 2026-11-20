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

// A :has() rule only widens a restyle when its subject compound can match
// an ancestor of the change (or one of their siblings): with
// `.card:has(.hot)` and no .card around, a change deep in the tree keeps its
// local scope instead of restyling the whole document (GitHub's pages carry
// such rules and restyled 2.5k elements per mutation).
func TestHasRuleWidensOnlyThroughItsSubject(t *testing.T) {
	eng, _, s1, _, p1, p2, _, _ := scopedFixture(t,
		`.card:has(.hot) { color: #ff0000 } :is(.panel, .box):has(> .hot) { background: #00ff00 } .menu :has(> .hot) { opacity: .5 }`)

	before := eng.styled
	p1.SetTextContent("changed")
	if walked := eng.styled - before; walked > 3 {
		t.Fatalf("text change with no :has subject around walked %d elements, want its parent subtree", walked)
	}
	s1.SetClass("card")
	p2.SetClass("hot")
	if s1.lastCS.Color.R < 0.9 {
		t.Fatalf(".card:has(.hot) not applied: %+v", s1.lastCS.Color)
	}
	p2.SetClass("")
	if s1.lastCS.Color.R > 0.5 {
		t.Fatal(".card:has(.hot) kept after .hot went away")
	}
	// :is() subjects and keyless subjects under a context compound.
	s1.SetClass("box")
	p2.SetClass("hot")
	if s1.lastCS.Background.G < 0.9 {
		t.Fatalf(":is(.panel, .box):has(> .hot) not applied: %+v", s1.lastCS.Background)
	}
}
