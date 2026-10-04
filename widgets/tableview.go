package widgets

import (
	"time"

	. "github.com/qizhanchan/qui"
)

// TableView is a virtualizing tabular list. Same single-column row
// virtualization as ListView, but each row is split into cells per
// Column definition. The caller supplies column definitions up-front
// (title + width); columns with Width == 0 share remaining space
// equally, so a common idiom is one fixed "ID" column + variable-
// width "Name" column.
//
// Current limitations:
//   - Column resize drag (handle on column boundary)
//   - Sort on header click
//   - Multi-select
//   - Horizontal scroll (columns are clipped if they overflow)
//
// The model / view split mirrors ListView: TableModel exposes row
// count + cell text; expansion / decoration is a view concern.

// TableModel is the data source for a TableView.
type TableModel interface {
	RowCount() int
	CellText(row, col int) string
}

// TableColumn describes one visible column.
type TableColumn struct {
	Title string
	// Width in logical pixels. 0 means "take an equal share of
	// whatever remains after fixed-width columns are laid out".
	Width float32
}

// SliceTableModel is a trivial TableModel backed by [][]string —
// rows[i][j] is cell (row i, column j).
type SliceTableModel struct {
	Rows [][]string
}

func (s *SliceTableModel) RowCount() int { return len(s.Rows) }
func (s *SliceTableModel) CellText(row, col int) string {
	if row < 0 || row >= len(s.Rows) {
		return ""
	}
	r := s.Rows[row]
	if col < 0 || col >= len(r) {
		return ""
	}
	return r[col]
}

// NewTableModel returns a SliceTableModel ready for use.
func NewTableModel(rows [][]string) *SliceTableModel {
	return &SliceTableModel{Rows: rows}
}

const (
	tableRowDefaultH    = 26
	tableHeaderDefaultH = 28
	tableCellPadX       = 10
)

// TableView is the virtualizing table widget.
type TableView struct {
	BaseWidget
	Model         TableModel
	rowWidgets    []Widget
	useWidgetRows bool
	Columns       []TableColumn
	SelectedIdx   int
	selection     singleSelectionModel
	RowHeight     float32
	HeaderHeight  float32
	OnSelect      func(row int)
	OnActivate    func(row int)

	scrollY  float32
	hoverIdx int
	focused  bool

	barDragging        bool
	barDragStartY      float32
	barDragStartScroll float32
}

// NewTableView constructs a TableView with the given columns and model.
// No row is pre-selected (SelectedIdx = -1).
func NewTableView(columns []TableColumn, model TableModel) *TableView {
	tv := &TableView{
		BaseWidget:    NewBaseWidget(),
		Model:         model,
		Columns:       columns,
		SelectedIdx:   -1,
		selection:     newSingleSelectionModel(),
		RowHeight:     tableRowDefaultH,
		HeaderHeight:  tableHeaderDefaultH,
		hoverIdx:      -1,
		useWidgetRows: false,
	}
	tv.SetSelf(tv)
	tv.Style().Background = Color{R: 0.1, G: 0.1, B: 0.12, A: 1}
	tv.Style().Foreground = ColorWhite
	tv.Style().Border = Color{R: 0.3, G: 0.3, B: 0.35, A: 1}
	tv.Style().BorderSize = 1
	tv.Style().Radius = 3
	tv.Style().Font = Font{Size: 14}
	return tv
}

func (tv *TableView) Focusable() bool { return tv.Enabled() }
func (tv *TableView) CancelInteraction() {
	if tv.barDragging {
		tv.barDragging = false
		tv.Invalidate()
	}
}
func (tv *TableView) SetFocused(f bool) {
	if tv.focused == f {
		return
	}
	tv.focused = f
	tv.Invalidate()
}

func (tv *TableView) ChildList() []Widget {
	if !tv.useWidgetRows {
		return nil
	}
	return tv.rowWidgets
}

func (tv *TableView) SetModel(model TableModel) {
	tv.detachRowWidgets()
	tv.Model = model
	tv.useWidgetRows = false
	tv.selection.Reset()
	tv.SelectedIdx = tv.selection.Index()
	tv.scrollY = 0
	tv.hoverIdx = -1
	tv.InvalidateLayout()
}

