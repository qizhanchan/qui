package widgets

import (
	"testing"

	. "github.com/qizhanchan/qui"
)

type countModel struct{ n int }

func (m *countModel) RowCount() int        { return m.n }
func (m *countModel) RowText(i int) string { return "row" }

// A live model that shrinks under a deep scroll offset still shows rows.
func TestListViewReclampsWhenModelShrinks(t *testing.T) {
	m := &countModel{n: 100}
	lv := NewListView(m)
	lv.Layout(Rect{W: 200, H: 100})
	lv.scrollY = lv.maxScroll()
	m.n = 5
	start, end := lv.visibleRange()
	if end <= start {
		t.Fatalf("visible rows [%d,%d) after the model shrank, want some", start, end)
	}
}
