package widgets

import (
	"time"

	. "github.com/qizhanchan/qui"
)

// ListView is a virtualizing list — it only renders rows that intersect
// the viewport, so a 100k-row list is just as cheap to draw as a 10-row
// one. Data can come from a plain-text ListModel, or from concrete row
// widgets supplied via SetRowWidgets.
//
// Built-in features:
//   - Click to select, Up/Down arrow keys to navigate
//   - Auto-scroll to keep selection visible
//   - Mouse wheel scrolling
//   - Draggable scrollbar thumb + click-on-track page jumps
//   - Selection + hover visual states
//   - Optional row-widget mode (rows are real child widgets)
//
// Not yet supported (future iterations):
//   - Multi-select (Ctrl/Shift range)
//   - Variable-height rows
//   - Keyboard PageUp/Down/Home/End (KeyPageUp etc. not in enum yet)

// ListModel is the data source for a plain-text ListView.
type ListModel interface {
	RowCount() int
	RowText(index int) string
}

// SliceListModel is a trivial ListModel backed by a []string. Covers
// the common case where the caller just wants to show a flat list.
type SliceListModel struct {
	Items []string
}

func (s *SliceListModel) RowCount() int            { return len(s.Items) }
func (s *SliceListModel) RowText(index int) string { return s.Items[index] }

// NewListModel returns a SliceListModel for quick use.
func NewListModel(items []string) *SliceListModel {
	return &SliceListModel{Items: items}
}

// ListView is the virtualizing list widget.
type ListView struct {
	BaseWidget
	Model ListModel
	// rowWidgets switches ListView into widget-row mode when set via
	// SetRowWidgets. In this mode RowCount comes from len(rowWidgets)
	// and row rendering/hit-testing target these concrete widgets.
	rowWidgets    []Widget
	useWidgetRows bool

	SelectedIdx int
	selection   singleSelectionModel
	RowHeight   float32
	// OnSelect fires when SelectedIdx changes (click or keyboard).
	OnSelect func(index int)
	// OnActivate fires on Enter key when a row is selected — use this
	// for double-click-equivalent semantics (open file, drill in, etc.).
	OnActivate func(index int)

	scrollY  float32
	hoverIdx int
	focused  bool

	// Scrollbar drag state — mirrors ScrollView.
	barDragging        bool
	barDragStartY      float32
	barDragStartScroll float32
}

const (
	listRowDefaultH  = 26
	listBarW         = 10
	listMinThumbH    = 24
	listRowPaddingX  = 10
	listKeyScrollPad = 4 // rows of margin when auto-scrolling to selection
)

// NewListView constructs a ListView backed by the given model.
// SelectedIdx defaults to -1 (no selection); set it after construction
// to pre-select a row.
func NewListView(model ListModel) *ListView {
	lv := &ListView{
		BaseWidget:    NewBaseWidget(),
		Model:         model,
		SelectedIdx:   -1,
		selection:     newSingleSelectionModel(),
		RowHeight:     listRowDefaultH,
		hoverIdx:      -1,
		useWidgetRows: false,
	}
	lv.SetSelf(lv)
	lv.Style().Background = Color{R: 0.1, G: 0.1, B: 0.12, A: 1}
	lv.Style().Foreground = ColorWhite
	lv.Style().Border = Color{R: 0.3, G: 0.3, B: 0.35, A: 1}
	lv.Style().BorderSize = 1
	lv.Style().Radius = 3
	lv.Style().Font = Font{Size: 14}
	return lv
}

func (lv *ListView) Focusable() bool { return lv.Enabled() }
func (lv *ListView) CancelInteraction() {
	if lv.barDragging {
		lv.barDragging = false
		lv.Invalidate()
	}
}
func (lv *ListView) SetFocused(f bool) {
	if lv.focused == f {
		return
	}
	lv.focused = f
	lv.Invalidate()
}

// ChildList exposes row widgets to framework tree walks (focus
// collection, etc.) when ListView is in widget-row mode.
func (lv *ListView) ChildList() []Widget {
	if !lv.useWidgetRows {
		return nil
	}
	return lv.rowWidgets
}