// SetRowWidgets switches TableView into widget-row mode.
//
// Rows are treated as child widgets and rendered in the body viewport
// (below the header strip).
func (tv *TableView) SetRowWidgets(rows []Widget) {
	if tv.useWidgetRows && tv.Model == nil && sameWidgetSlice(tv.rowWidgets, rows) {
		return
	}
	tv.Model = nil
	tv.useWidgetRows = true
	oldRows := append([]Widget(nil), tv.rowWidgets...)
	for _, old := range oldRows {
		if old != nil && !widgetSliceContains(rows, old) {
			tv.ReleaseChildForTransfer(old)
			DetachWidgetTree(old)
		}
	}

	var parent Widget = tv
	if s := tv.Self(); s != nil {
		parent = s
	}
	accepted := make([]Widget, 0, len(rows))
	for _, row := range rows {
		if row != nil && !widgetSliceContains(accepted, row) && AdoptWidgetTree(row, parent, tv.Window()) {
			accepted = append(accepted, row)
		}
	}
	tv.rowWidgets = accepted

	count := len(tv.rowWidgets)
	tv.selection.Sync(tv.SelectedIdx)
	tv.selection.Clamp(count)
	tv.SelectedIdx = tv.selection.Index()
	if tv.hoverIdx >= count {
		tv.hoverIdx = -1
	}
	tv.scrollY = ClampScroll(tv.scrollY, tv.contentHeight(), tv.bodyRect().H)
	tv.InvalidateLayout()
}

func (tv *TableView) detachRowWidgets() {
	rows := append([]Widget(nil), tv.rowWidgets...)
	for _, row := range rows {
		if row == nil {
			continue
		}
		tv.ReleaseChildForTransfer(row)
		DetachWidgetTree(row)
	}
	tv.rowWidgets = nil
}

func (tv *TableView) ReleaseChildForTransfer(child Widget) bool {
	for i, row := range tv.rowWidgets {
		if row != child {
			continue
		}
		tv.rowWidgets = append(tv.rowWidgets[:i], tv.rowWidgets[i+1:]...)
		if child.Parent() == tv {
			child.SetParent(nil)
		}
		tv.InvalidateLayout()
		return true
	}
	return false
}

func (tv *TableView) RowCount() int {
	if tv.useWidgetRows {
		return len(tv.rowWidgets)
	}
	if tv.Model == nil {
		return 0
	}
	return tv.Model.RowCount()
}

// SelectedRow returns the currently-selected row index, or -1.
func (tv *TableView) SelectedRow() int { return tv.SelectedIdx }

// Select updates SelectedIdx and auto-scrolls to keep it visible.
func (tv *TableView) Select(row int) {
	n := tv.RowCount()
	tv.selection.Sync(tv.SelectedIdx)
	row, changed := tv.selection.Select(row, n)
	tv.SelectedIdx = tv.selection.Index()
	if !changed {
		return
	}
	if row < 0 {
		tv.Invalidate()
		return
	}
	tv.ensureVisible(row)
	tv.Invalidate()
	if tv.OnSelect != nil {
		tv.OnSelect(row)
	}
}

func (tv *TableView) Measure(available Size) Size { return available }

// contentHeight is the vertical extent of the row body (excludes
// header — header is fixed).
func (tv *TableView) contentHeight() float32 {
	return fixedRowContentHeight(tv.RowCount(), tv.RowHeight)
}

// bodyRect returns the rectangle below the header row where data rows
// are drawn.
func (tv *TableView) bodyRect() Rect {
	b := tv.Bounds()
	return Rect{X: b.X, Y: b.Y + tv.HeaderHeight, W: b.W, H: b.H - tv.HeaderHeight}
}

func (tv *TableView) hasScrollbar() bool {
	return tv.contentHeight() > tv.bodyRect().H
}

// columnWidths resolves the final per-column width array. Fixed widths
// are kept; zero-width columns split whatever remains.
func (tv *TableView) columnWidths() []float32 {
	avail := tv.Bounds().W
	if tv.hasScrollbar() {
		avail -= VBarWidth
	}
	return resolveColumnWidths(tv.Columns, avail)
}

