package htmlcss

import (
	"testing"

	"github.com/qizhanchan/qui"
)

// scopedFixture builds body > (section1 > p1,p2) (section2 > p3,p4),
// mounted and fully styled — 7 elements total.
func scopedFixture(t *testing.T, css string) (eng *StyleEngine, body, s1, s2, p1, p2, p3, p4 *El) {
	t.Helper()
	eng = NewStyleEngine(css)
	body = eng.NewEl("div")
	s1, s2 = eng.NewEl("section"), eng.NewEl("section")
	p1, p2 = eng.NewEl("p"), eng.NewEl("p")
	p3, p4 = eng.NewEl("p"), eng.NewEl("p")
	p1.SetTextContent("one")
	p2.SetTextContent("two")
	p3.SetTextContent("three")
	p4.SetTextContent("four")
	s1.SetElementChildren([]qui.Widget{p1, p2})
	s2.SetElementChildren([]qui.Widget{p3, p4})
	body.SetElementChildren([]qui.Widget{s1, s2})
	eng.SetRoot(body)

	win := qui.NewTestWindow(qui.Size{W: 400, H: 300})
	win.SetRoot(body)
	eng.Restyle()
	return
}

// A change to one element restyles only its parent's subtree — the exact
// impact scope of any single-element change (self + descendants +
// following-sibling subtrees) — instead of the whole document.
func TestScopedRestyleWalksOnlyParentSubtree(t *testing.T) {
	eng, _, _, _, p1, p2, _, p4 := scopedFixture(t,
		`.hot { color: #ff0000 } .hot ~ p { color: #00ff00 }`)

	before := eng.styled
	p1.SetClass("hot") // test-mode markDirty flushes inline
	if walked := eng.styled - before; walked != 3 {
		t.Fatalf("scoped restyle walked %d elements, want 3 (section1 + p1 + p2)", walked)
	}
	// The change itself and its sibling-combinator fallout both landed.
	if c := p1.lastCS.Color; c.R < 0.9 {
		t.Fatalf("p1 color = %+v, want red", c)
	}
	if c := p2.lastCS.Color; c.G < 0.9 {
		t.Fatalf("p1's following sibling p2 color = %+v, want green (.hot ~ p)", c)
	}
	// The untouched sibling section kept its default style.
	if c := p4.lastCS.Color; c.G > 0.5 {
		t.Fatalf("p4 (other section) color = %+v, must not pick up .hot ~ p", c)
	}
}

// A descendant selector keyed off an ancestor class re-applies to the
// whole subtree when the ancestor's class changes.
func TestScopedRestyleDescendantCascade(t *testing.T) {
	_, _, s1, _, p1, p2, p3, _ := scopedFixture(t,
		`.dark p { color: #ffffff }`)

	s1.SetClass("dark")
	white := qui.Color{R: 1, G: 1, B: 1, A: 1}
	if p1.lastCS.Color != white || p2.lastCS.Color != white {
		t.Fatalf("descendants of .dark not restyled: p1=%+v p2=%+v", p1.lastCS.Color, p2.lastCS.Color)
	}
	if p3.lastCS.Color == white {
		t.Fatalf("p3 outside .dark subtree must stay default")
	}
}

// A dirty burst whose scopes nest (child's scope inside an ancestor's
// scope) collapses to the outermost scope — no element walked twice.
func TestScopedRestylePrunesNestedScopes(t *testing.T) {
	eng, _, s1, _, p1, _, _, _ := scopedFixture(t, ``)

	// Simulate a coalesced burst: p1 dirty (scope s1) AND s1 dirty
	// (scope body). s1's scope is inside body's ⇒ one walk of 7.
	eng.dirty = map[*El]bool{p1: true, s1: true}
	before := eng.styled
	eng.flush()
	if walked := eng.styled - before; walked != 7 {
		t.Fatalf("nested-scope burst walked %d elements, want 7 (one body walk)", walked)
	}
}

// Two dirty elements in independent subtrees restyle both scopes and
// nothing else.
func TestScopedRestyleIndependentScopes(t *testing.T) {
	eng, _, _, _, p1, _, p3, _ := scopedFixture(t, ``)

	eng.dirty = map[*El]bool{p1: true, p3: true}
	before := eng.styled
	eng.flush()
	if walked := eng.styled - before; walked != 6 {
		t.Fatalf("independent burst walked %d elements, want 6 (both sections, no body)", walked)
	}
}

// A change on the root element (no parent) restyles from the root itself.
func TestScopedRestyleRootScope(t *testing.T) {
	eng, body, _, _, _, _, _, _ := scopedFixture(t, `.x p { color: #ff0000 }`)

	before := eng.styled
	body.SetClass("x")
	if walked := eng.styled - before; walked != 7 {
		t.Fatalf("root change walked %d elements, want all 7", walked)
	}
}

// An unadopted top-level element (a portal/dialog root) is its own scope:
// mutating it restyles just its subtree, not the main document.
func TestScopedRestylePortalRootScope(t *testing.T) {
	eng, _, _, _, _, _, _, _ := scopedFixture(t, `.pop { background: #0000ff }`)

	portal := eng.NewEl("div") // never adopted ⇒ stays a restyle root
	before := eng.styled
	portal.SetClass("pop")
	if walked := eng.styled - before; walked != 1 {
		t.Fatalf("portal-root change walked %d elements, want 1", walked)
	}
	if got := portal.Style().Background; got.B < 0.9 {
		t.Fatalf("portal background = %+v, want blue", got)
	}
}