// RowCount returns the count from the active row source.
func (lv *ListView) RowCount() int {
	if lv.useWidgetRows {
		return len(lv.rowWidgets)
	}
	if lv.Model == nil {
		return 0
	}
	return lv.Model.RowCount()
}

// SelectedText returns the text of the currently-selected row in
// text-model mode, or "" in widget-row mode.
func (lv *ListView) SelectedText() string {
	if lv.useWidgetRows {
		return ""
	}
	if lv.Model == nil || lv.SelectedIdx < 0 || lv.SelectedIdx >= lv.Model.RowCount() {
		return ""
	}
	return lv.Model.RowText(lv.SelectedIdx)
}

// SetModel swaps in a new plain-text model and leaves widget-row mode.
// Selection and scroll are reset because previous indices are no
// longer meaningful.
func (lv *ListView) SetModel(m ListModel) {
	lv.detachRowWidgets()
	lv.Model = m
	lv.useWidgetRows = false
	lv.selection.Reset()
	lv.SelectedIdx = lv.selection.Index()
	lv.scrollY = 0
	lv.hoverIdx = -1
	lv.InvalidateLayout()
}

// SetRowWidgets switches ListView to widget-row mode.
//
// Rows are treated as real child widgets for hit-testing and event
// routing. Parent pointers are wired to this ListView so capture/
// bubble paths include the list.
func (lv *ListView) SetRowWidgets(rows []Widget) {
	if lv.useWidgetRows && lv.Model == nil && sameWidgetSlice(lv.rowWidgets, rows) {
		return
	}
	lv.Model = nil
	lv.useWidgetRows = true
	oldRows := append([]Widget(nil), lv.rowWidgets...)
	for _, old := range oldRows {
		if old != nil && !widgetSliceContains(rows, old) {
			lv.ReleaseChildForTransfer(old)
			DetachWidgetTree(old)
		}
	}

	var parent Widget = lv
	if s := lv.Self(); s != nil {
		parent = s
	}
	accepted := make([]Widget, 0, len(rows))
	for _, row := range rows {
		if row != nil && !widgetSliceContains(accepted, row) && AdoptWidgetTree(row, parent, lv.Window()) {
			accepted = append(accepted, row)
		}
	}
	lv.rowWidgets = accepted

	count := len(lv.rowWidgets)
	lv.selection.Sync(lv.SelectedIdx)
	lv.selection.Clamp(count)
	lv.SelectedIdx = lv.selection.Index()
	if lv.hoverIdx >= count {
		lv.hoverIdx = -1
	}
	lv.ClampScroll()
	lv.InvalidateLayout()
}

func sameWidgetSlice(a, b []Widget) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func widgetSliceContains(widgets []Widget, target Widget) bool {
	for _, widget := range widgets {
		if widget == target {
			return true
		}
	}
	return false
}

func (lv *ListView) detachRowWidgets() {
	rows := append([]Widget(nil), lv.rowWidgets...)
	for _, row := range rows {
		if row == nil {
			continue
		}
		lv.ReleaseChildForTransfer(row)
		DetachWidgetTree(row)
	}
	lv.rowWidgets = nil
}

func (lv *ListView) ReleaseChildForTransfer(child Widget) bool {
	for i, row := range lv.rowWidgets {
		if row != child {
			continue
		}
		lv.rowWidgets = append(lv.rowWidgets[:i], lv.rowWidgets[i+1:]...)
		if child.Parent() == lv {
			child.SetParent(nil)
		}
		lv.InvalidateLayout()
		return true
	}
	return false
}

// Select changes the selection and auto-scrolls to keep the row visible.
func (lv *ListView) Select(idx int) {
	count := lv.RowCount()
	lv.selection.Sync(lv.SelectedIdx)
	idx, changed := lv.selection.Select(idx, count)
	lv.SelectedIdx = lv.selection.Index()
	if !changed {
		return
	}
	if idx < 0 {
		lv.Invalidate()
		return
	}
	lv.ensureVisible(idx)
	lv.Invalidate()
	if lv.OnSelect != nil {
		lv.OnSelect(idx)
	}
}