// resolveColumnWidths is the shared width resolver: fixed-width columns
// keep their declared Width; zero-width columns split whatever remains
// of `avail` evenly. Used by both TableView (for header / cell layout)
// and TableRow (the widget-row helper) so the two agree on cell rects.
func resolveColumnWidths(columns []TableColumn, avail float32) []float32 {
	widths := make([]float32, len(columns))
	var fixedSum float32
	flexCount := 0
	for i, c := range columns {
		if c.Width > 0 {
			widths[i] = c.Width
			fixedSum += c.Width
		} else {
			flexCount++
		}
	}
	if flexCount > 0 {
		flexShare := (avail - fixedSum) / float32(flexCount)
		if flexShare < 0 {
			flexShare = 0
		}
		for i, c := range columns {
			if c.Width <= 0 {
				widths[i] = flexShare
			}
		}
	}
	return widths
}

// visibleRange returns rows in [start, end) intersecting the body.
func (tv *TableView) visibleRange() (int, int) {
	return fixedRowVisibleRange(tv.RowCount(), tv.RowHeight, tv.scrollY, tv.bodyRect())
}

func (tv *TableView) ensureVisible(row int) {
	tv.scrollY = fixedRowEnsureVisible(tv.scrollY, row, tv.RowCount(), tv.RowHeight, tv.bodyRect())
}

func (tv *TableView) Draw(canvas Canvas) {
	b := tv.Bounds()
	if tv.Style().Background.A > 0 {
		canvas.FillRoundedRect(b, tv.Style().Radius, tv.Style().Background)
	}

	widths := tv.columnWidths()

	// Header — drawn first so the body clip doesn't paint over it.
	tv.drawHeader(canvas, widths)

	// Body rows, virtualized + clipped.
	body := tv.bodyRect()
	contentW := body.W
	if tv.hasScrollbar() {
		contentW -= VBarWidth
	}
	bodyClip := Rect{X: body.X, Y: body.Y, W: contentW, H: body.H}
	var effectiveClip Rect
	if ca, ok := canvas.(ClipAware); ok {
		effectiveClip = ca.ClipBoundsLogical().Intersect(bodyClip)
	} else {
		effectiveClip = bodyClip
	}
	if !effectiveClip.IsEmpty() {
		id := canvas.Save()
		canvas.ClipRect(effectiveClip)
		if tv.useWidgetRows {
			tv.drawWidgetRows(canvas, body, contentW)
		} else {
			tv.drawRows(canvas, widths, body, contentW)
		}
		canvas.RestoreTo(id)
	}

	// Scrollbar on the body area only — doesn't extend into header
	// strip, matching most native tables.
	if tv.hasScrollbar() {
		track := VBarTrackRect(body)
		thumb := VBarThumbRect(track, tv.contentHeight(), tv.scrollY)
		VBarDraw(canvas, track, thumb, tv.barDragging)
	}
	if tv.Style().BorderSize > 0 {
		canvas.StrokeRect(b, tv.Style().Border, tv.Style().BorderSize)
	}
}

func (tv *TableView) bodyContentWidth() float32 {
	contentW := tv.bodyRect().W
	if tv.hasScrollbar() {
		contentW -= VBarWidth
	}
	return contentW
}

func (tv *TableView) rowRect(row int, contentW float32) Rect {
	return fixedRowRect(tv.bodyRect(), row, tv.RowHeight, tv.scrollY, contentW)
}

func (tv *TableView) drawHeader(canvas Canvas, widths []float32) {
	b := tv.Bounds()
	header := Rect{X: b.X, Y: b.Y, W: b.W, H: tv.HeaderHeight}
	canvas.FillRect(header, Color{R: 0.14, G: 0.14, B: 0.17, A: 1})
	x := b.X
	headerFont := Font{Size: tv.Style().Font.Size, Bold: true}
	for i, col := range tv.Columns {
		cellRect := Rect{X: x, Y: b.Y, W: widths[i], H: tv.HeaderHeight}
		canvas.DrawText(col.Title,
			Rect{X: cellRect.X + tableCellPadX, Y: cellRect.Y + 6, W: cellRect.W - 2*tableCellPadX, H: cellRect.H - 10},
			Color{R: 0.85, G: 0.85, B: 0.88, A: 1}, headerFont)
		// Right divider.
		canvas.FillRect(Rect{X: cellRect.X + widths[i] - 1, Y: b.Y + 4, W: 1, H: tv.HeaderHeight - 8},
			Color{R: 0.3, G: 0.3, B: 0.33, A: 1})
		x += widths[i]
	}
	// Bottom separator between header and body.
	canvas.FillRect(Rect{X: b.X, Y: b.Y + tv.HeaderHeight - 1, W: b.W, H: 1},
		Color{R: 0.3, G: 0.3, B: 0.33, A: 1})
}

