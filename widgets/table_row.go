package widgets

import . "github.com/qizhanchan/qui"

// TableRow is the helper for TableView's widget-row mode. It owns
// a slice of cell widgets and lays them out horizontally to match
// the column widths reported by `columns`, using the same fixed-vs-flex
// rule TableView itself uses (see resolveColumnWidths).
//
// Pass rows into TableView.SetRowWidgets:
//
//	row := widgets.NewTableRow(tv.Columns, []qui.Widget{
//	    widgets.NewLabel("42"),
//	    widgets.NewInput(""),
//	    widgets.NewSelect(...),
//	})
//	tv.SetRowWidgets([]qui.Widget{row, otherRow})
//
// Cell widgets fill their column rect with zero inset. To pad a
// cell (e.g. a Input that shouldn't kiss the column divider),
// wrap it in a Container with the desired Style().Padding before
// passing it in.
//
// Cell count may differ from column count: extra cells past the last
// column are not laid out; missing cells leave that column blank.
type TableRow struct {
	Container
	columns []TableColumn
	cells   []Widget
}

// NewTableRow builds a TableRow with the given columns and cells.
// Cells are wired as children of the embedded Container, so event
// dispatch, focus collection, hit-testing, and Tick propagation all
// work without further plumbing.
func NewTableRow(columns []TableColumn, cells []Widget) *TableRow {
	r := &TableRow{
		Container: Container{BaseWidget: NewBaseWidget()},
		columns:   append([]TableColumn(nil), columns...),
		cells:     append([]Widget(nil), cells...),
	}
	r.SetSelf(r)
	r.syncCells()
	return r
}

// syncCells mounts exactly the cells that have a column. Cells past the
// last column are not laid out, so keeping them as children would leave
// them focusable/hit-testable at stale bounds (and drawing over the row).
func (r *TableRow) syncCells() {
	limit := len(r.columns)
	if limit == 0 {
		limit = len(r.cells)
	}
	if limit > len(r.cells) {
		limit = len(r.cells)
	}
	for r.ChildCount() > limit {
		r.RemoveChildAt(r.ChildCount() - 1)
	}
	for i := r.ChildCount(); i < limit; i++ {
		if c := r.cells[i]; c != nil {
			r.AddChild(c)
		}
	}
}

// SetColumns replaces the column layout. Use this when the parent
// TableView's columns change after the row was built — the row
// re-lays its cells on the next frame.
func (r *TableRow) SetColumns(columns []TableColumn) {
	r.columns = append(r.columns[:0], columns...)
	r.syncCells()
	r.InvalidateLayout()
}

// Layout overrides Container.Layout: the row has no LayoutEngine
// because cell positions are derived from the column widths, not
// a flex/grid pass.
func (r *TableRow) Layout(rect Rect) {
	r.BaseWidget.Layout(rect)
	widths := resolveColumnWidths(r.columns, rect.W)
	x := rect.X
	for i, child := range r.ChildList() {
		if i >= len(widths) {
			break
		}
		w := widths[i]
		cell := Rect{X: x, Y: rect.Y, W: w, H: rect.H}
		MeasureChild(child, Size{W: cell.W, H: cell.H})
		child.Layout(cell)
		x += w
	}
}

// Draw clips each cell to its column; a wide label/input in one cell must
// not paint over its neighbour (the model-row path already clips).
func (r *TableRow) Draw(canvas Canvas) {
	for _, child := range r.ChildList() {
		if boxChildHidden(child) {
			continue
		}
		cell := child.Bounds()
		if cell.IsEmpty() {
			continue
		}
		WithClipRect(canvas, cell, func(c Canvas) { child.Draw(c) })
	}
}
