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
// Columns can align their text, draw their own cells (DrawCell) and sort
// on a header click (Sortable + OnSort, or a SortableTableModel). Widget
// rows can be supplied up front (SetRowWidgets) or built only while
// visible (SetRowFactory).
//
// Current limitations:
//   - Column resize drag (handle on column boundary)
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
	// TitleKey makes the header caption come from the message catalog
	// (Title is the fallback).
	TitleKey string
	// Width in logical pixels. 0 means "take an equal share of
	// whatever remains after fixed-width columns are laid out".
	Width float32
	// Align places the cell text (and header caption) horizontally.
	Align TextAlign
	// Sortable makes a header click sort by this column (see
	// TableView.OnSort).
	Sortable bool
	// DrawCell, when set, paints this column's cells instead of the
	// model text — a progress bar, a status dot, an icon. It runs inside
	// the cell's clip, after the row background.
	DrawCell func(canvas Canvas, cell TableCell)
}

// DisplayTitle is the header caption: TitleKey resolved, or Title.
func (c TableColumn) DisplayTitle() string {
	if c.TitleKey == "" {
		return c.Title
	}
	return TranslateOr("", c.TitleKey, c.Title, nil)
}

// TableCell is what a DrawCell callback paints.
type TableCell struct {
	Row, Col int
	// Rect is the cell's full rectangle; Content is Rect minus the
	// standard horizontal padding.
	Rect, Content Rect
	Text          string
	Selected      bool
	Hovered       bool
	// Foreground is the text color the table would have used.
	Foreground Color
	Font       Font
}

// SortableTableModel is implemented by a model that can reorder itself.
// A header click on a Sortable column calls SortBy (after OnSort).
type SortableTableModel interface {
	SortBy(col int, descending bool)
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
	lazy          *lazyRows
	// Colors overrides the theme palette field by field (see RowColors).
	Colors       RowColors
	Columns      []TableColumn
	SelectedIdx  int
	selection    singleSelectionModel
	RowHeight    float32
	HeaderHeight float32
	OnSelect     func(row int)
	// OnActivate fires on a double-click on a row, or Enter with a row
	// selected.
	OnActivate func(row int)
	// SortColumn is the column the data is sorted by (-1 for none) and
	// SortDescending its direction; the header shows an arrow there. A
	// click on a Sortable header updates both, then calls OnSort and the
	// model's SortBy.
	SortColumn     int
	SortDescending bool
	OnSort         func(col int, descending bool)

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
		SortColumn:    -1,
	}
	tv.SetSelf(tv)
	// Colors resolve from the theme at draw time (see RowColors).
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
	if tv.lazy != nil {
		return tv.lazy.children()
	}
	return tv.rowWidgets
}

// widgetRow returns row i in widget-row mode, building it in factory mode.
func (tv *TableView) widgetRow(i int) Widget {
	if tv.lazy != nil {
		var parent Widget = tv
		if s := tv.Self(); s != nil {
			parent = s
		}
		return tv.lazy.get(i, parent, tv.Window())
	}
	if i < 0 || i >= len(tv.rowWidgets) {
		return nil
	}
	return tv.rowWidgets[i]
}

// SetRowFactory switches to widget-row mode with rows built on demand
// (typically NewTableRow(tv.Columns, cells)): build(i) runs when row i
// scrolls into view and far-off rows are released. See
// ListView.SetRowFactory.
func (tv *TableView) SetRowFactory(count int, build func(row int) Widget) {
	tv.detachRowWidgets()
	tv.Model = nil
	tv.useWidgetRows = true
	tv.lazy = &lazyRows{count: maxInt(count, 0), build: build}
	tv.selection.Sync(tv.SelectedIdx)
	tv.selection.Clamp(tv.lazy.count)
	tv.SelectedIdx = tv.selection.Index()
	tv.scrollY = ClampScroll(tv.scrollY, tv.contentHeight(), tv.bodyRect().H)
	tv.InvalidateLayout()
}

// SetRowCount updates a factory table's row count.
func (tv *TableView) SetRowCount(count int) {
	if tv.lazy == nil {
		return
	}
	tv.lazy.count = maxInt(count, 0)
	tv.lazy.prune(0, tv.lazy.count)
	tv.selection.Sync(tv.SelectedIdx)
	tv.selection.Clamp(tv.lazy.count)
	tv.SelectedIdx = tv.selection.Index()
	tv.scrollY = ClampScroll(tv.scrollY, tv.contentHeight(), tv.bodyRect().H)
	tv.InvalidateLayout()
}