func (tv *TableView) drawRows(canvas Canvas, widths []float32, body Rect, contentW float32) {
	start, end := tv.visibleRange()
	selectedBg := Color{R: 0.25, G: 0.5, B: 1.0, A: 1}
	hoverBg := Color{R: 0.18, G: 0.18, B: 0.22, A: 1}

	for i := start; i < end; i++ {
		rowY := body.Y + float32(i)*tv.RowHeight - tv.scrollY
		rowRect := Rect{X: body.X, Y: rowY, W: contentW, H: tv.RowHeight}

		if i == tv.SelectedIdx {
			canvas.FillRect(rowRect, selectedBg)
		} else if i == tv.hoverIdx {
			canvas.FillRect(rowRect, hoverBg)
		} else if i%2 == 1 {
			// Subtle zebra striping for readability.
			canvas.FillRect(rowRect, Color{R: 0.12, G: 0.12, B: 0.14, A: 1})
		}

		x := rowRect.X
		for c, w := range widths {
			cellRect := Rect{X: x + tableCellPadX, Y: rowRect.Y + 4, W: w - 2*tableCellPadX, H: rowRect.H - 8}
			canvas.DrawText(tv.Model.CellText(i, c), cellRect, tv.Style().Foreground, tv.Style().Font)
			x += w
		}
	}
}

func (tv *TableView) drawWidgetRows(canvas Canvas, body Rect, contentW float32) {
	start, end := tv.visibleRange()
	selectedBg := Color{R: 0.25, G: 0.5, B: 1.0, A: 1}
	hoverBg := Color{R: 0.18, G: 0.18, B: 0.22, A: 1}

	for i := start; i < end; i++ {
		if i < 0 || i >= len(tv.rowWidgets) {
			continue
		}
		row := tv.rowWidgets[i]
		if row == nil {
			continue
		}
		rowRect := tv.rowRect(i, contentW)
		if i == tv.SelectedIdx {
			canvas.FillRect(rowRect, selectedBg)
		} else if i == tv.hoverIdx {
			canvas.FillRect(rowRect, hoverBg)
		} else if i%2 == 1 {
			canvas.FillRect(rowRect, Color{R: 0.12, G: 0.12, B: 0.14, A: 1})
		}
		row.Layout(rowRect)
		WithClipRect(canvas, rowRect, row.Draw)
	}
}

