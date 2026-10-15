package htmlcss

import (
	"testing"

	"github.com/qizhanchan/qui"
)

// listFixture mounts .list > n × (.row > span), fully styled.
func listFixture(t testing.TB, css string, n int) (eng *StyleEngine, list *El, rows []*El) {
	t.Helper()
	eng = NewStyleEngine(css)
	list = eng.NewEl("div")
	list.SetClass("list")
	rows = make([]*El, n)
	kids := make([]qui.Widget, n)
	for i := range rows {
		rows[i] = newRow(eng, "row")
		kids[i] = rows[i]
	}
	list.SetElementChildren(kids)
	eng.SetRoot(list)
	win := qui.NewTestWindow(qui.Size{W: 400, H: 300})
	win.SetRoot(list)
	eng.Restyle()
	return
}

func newRow(eng *StyleEngine, class string) *El {
	r := eng.NewEl("div")
	r.SetClass(class)
	s := eng.NewEl("span")
	s.SetTextContent("x")
	r.SetElementChildren([]qui.Widget{s})
	return r
}

func asWidgets(els []*El) []qui.Widget {
	out := make([]qui.Widget, len(els))
	for i, e := range els {
		out[i] = e
	}
	return out
}

// Growing a long list styles the new rows only — the rows that stay keep
// their style (no structural rule concerns them), however many there are.
func TestChildrenChangedStylesOnlyNewChildren(t *testing.T) {
	eng, list, rows := listFixture(t, `.row { color: #ff0000 } .row span { color: #00ff00 }`, 50)
	extra := []*El{newRow(eng, "row"), newRow(eng, "row")}

	before := eng.styled
	list.SetElementChildren(asWidgets(append(append([]*El{}, rows...), extra...)))
	if walked := eng.styled - before; walked > 6 {
		t.Fatalf("appending 2 rows walked %d elements, want ≤ 6 (the new rows and their spans)", walked)
	}
	for _, r := range extra {
		if c := r.lastCS.Color; c.R < 0.9 {
			t.Fatalf("new row color = %+v, want red", c)
		}
		if span := r.elementKids[0].(*El); span.lastCS.Color.G < 0.9 {
			t.Fatalf("new row's span color = %+v, want green", span.lastCS.Color)
		}
	}
	// Removing rows restyles nothing.
	before = eng.styled
	list.SetElementChildren(asWidgets(rows[:10]))
	if walked := eng.styled - before; walked != 0 {
		t.Fatalf("removing rows walked %d elements, want 0", walked)
	}
}

// A structural rule that concerns the children still restyles them all
// when the list changes: the old first row stops being :first-child.
func TestChildrenChangedStructuralRule(t *testing.T) {
	eng, list, rows := listFixture(t, `.row:first-child { color: #ff0000 }`, 3)
	if c := rows[0].lastCS.Color; c.R < 0.9 {
		t.Fatalf("first row color = %+v, want red", c)
	}
	head := newRow(eng, "row")
	list.SetElementChildren(asWidgets(append([]*El{head}, rows...)))
	if c := head.lastCS.Color; c.R < 0.9 {
		t.Fatalf("new first row color = %+v, want red", c)
	}
	if c := rows[0].lastCS.Color; c.R > 0.5 {
		t.Fatalf("old first row color = %+v, must drop :first-child", c)
	}
}

// A structural rule for other elements does not widen a list's restyle.
func TestChildrenChangedUnrelatedStructuralRule(t *testing.T) {
	eng, list, rows := listFixture(t, `.row { color: #ff0000 } .tab:first-child { color: #00ff00 } .a + .b { color: #0000ff }`, 50)
	extra := newRow(eng, "row")
	before := eng.styled
	list.SetElementChildren(asWidgets(append(append([]*El{}, rows...), extra)))
	if walked := eng.styled - before; walked > 3 {
		t.Fatalf("appending 1 row walked %d elements, want ≤ 3", walked)
	}
	// A class change on one row restyles that row only.
	before = eng.styled
	rows[3].SetClass("row selected")
	if walked := eng.styled - before; walked != 2 {
		t.Fatalf("class change walked %d elements, want 2 (the row and its span)", walked)
	}
}

// Moving a styled child under another parent restyles it there.
func TestChildrenChangedReparentedChild(t *testing.T) {
	eng := NewStyleEngine(`.a .row { color: #ff0000 } .b .row { color: #00ff00 }`)
	root := eng.NewEl("div")
	a, b := eng.NewEl("div"), eng.NewEl("div")
	a.SetClass("a")
	b.SetClass("b")
	row := newRow(eng, "row")
	a.SetElementChildren([]qui.Widget{row})
	root.SetElementChildren([]qui.Widget{a, b})
	eng.SetRoot(root)
	win := qui.NewTestWindow(qui.Size{W: 400, H: 300})
	win.SetRoot(root)
	eng.Restyle()
	if c := row.lastCS.Color; c.R < 0.9 {
		t.Fatalf("row under .a = %+v, want red", c)
	}
	a.SetElementChildren(nil)
	b.SetElementChildren([]qui.Widget{row})
	if c := row.lastCS.Color; c.G < 0.9 {
		t.Fatalf("row moved under .b = %+v, want green", c)
	}
}