// RefreshRows drops every built factory row so visible rows are rebuilt.
func (tv *TableView) RefreshRows() {
	if tv.lazy != nil {
		tv.lazy.releaseAll()
		tv.Invalidate()
	}
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
	if tv.useWidgetRows && tv.Model == nil && tv.lazy == nil && sameWidgetSlice(tv.rowWidgets, rows) {
		return
	}
	tv.lazy.releaseAll()
	tv.lazy = nil
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
	tv.lazy.releaseAll()
	tv.lazy = nil
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
	if tv.lazy.release(child) {
		if child.Parent() == tv {
			child.SetParent(nil)
		}
		return true
	}
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
	if tv.lazy != nil {
		return tv.lazy.count
	}
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

func (tv *TableView) colors() RowColors { return tv.Colors.resolve(tv.Style()) }

func (tv *TableView) Draw(canvas Canvas) {
	b := tv.Bounds()
	c := tv.colors()
	if c.Background.A > 0 {
		canvas.FillRoundedRect(b, tv.Style().Radius, c.Background)
	}

	widths := tv.columnWidths()

	// Header — drawn first so the body clip doesn't paint over it.
	tv.drawHeader(canvas, widths, c)

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
			tv.drawWidgetRows(canvas, body, contentW, c)
		} else {
			tv.drawRows(canvas, widths, body, contentW, c)
		}
		canvas.RestoreTo(id)
	}
	if tv.lazy != nil {
		start, end := tv.visibleRange()
		tv.lazy.prune(start, end)
	}

	// Scrollbar on the body area only — doesn't extend into header
	// strip, matching most native tables.
	if tv.hasScrollbar() {
		track := VBarTrackRect(body)
		thumb := VBarThumbRect(track, tv.contentHeight(), tv.scrollY)
		VBarDrawColors(canvas, track, thumb, tv.barDragging, c.Scrollbar)
	}
	if tv.Style().BorderSize > 0 {
		canvas.StrokeRect(b, c.Border, tv.Style().BorderSize)
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

func (tv *TableView) drawHeader(canvas Canvas, widths []float32, c RowColors) {
	b := tv.Bounds()
	header := Rect{X: b.X, Y: b.Y, W: b.W, H: tv.HeaderHeight}
	canvas.FillRect(header, c.Header)
	x := b.X
	headerFont := Font{Size: tv.Style().Font.Size, Bold: true}
	for i, col := range tv.Columns {
		cellRect := Rect{X: x, Y: b.Y, W: widths[i], H: tv.HeaderHeight}
		textRect := Rect{X: cellRect.X + tableCellPadX, Y: cellRect.Y + 6, W: cellRect.W - 2*tableCellPadX, H: cellRect.H - 10}
		if i == tv.SortColumn {
			// Reserve the arrow's slot at the trailing edge.
			const arrowW = 12
			arrow := Rect{X: textRect.X + textRect.W - arrowW, Y: cellRect.Y, W: arrowW, H: cellRect.H}
			drawSortArrow(canvas, arrow, tv.SortDescending, c.HeaderText)
			textRect.W -= arrowW + 4
		}
		drawAlignedText(canvas, col.DisplayTitle(), textRect, c.HeaderText, headerFont, col.Align)
		// Right divider.
		canvas.FillRect(Rect{X: cellRect.X + widths[i] - 1, Y: b.Y + 4, W: 1, H: tv.HeaderHeight - 8}, c.Divider)
		x += widths[i]
	}
	// Bottom separator between header and body.
	canvas.FillRect(Rect{X: b.X, Y: b.Y + tv.HeaderHeight - 1, W: b.W, H: 1}, c.Divider)
}

// drawSortArrow paints a small up (ascending) or down triangle centered
// in slot.
func drawSortArrow(canvas Canvas, slot Rect, descending bool, color Color) {
	const w, h = 8, 4
	cx := slot.X + slot.W/2
	cy := slot.Y + (slot.H-h)/2
	for i := 0; i < h; i++ {
		rowW := w * (1 - float32(i)/h)
		y := cy + float32(i) // descending: base on top, apex below
		if !descending {
			y = cy + h - 1 - float32(i)
		}
		canvas.FillRect(Rect{X: cx - rowW/2, Y: y, W: rowW, H: 1}, color)
	}
}

// drawAlignedText draws text in rect at the given horizontal alignment.
func drawAlignedText(canvas Canvas, text string, rect Rect, color Color, font Font, align TextAlign) {
	if align == TextAlignCenter || align == TextAlignEnd {
		tw, _ := TextMetrics(text, font)
		if tw < rect.W {
			if align == TextAlignCenter {
				rect.X += (rect.W - tw) / 2
			} else {
				rect.X += rect.W - tw
			}
			rect.W = tw
		}
	}
	canvas.DrawText(text, rect, color, font)
}

func (tv *TableView) drawRows(canvas Canvas, widths []float32, body Rect, contentW float32, c RowColors) {
	start, end := tv.visibleRange()
	for i := start; i < end; i++ {
		rowY := body.Y + float32(i)*tv.RowHeight - tv.scrollY
		rowRect := Rect{X: body.X, Y: rowY, W: contentW, H: tv.RowHeight}

		selected := i == tv.SelectedIdx
		fg := c.Text
		if selected {
			canvas.FillRect(rowRect, c.Selected)
			fg = c.SelectedText
		} else if i == tv.hoverIdx {
			canvas.FillRect(rowRect, c.Hover)
		} else if i%2 == 1 && c.Stripe.A > 0 {
			// Subtle zebra striping for readability.
			canvas.FillRect(rowRect, c.Stripe)
		}

		x := rowRect.X
		for col, w := range widths {
			cell := Rect{X: x, Y: rowRect.Y, W: w, H: rowRect.H}
			content := Rect{X: x + tableCellPadX, Y: rowRect.Y + 4, W: w - 2*tableCellPadX, H: rowRect.H - 8}
			text := tv.Model.CellText(i, col)
			if col < len(tv.Columns) && tv.Columns[col].DrawCell != nil {
				draw := tv.Columns[col].DrawCell
				WithClipRect(canvas, cell, func(cv Canvas) {
					draw(cv, TableCell{Row: i, Col: col, Rect: cell, Content: content, Text: text,
						Selected: selected, Hovered: i == tv.hoverIdx, Foreground: fg, Font: tv.Style().Font})
				})
			} else {
				align := TextAlignStart
				if col < len(tv.Columns) {
					align = tv.Columns[col].Align
				}
				drawAlignedText(canvas, text, content, fg, tv.Style().Font, align)
			}
			x += w
		}
	}
}

func (tv *TableView) drawWidgetRows(canvas Canvas, body Rect, contentW float32, c RowColors) {
	start, end := tv.visibleRange()
	for i := start; i < end; i++ {
		row := tv.widgetRow(i)
		if row == nil {
			continue
		}
		rowRect := tv.rowRect(i, contentW)
		if i == tv.SelectedIdx {
			canvas.FillRect(rowRect, c.Selected)
		} else if i == tv.hoverIdx {
			canvas.FillRect(rowRect, c.Hover)
		} else if i%2 == 1 && c.Stripe.A > 0 {
			canvas.FillRect(rowRect, c.Stripe)
		}
		row.Layout(rowRect)
		WithClipRect(canvas, rowRect, row.Draw)
	}
}

// headerColumnAt returns the column under x in the header, or -1.
func (tv *TableView) headerColumnAt(x float32) int {
	widths := tv.columnWidths()
	cx := tv.Bounds().X
	for i, w := range widths {
		if x >= cx && x < cx+w {
			return i
		}
		cx += w
	}
	return -1
}

// SortBy sets the sort column / direction, repaints the header, and
// notifies OnSort and a SortableTableModel.
func (tv *TableView) SortBy(col int, descending bool) {
	tv.SortColumn, tv.SortDescending = col, descending
	tv.Invalidate()
	if tv.OnSort != nil {
		tv.OnSort(col, descending)
	}
	if m, ok := tv.Model.(SortableTableModel); ok && col >= 0 {
		m.SortBy(col, descending)
		tv.Invalidate()
	}
}

// AccessibleChildren publishes the header cells, and in model mode the
// visible data cells, so `[role=columnheader][name="Size"]` can be clicked
// to sort and a cell can be read without a screenshot.
func (tv *TableView) AccessibleChildren() []AXChild {
	widths := tv.columnWidths()
	b := tv.Bounds()
	var out []AXChild
	x := b.X
	for i, col := range tv.Columns {
		out = append(out, AXChild{Role: RoleColumnHeader, Name: col.DisplayTitle(),
			Bounds: Rect{X: x, Y: b.Y, W: widths[i], H: tv.HeaderHeight}})
		x += widths[i]
	}
	if tv.useWidgetRows || tv.Model == nil {
		return out
	}
	start, end := tv.visibleRange()
	contentW := tv.bodyContentWidth()
	for r := start; r < end; r++ {
		rowRect := tv.rowRect(r, contentW)
		var st AccessibleState
		if r == tv.SelectedIdx {
			st |= AXStateSelected
		}
		cx := rowRect.X
		for col, w := range widths {
			out = append(out, AXChild{Role: RoleTableCell, Name: tv.Model.CellText(r, col), State: st,
				Bounds: Rect{X: cx, Y: rowRect.Y, W: w, H: rowRect.H}})
			cx += w
		}
	}
	return out
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
			// Header click: sort by a Sortable column (again to flip).
			if e.Y < tv.Bounds().Y+tv.HeaderHeight {
				col := tv.headerColumnAt(e.X)
				if col < 0 || col >= len(tv.Columns) || !tv.Columns[col].Sortable {
					return false
				}
				desc := false
				if col == tv.SortColumn {
					desc = !tv.SortDescending
				}
				tv.SortBy(col, desc)
				return true
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
				tv.Select(idx)
				if e.Clicks == 2 && tv.OnActivate != nil {
					tv.OnActivate(idx)
				}
				// In widget-row mode capture selects without consuming, so
				// the cell widgets still get the press.
				return !(tv.useWidgetRows && e.Phase() == PhaseCapture)
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
				row := tv.widgetRow(i)
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
		row := tv.widgetRow(i)
		if row == nil {
			continue
		}
		row.Layout(tv.rowRect(i, contentW))
		dirty = dirty.Union(TickWidget(row, now))
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