func (tv *TableView) Handle(event Event) bool {
	if !tv.Enabled() {
		return false
	}
	body := tv.bodyRect()
	switch e := event.(type) {
	case MouseEvent:
		switch e.Type() {
		case EventMouseLeave:
			if tv.hoverIdx != -1 {
				tv.hoverIdx = -1
				tv.Invalidate()
			}
		case EventMouseMove:
			if idx := tv.rowAt(e.X, e.Y); idx != tv.hoverIdx {
				tv.hoverIdx = idx
				tv.Invalidate()
			}
			if tv.barDragging {
				track := VBarTrackRect(body)
				old := tv.scrollY
				tv.scrollY = VBarScrollFromDrag(track, tv.contentHeight(),
					tv.barDragStartScroll, tv.barDragStartY, e.Y)
				tv.scrollY = ClampScroll(tv.scrollY, tv.contentHeight(), body.H)
				if tv.scrollY != old {
					tv.Invalidate()
				}
				return true
			}
		case EventScroll:
			if body.Contains(Point{X: e.X, Y: e.Y}) {
				old := tv.scrollY
				tv.scrollY = ClampScroll(tv.scrollY-e.DeltaY*tv.RowHeight,
					tv.contentHeight(), body.H)
				if tv.scrollY != old {
					tv.Invalidate()
				}
				return true
			}
		case EventMouseDown:
			if e.Button != MouseButtonLeft {
				return false
			}
			p := Point{X: e.X, Y: e.Y}
			if !tv.Bounds().Contains(p) {
				return false
			}
			// Header clicks are ignored for now (future: sort).
			if e.Y < tv.Bounds().Y+tv.HeaderHeight {
				return false
			}
			// Scrollbar.
			if tv.hasScrollbar() {
				track := VBarTrackRect(body)
				thumb := VBarThumbRect(track, tv.contentHeight(), tv.scrollY)
				if thumb.Contains(p) {
					tv.barDragging = true
					tv.barDragStartY = e.Y
					tv.barDragStartScroll = tv.scrollY
					tv.Invalidate()
					return true
				}
				if track.Contains(p) {
					old := tv.scrollY
					if e.Y < thumb.Y {
						tv.scrollY = ClampScroll(tv.scrollY-body.H*0.9, tv.contentHeight(), body.H)
					} else {
						tv.scrollY = ClampScroll(tv.scrollY+body.H*0.9, tv.contentHeight(), body.H)
					}
					if tv.scrollY != old {
						tv.Invalidate()
					}
					return true
				}
			}
			// Row selection.
			if idx := tv.rowAt(e.X, e.Y); idx >= 0 {
				if tv.useWidgetRows && e.Phase() == PhaseCapture {
					tv.Select(idx)
					return false
				}
				tv.Select(idx)
				return true
			}
		case EventMouseUp:
			if tv.barDragging {
				tv.barDragging = false
				tv.Invalidate()
				return true
			}
		}
	case KeyEvent:
		if !tv.focused || e.Type() != EventKeyDown {
			return false
		}
		switch e.Key {
		case KeyDown:
			next := tv.SelectedIdx + 1
			if tv.SelectedIdx < 0 {
				next = 0
			}
			tv.Select(next)
			return true
		case KeyUp:
			if tv.SelectedIdx > 0 {
				tv.Select(tv.SelectedIdx - 1)
			}
			return true
		case KeyEnter:
			if tv.OnActivate != nil && tv.SelectedIdx >= 0 {
				tv.OnActivate(tv.SelectedIdx)
			}
			return true
		}
	}
	return false
}

func (tv *TableView) HitTest(p Point) Widget {
	if !tv.Bounds().Contains(p) {
		return nil
	}
	if tv.useWidgetRows {
		body := tv.bodyRect()
		contentW := tv.bodyContentWidth()
		if p.Y >= body.Y && p.X <= body.X+contentW {
			start, end := tv.visibleRange()
			for i := end - 1; i >= start; i-- {
				if i < 0 || i >= len(tv.rowWidgets) {
					continue
				}
				row := tv.rowWidgets[i]
				if row == nil {
					continue
				}
				rowRect := tv.rowRect(i, contentW)
				if !rowRect.Contains(p) {
					continue
				}
				row.Layout(rowRect)
				if hit := row.HitTest(p); hit != nil {
					return hit
				}
				return row
			}
		}
	}
	return tv
}

func (tv *TableView) Tick(now time.Time) Rect {
	if !tv.useWidgetRows {
		return Rect{}
	}
	start, end := tv.visibleRange()
	if start == end {
		return Rect{}
	}
	contentW := tv.bodyContentWidth()
	var dirty Rect
	for i := start; i < end; i++ {
		if i < 0 || i >= len(tv.rowWidgets) {
			continue
		}
		row := tv.rowWidgets[i]
		if row == nil {
			continue
		}
		row.Layout(tv.rowRect(i, contentW))
		if t, ok := row.(Tickable); ok {
			dirty = dirty.Union(t.Tick(now))
		}
	}
	return dirty
}

// rowAt returns the data-row index at (x, y), or -1 if that point is
// in the header, scrollbar, or outside the widget.
func (tv *TableView) rowAt(x, y float32) int {
	b := tv.Bounds()
	if !b.Contains(Point{X: x, Y: y}) {
		return -1
	}
	// Header area doesn't correspond to data rows.
	if y < b.Y+tv.HeaderHeight {
		return -1
	}
	body := tv.bodyRect()
	contentW := body.W
	if tv.hasScrollbar() {
		contentW -= VBarWidth
	}
	return fixedRowAt(body, contentW, tv.RowCount(), tv.RowHeight, tv.scrollY, x, y)
}