// Measure reports a small natural size. Returning `available` here
// would make Flex see the list's basis as the whole container, push
// basisSum past the axis length, and (because Shrink is 0 by default)
// keep every sibling at its over-sized basis — the list would extend
// past the container and squeeze neighbors off-screen. Callers grow
// the list with FlexItem.Grow when they want it to fill space.
func (lv *ListView) Measure(available Size) Size {
	width := float32(240)
	height := float32(160)
	if available.W > 0 && width > available.W {
		width = available.W
	}
	if available.H > 0 && height > available.H {
		height = available.H
	}
	return Size{W: width, H: height}
}

// contentHeight is the total height needed to render every row.
func (lv *ListView) contentHeight() float32 {
	return fixedRowContentHeight(lv.RowCount(), lv.RowHeight)
}

func (lv *ListView) maxScroll() float32 {
	over := lv.contentHeight() - lv.Bounds().H
	if over <= 0 {
		return 0
	}
	return over
}

func (lv *ListView) hasScrollbar() bool { return lv.maxScroll() > 0 }

func (lv *ListView) contentWidth() float32 {
	contentW := lv.Bounds().W
	if lv.hasScrollbar() {
		contentW -= listBarW
	}
	return contentW
}

func (lv *ListView) rowRect(index int, contentW float32) Rect {
	return fixedRowRect(lv.Bounds(), index, lv.RowHeight, lv.scrollY, contentW)
}

func (lv *ListView) ClampScroll() {
	max := lv.maxScroll()
	if lv.scrollY < 0 {
		lv.scrollY = 0
	}
	if lv.scrollY > max {
		lv.scrollY = max
	}
}

// visibleRange returns the half-open [start, end) range of rows that
// intersect the viewport at the current scrollY. Used by Draw to skip
// every out-of-view row — the core of the virtualization.
func (lv *ListView) visibleRange() (int, int) {
	return fixedRowVisibleRange(lv.RowCount(), lv.RowHeight, lv.scrollY, lv.Bounds())
}

// ensureVisible nudges scrollY so row `idx` falls inside the viewport
// with one row of breathing room (capped to 1/3 of the viewport so a
// tiny viewport doesn't push the selected row off).
func (lv *ListView) ensureVisible(idx int) {
	lv.scrollY = fixedRowEnsureVisible(lv.scrollY, idx, lv.RowCount(), lv.RowHeight, lv.Bounds())
}

func (lv *ListView) Draw(canvas Canvas) {
	b := lv.Bounds()
	// Background + border.
	if lv.Style().Background.A > 0 {
		canvas.FillRoundedRect(b, lv.Style().Radius, lv.Style().Background)
	}

	// Clip row drawing to the content area (excluding scrollbar column).
	contentW := lv.contentWidth()
	contentClip := Rect{X: b.X, Y: b.Y, W: contentW, H: b.H}
	var effectiveClip Rect
	if ca, ok := canvas.(ClipAware); ok {
		effectiveClip = ca.ClipBoundsLogical().Intersect(contentClip)
	} else {
		effectiveClip = contentClip
	}
	if !effectiveClip.IsEmpty() {
		id := canvas.Save()
		canvas.ClipRect(effectiveClip)
		if lv.useWidgetRows {
			lv.drawRowWidgets(canvas, contentW)
		} else {
			lv.drawRows(canvas, contentW)
		}
		canvas.RestoreTo(id)
	}

	if lv.hasScrollbar() {
		lv.drawScrollbar(canvas)
	}

	// Outer border last so it's not clipped by the row scope.
	if lv.Style().BorderSize > 0 {
		canvas.StrokeRect(b, lv.Style().Border, lv.Style().BorderSize)
	}
}

func (lv *ListView) drawRows(canvas Canvas, contentW float32) {
	start, end := lv.visibleRange()
	selectedBg := Color{R: 0.25, G: 0.5, B: 1.0, A: 1}
	hoverBg := Color{R: 0.18, G: 0.18, B: 0.22, A: 1}

	for i := start; i < end; i++ {
		rowRect := lv.rowRect(i, contentW)

		// Row state coloring — selection wins over hover.
		if i == lv.SelectedIdx {
			canvas.FillRect(rowRect, selectedBg)
		} else if i == lv.hoverIdx {
			canvas.FillRect(rowRect, hoverBg)
		}

		// Row text.
		textRect := Rect{
			X: rowRect.X + listRowPaddingX,
			Y: rowRect.Y + 4,
			W: rowRect.W - 2*listRowPaddingX,
			H: rowRect.H - 8,
		}
		canvas.DrawText(lv.Model.RowText(i), textRect, lv.Style().Foreground, lv.Style().Font)
	}
}

func (lv *ListView) drawRowWidgets(canvas Canvas, contentW float32) {
	start, end := lv.visibleRange()
	if start == end {
		return
	}
	selectedBg := Color{R: 0.25, G: 0.5, B: 1.0, A: 1}
	hoverBg := Color{R: 0.18, G: 0.18, B: 0.22, A: 1}

	for i := start; i < end; i++ {
		rowRect := lv.rowRect(i, contentW)

		if i == lv.SelectedIdx {
			canvas.FillRect(rowRect, selectedBg)
		} else if i == lv.hoverIdx {
			canvas.FillRect(rowRect, hoverBg)
		}

		if i < 0 || i >= len(lv.rowWidgets) {
			continue
		}
		row := lv.rowWidgets[i]
		if row == nil {
			continue
		}
		row.Layout(rowRect)
		WithClipRect(canvas, rowRect, row.Draw)
	}
}

// Tick forwards per-frame updates to visible row widgets in
// widget-row mode.
func (lv *ListView) Tick(now time.Time) Rect {
	if !lv.useWidgetRows {
		return Rect{}
	}
	start, end := lv.visibleRange()
	if start == end {
		return Rect{}
	}
	contentW := lv.contentWidth()
	var dirty Rect
	for i := start; i < end; i++ {
		if i < 0 || i >= len(lv.rowWidgets) {
			continue
		}
		row := lv.rowWidgets[i]
		if row == nil {
			continue
		}
		row.Layout(lv.rowRect(i, contentW))
		dirty = dirty.Union(TickWidget(row, now))
	}
	return dirty
}

func (lv *ListView) Handle(event Event) bool {
	if !lv.Enabled() {
		return false
	}
	switch e := event.(type) {
	case MouseEvent:
		switch e.Type() {
		case EventMouseEnter:
			// Hover tracking starts on the next MouseMove with a real point.
		case EventMouseLeave:
			if lv.hoverIdx != -1 {
				lv.hoverIdx = -1
				lv.Invalidate()
			}
		case EventMouseMove:
			if idx := lv.rowAt(e.X, e.Y); idx != lv.hoverIdx {
				lv.hoverIdx = idx
				lv.Invalidate()
			}
			if lv.barDragging {
				lv.barDragMove(e.Y)
				return true
			}
		case EventScroll:
			if lv.Bounds().Contains(Point{X: e.X, Y: e.Y}) {
				lv.scrollBy(-e.DeltaY * lv.RowHeight)
				return true
			}
		case EventMouseDown:
			if e.Button != MouseButtonLeft {
				return false
			}
			if !lv.Bounds().Contains(Point{X: e.X, Y: e.Y}) {
				return false
			}
			// Scrollbar first.
			if lv.hasScrollbar() {
				thumb := lv.barThumb()
				if thumb.Contains(Point{X: e.X, Y: e.Y}) {
					lv.barDragging = true
					lv.barDragStartY = e.Y
					lv.barDragStartScroll = lv.scrollY
					lv.Invalidate()
					return true
				}
				if lv.barTrack().Contains(Point{X: e.X, Y: e.Y}) {
					if e.Y < thumb.Y {
						lv.scrollBy(-lv.Bounds().H * 0.9)
					} else {
						lv.scrollBy(lv.Bounds().H * 0.9)
					}
					return true
				}
			}
			// In widget-row mode, capture updates selection but does not
			// consume. This lets descendant interactive widgets (buttons,
			// inputs) still receive target-phase MouseDown/Up.
			if lv.useWidgetRows && e.Phase() == PhaseCapture {
				if idx := lv.rowAt(e.X, e.Y); idx >= 0 {
					lv.Select(idx)
				}
				return false
			}
			// Row click.
			if idx := lv.rowAt(e.X, e.Y); idx >= 0 {
				lv.Select(idx)
				return true
			}
		case EventMouseUp:
			if lv.barDragging {
				lv.barDragging = false
				lv.Invalidate()
				return true
			}
		}
	case KeyEvent:
		if !lv.focused || e.Type() != EventKeyDown {
			return false
		}
		switch e.Key {
		case KeyDown:
			next := lv.SelectedIdx + 1
			if lv.SelectedIdx < 0 {
				next = 0
			}
			lv.Select(next)
			return true
		case KeyUp:
			if lv.SelectedIdx > 0 {
				lv.Select(lv.SelectedIdx - 1)
			}
			return true
		case KeyEnter:
			if lv.OnActivate != nil && lv.SelectedIdx >= 0 {
				lv.OnActivate(lv.SelectedIdx)
			}
			return true
		}
	}
	return false
}

func (lv *ListView) HitTest(p Point) Widget {
	if !lv.Bounds().Contains(p) {
		return nil
	}
	if lv.useWidgetRows {
		contentW := lv.contentWidth()
		if p.X <= lv.Bounds().X+contentW {
			start, end := lv.visibleRange()
			for i := end - 1; i >= start; i-- {
				if i < 0 || i >= len(lv.rowWidgets) {
					continue
				}
				row := lv.rowWidgets[i]
				if row == nil {
					continue
				}
				rowRect := lv.rowRect(i, contentW)
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
	return lv
}

// rowAt returns the row index at the given point, or -1 if the point
// is outside the list or on the scrollbar.
func (lv *ListView) rowAt(x, y float32) int {
	return fixedRowAt(lv.Bounds(), lv.contentWidth(), lv.RowCount(), lv.RowHeight, lv.scrollY, x, y)
}

// scrollBy offsets scrollY with clamping.
func (lv *ListView) scrollBy(dy float32) {
	old := lv.scrollY
	lv.scrollY += dy
	lv.ClampScroll()
	if lv.scrollY != old {
		lv.Invalidate()
	}
}

// ScrollTo sets scrollY explicitly, clamped.
func (lv *ListView) ScrollTo(y float32) {
	old := lv.scrollY
	lv.scrollY = y
	lv.ClampScroll()
	if lv.scrollY != old {
		lv.Invalidate()
	}
}

// ----------------------------------------------------------------------
// Scrollbar (mirrors ScrollView's logic — candidate for extraction
// once we have a third consumer).

func (lv *ListView) barTrack() Rect {
	b := lv.Bounds()
	return Rect{X: b.X + b.W - listBarW, Y: b.Y, W: listBarW, H: b.H}
}

func (lv *ListView) barThumb() Rect {
	track := lv.barTrack()
	content := lv.contentHeight()
	if content <= 0 {
		return track
	}
	thumbH := track.H * track.H / content
	if thumbH < listMinThumbH {
		thumbH = listMinThumbH
	}
	if thumbH > track.H {
		thumbH = track.H
	}
	travel := track.H - thumbH
	progress := float32(0)
	if max := lv.maxScroll(); max > 0 {
		progress = lv.scrollY / max
	}
	return Rect{X: track.X, Y: track.Y + travel*progress, W: track.W, H: thumbH}
}

func (lv *ListView) barDragMove(y float32) {
	track := lv.barTrack()
	thumb := lv.barThumb()
	travel := track.H - thumb.H
	if travel <= 0 {
		return
	}
	dy := y - lv.barDragStartY
	scrollPerPixel := lv.maxScroll() / travel
	old := lv.scrollY
	lv.scrollY = lv.barDragStartScroll + dy*scrollPerPixel
	lv.ClampScroll()
	if lv.scrollY != old {
		lv.Invalidate()
	}
}

func (lv *ListView) drawScrollbar(canvas Canvas) {
	track := lv.barTrack()
	thumb := lv.barThumb()
	canvas.FillRoundedRect(track, 2, Color{R: 0.06, G: 0.06, B: 0.08, A: 1})
	color := Color{R: 0.4, G: 0.4, B: 0.45, A: 1}
	if lv.barDragging {
		color = Color{R: 0.6, G: 0.6, B: 0.65, A: 1}
	}
	canvas.FillRoundedRect(thumb, 2, color)
}
