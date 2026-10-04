package widgets

import (
	"time"

	. "github.com/qizhanchan/qui"
)

// Dialog is a modal overlay that centers a content box over the window,
// dims the background, and blocks mouse/keyboard interaction with the
// main tree until closed. Implements modalOverlay so focus.go's Tab
// collection restricts Tab cycling to inside the dialog.
//
// Layout convention:
//
//	┌────────────────── backdrop (full window, dimmed) ──────────────┐
//	│              ┌───── contentBounds (centered) ───────┐          │
//	│              │  Title  (or the Header widget)       │          │
//	│              │                                      │          │
//	│              │  Content widget                      │          │
//	│              │                                      │          │
//	│              │                         [Cancel][OK] │          │
//	│              └──────────────────────────────────────┘          │
//	└────────────────────────────────────────────────────────────────┘
//
// Usage:
//
//	dlg := widgets.NewDialog("Save changes?", bodyContent)
//	dlg.AddButton("Cancel", nil)          // nil onClick = just close
//	save := dlg.AddButton("Save", saveDocument)
//	dlg.DefaultAction = save              // Enter presses Save
//	dlg.Show(window)
//
// Actions are arbitrary widgets. AddButton is the shorthand for a plain
// button that closes the dialog after its callback; AddAction appends any
// widget (an icon button, a destructive-styled button, a checkbox) and
// leaves closing to the caller — the shape for validation:
//
//	save := widgets.NewButton("Save", nil)
//	save.OnClick = func() {
//		if err := validate(); err != nil {
//			errLabel.SetText(err.Error()) // dialog stays open
//			return
//		}
//		dlg.CloseWith(widgets.DialogCloseAction)
//	}
//	dlg.AddAction(save)
//
// The content area clips its widget. Content that can outgrow the window
// should be wrapped in a ScrollView by the caller.
type Dialog struct {
	BaseWidget
	// Title is the literal headline; SetTitleKey localizes it. Ignored
	// when Header is set.
	Title string
	// Header, when non-nil, replaces the default title row — a custom
	// headline with an icon, a close ✕, a step indicator. It is laid out
	// across the headline row at its measured height.
	Header  Widget
	Content Widget
	// Actions is the bottom row, laid out per ActionsAlign at each
	// widget's measured width. Append with AddButton / AddAction.
	Actions      []Widget
	ActionsAlign DialogActionsAlign

	// DefaultAction is pressed by Enter while the dialog is up (unless the
	// focused widget consumed the key first — a TextArea's newline, a
	// focused button pressing itself). A *Button fires its OnClick; any
	// other widget needs an Activate() method.
	DefaultAction Widget
	// InitialFocus receives focus on Show. Nil = the first focusable
	// widget inside the dialog.
	InitialFocus Widget
	// DismissOnBackdrop closes the dialog (DialogCloseBackdrop) on a
	// click outside the box. Off by default: a backdrop click is absorbed.
	DismissOnBackdrop bool
	// CanClose vetoes a user-initiated close (an AddButton action,
	// Escape, a backdrop click, or CloseWith): returning false keeps the
	// dialog open. Close() — the app closing it — is never vetoed.
	CanClose func(reason DialogCloseReason) bool
	// OnClose fires after the dialog leaves the window, with the reason.
	OnClose func(reason DialogCloseReason)

	// Width / Height override the default box dimensions when > 0.
	// Set them directly or via SetSize. The Metrics width band only
	// applies when Width is zero (the "alert" preset); a non-zero Width
	// is honored as-is, clamped only against the window edges.
	// Height defaults to whatever the header+content+actions sum to.
	Width  float32
	Height float32

	// Metrics overrides the spacing. Nil = DefaultDialogMetrics().
	Metrics *DialogMetrics

	// Color knobs. A zero ContainerColor / TitleColor resolves the theme
	// (SurfaceOverlay / Text) at draw time, so a theme switch restyles
	// an open dialog. ScrimColor is painted over the whole window; A=0
	// skips it.
	ScrimColor      Color
	ContainerColor  Color
	TitleColor      Color
	ContainerRadius float32
	// ContainerElevation is the Theme.Elevation index for the box
	// shadow. 0 = flat.
	ContainerElevation int

	titleKey      messageKey
	window        *Window
	contentBounds Rect
}

// DialogCloseReason says why a dialog closed — passed to CanClose and
// OnClose.
type DialogCloseReason int

const (
	// DialogCloseProgrammatic: the app called Close.
	DialogCloseProgrammatic DialogCloseReason = iota
	// DialogCloseAction: an AddButton action, or CloseWith from an action.
	DialogCloseAction
	// DialogCloseEscape: the user pressed Escape.
	DialogCloseEscape
	// DialogCloseBackdrop: a click outside the box (DismissOnBackdrop).
	DialogCloseBackdrop
)

func (r DialogCloseReason) String() string {
	switch r {
	case DialogCloseAction:
		return "action"
	case DialogCloseEscape:
		return "escape"
	case DialogCloseBackdrop:
		return "backdrop"
	}
	return "programmatic"
}

// DialogActionsAlign places the action row's widgets.
type DialogActionsAlign int

const (
	// DialogActionsEnd right-aligns the actions (the platform default).
	DialogActionsEnd DialogActionsAlign = iota
	// DialogActionsStart left-aligns them.
	DialogActionsStart
	// DialogActionsSpaceBetween pins the first action left and the last
	// right, spreading the rest between.
	DialogActionsSpaceBetween
	// DialogActionsStretch gives every action an equal share of the row.
	DialogActionsStretch
)

// DialogMetrics is the dialog's spacing. The defaults are a 24 px inset,
// a 40 px action row and an alert box inside a 280–560 band.
type DialogMetrics struct {
	HeadlinePadTop float32 // above the title / Header
	HeadlinePadX   float32
	HeadlineHeight float32 // the default title row (Header measures its own)

	ContentPadTop    float32
	ContentPadX      float32
	ContentPadBottom float32 // when actions follow; otherwise ContentPadTop

	ActionsPadTop    float32
	ActionsPadX      float32
	ActionsPadBottom float32
	ActionsHeight    float32 // minimum action-row height
	ActionsGap       float32

	MinWidth, MaxWidth, DefaultWidth float32 // the Width==0 band
	WindowMargin                     float32 // min gap to each window edge
}

// DefaultDialogMetrics returns the stock spacing.
func DefaultDialogMetrics() DialogMetrics {
	return DialogMetrics{
		HeadlinePadTop: 24, HeadlinePadX: 24, HeadlineHeight: 32,
		ContentPadTop: 24, ContentPadX: 24, ContentPadBottom: 8,
		ActionsPadTop: 16, ActionsPadX: 24, ActionsPadBottom: 24,
		ActionsHeight: 40, ActionsGap: 8,
		MinWidth: 280, MaxWidth: 560, DefaultWidth: 312,
		WindowMargin: 24,
	}
}

var defaultDialogMetrics = DefaultDialogMetrics()

func (d *Dialog) metrics() *DialogMetrics {
	if d.Metrics != nil {
		return d.Metrics
	}
	return &defaultDialogMetrics
}

// NewDialog creates a plain dialog with the given title and body
// content. Look: theme-colored rounded box, thin translucent-black
// scrim, no shadow. For a designed look, set the color knobs, or render
// a dialog on the htmlcss layer with h.Dialog.
func NewDialog(title string, content Widget) *Dialog {
	d := &Dialog{
		BaseWidget:      NewBaseWidget(),
		Title:           title,
		Content:         content,
		ScrimColor:      Color{R: 0, G: 0, B: 0, A: 0.32},
		ContainerRadius: 8,
	}
	d.SetSelf(d)
	if content != nil {
		content.SetParent(d)
	}
	return d
}

// Modal marks this overlay as focus-trapping. Satisfies modalOverlay.
func (d *Dialog) Modal() bool { return true }

// SetTitleKey makes the title come from the message catalog. Pass "" to
// go back to the literal Title field.
func (d *Dialog) SetTitleKey(key string, args ...any) {
	if d.titleKey.set(key, args) {
		d.InvalidateLayout()
	}
}

// TitleKey returns the title's message key, or "".
func (d *Dialog) TitleKey() string { return d.titleKey.key }

// DisplayTitle is the title the dialog renders: the resolved message
// when a title key is set, otherwise Title.
func (d *Dialog) DisplayTitle() string { return d.titleKey.resolve(d.Title) }

// SetHeader replaces the default title row with w (nil restores it).
func (d *Dialog) SetHeader(w Widget) {
	d.Header = w
	if w != nil {
		w.SetParent(d)
	}
	d.InvalidateLayout()
}

// SetSize overrides the dialog box dimensions. Pass 0 for either axis to
// fall back to the default for that axis — the Metrics width band, and
// height = headline + content + actions.
func (d *Dialog) SetSize(width, height float32) {
	d.Width = width
	d.Height = height
	d.InvalidateLayout()
}

// AddButton appends a plain button that runs onClick and then closes the
// dialog (DialogCloseAction, subject to CanClose). onClick may be nil —
// the button just closes, the usual "Cancel". Restyle the returned
// button through its States, or use AddAction for a widget that decides
// for itself when to close.
func (d *Dialog) AddButton(text string, onClick func()) *Button {
	btn := NewButton(text, nil)
	btn.OnClick = func() {
		if onClick != nil {
			onClick()
		}
		d.CloseWith(DialogCloseAction)
	}
	d.AddAction(btn)
	return btn
}

// AddAction appends any widget to the action row. It never closes the
// dialog by itself; its handler calls CloseWith / Close when done.
func (d *Dialog) AddAction(w Widget) {
	if w == nil {
		return
	}
	w.SetParent(d)
	d.Actions = append(d.Actions, w)
	d.InvalidateLayout()
}

// Show pushes the dialog onto the window's overlay stack and focuses
// InitialFocus, or else the first focusable widget inside. No-op if
// already shown.
func (d *Dialog) Show(w *Window) {
	if w == nil || d.window != nil {
		return
	}
	d.window = w
	// Full-window rect = the backdrop dims the whole view.
	d.Layout(Rect{X: 0, Y: 0, W: w.Size().W, H: w.Size().H})
	w.PushOverlay(d)
	if d.InitialFocus != nil {
		w.SetFocus(d.InitialFocus)
		if w.Focused() == d.InitialFocus {
			return
		}
	}
	// The modal trap scopes CollectFocusables to this dialog.
	for _, wd := range w.CollectFocusables() {
		w.SetFocus(wd)
		break
	}
}

// OnWindowResize re-centers the dialog when the window is resized.
// Satisfies OverlayResizer.
func (d *Dialog) OnWindowResize(newSize Size) { d.RelayoutOverlay(newSize) }

// RelayoutOverlay re-lays the dialog out against the full window — after
// a resize, or when its content changed size while shown (satisfies
// OverlayLayouter). The box may move or resize inside the unchanged
// full-window bounds, so the whole window is repainted.
func (d *Dialog) RelayoutOverlay(winSize Size) {
	if d.window == nil {
		return
	}
	d.Layout(Rect{X: 0, Y: 0, W: winSize.W, H: winSize.H})
	d.window.InvalidateRect(d.Bounds())
}

// Close removes the dialog and fires OnClose(DialogCloseProgrammatic).
// Not subject to CanClose. Safe to call when not shown.
func (d *Dialog) Close() { d.close(DialogCloseProgrammatic) }

// CloseWith closes the dialog for reason, first asking CanClose — the
// call for an action widget's own handler. Programmatic reasons skip the
// veto, like Close.
func (d *Dialog) CloseWith(reason DialogCloseReason) {
	if d.window == nil {
		return
	}
	if reason != DialogCloseProgrammatic && d.CanClose != nil && !d.CanClose(reason) {
		return
	}
	d.close(reason)
}

func (d *Dialog) close(reason DialogCloseReason) {
	if d.window == nil {
		return
	}
	d.window.RemoveOverlay(d)
	d.window = nil
	if d.OnClose != nil {
		d.OnClose(reason)
	}
}

// IsShown reports whether the dialog is on a window's overlay stack.
func (d *Dialog) IsShown() bool { return d.window != nil }

// activateDefault presses DefaultAction. Reports whether it fired.
func (d *Dialog) activateDefault() bool {
	switch a := d.DefaultAction.(type) {
	case nil:
		return false
	case *Button:
		if !a.Focusable() || a.OnClick == nil { // Focusable = enabled and not disabled
			return false
		}
		a.OnClick()
		return true
	case interface{ Activate() }:
		if !d.DefaultAction.Enabled() {
			return false
		}
		a.Activate()
		return true
	}
	return false
}

// Tick fans frame ticks out to the header, content and actions. Dialog
// is Tickable, so the window's tick walk hands it the whole subtree;
// TickWidget recurses through children that aren't Tickable themselves
// (a ScrollView wrapping a form), so a caret or hover transition deep in
// the content still advances.
func (d *Dialog) Tick(now time.Time) Rect {
	var dirty Rect
	for _, w := range d.ChildList() {
		dirty = dirty.Union(TickWidget(w, now))
	}
	return dirty
}

// ChildList exposes header, content and actions for focus / tick / AX.
func (d *Dialog) ChildList() []Widget {
	children := make([]Widget, 0, 2+len(d.Actions))
	if d.Header != nil {
		children = append(children, d.Header)
	}
	if d.Content != nil {
		children = append(children, d.Content)
	}
	return append(children, d.Actions...)
}

// Measure returns the natural content box size. When Width/Height are
// unset, width is pinned to the Metrics default width (clamped to the
// min/max band) and height is the section sum. A non-zero Width or
// Height bypasses that band for that axis — the only clamp is the
// window-edge clamp in Layout.
func (d *Dialog) Measure(available Size) Size {
	m := d.metrics()
	var boxW float32
	if d.Width > 0 {
		boxW = d.Width
	} else {
		boxW = m.DefaultWidth
		if available.W > 0 && available.W < boxW {
			boxW = available.W
		}
		if boxW < m.MinWidth {
			boxW = m.MinWidth
		}
		if boxW > m.MaxWidth {
			boxW = m.MaxWidth
		}
	}
	boxH := d.Height
	if boxH <= 0 {
		boxH = d.boxHeight(boxW)
	}
	return Size{W: boxW, H: boxH}
}

// headerHeight is the headline row: top pad + the Header's measured
// height, or + the title line; zero with neither.
func (d *Dialog) headerHeight(boxW float32) float32 {
	m := d.metrics()
	switch {
	case d.Header != nil:
		sz := MeasureConstrained(d.Header, Size{W: boxW - 2*m.HeadlinePadX, H: 1 << 20})
		return m.HeadlinePadTop + sz.H
	case d.DisplayTitle() != "":
		return m.HeadlinePadTop + m.HeadlineHeight
	}
	return 0
}

// actionsRowHeight is the tallest action, at least ActionsHeight.
func (d *Dialog) actionsRowHeight(boxW float32) float32 {
	m := d.metrics()
	h := m.ActionsHeight
	for _, a := range d.Actions {
		if sz := MeasureConstrained(a, Size{W: boxW, H: m.ActionsHeight}); sz.H > h {
			h = sz.H
		}
	}
	return h
}

func (d *Dialog) actionsHeight(boxW float32) float32 {
	if len(d.Actions) == 0 {
		return 0
	}
	m := d.metrics()
	return m.ActionsPadTop + d.actionsRowHeight(boxW) + m.ActionsPadBottom
}

func (d *Dialog) contentPadBottom() float32 {
	m := d.metrics()
	if len(d.Actions) > 0 {
		return m.ContentPadBottom
	}
	return m.ContentPadTop
}

// boxHeight sums headline + content + actions for a box of width boxW.
// Sections without their optional element collapse to zero.
func (d *Dialog) boxHeight(boxW float32) float32 {
	m := d.metrics()
	h := d.headerHeight(boxW)
	if d.Content != nil {
		size := MeasureConstrained(d.Content, Size{W: boxW - 2*m.ContentPadX, H: 1 << 20})
		h += m.ContentPadTop + size.H + d.contentPadBottom()
	}
	return h + d.actionsHeight(boxW)
}

func (d *Dialog) Layout(rect Rect) {
	d.BaseWidget.Layout(rect)
	m := d.metrics()

	// Two layout modes:
	//   - Embedded (no window): rect IS the content box. Used by golden
	//     snapshots and any caller embedding the dialog inline.
	//   - Modal (after Show): rect is the full window. Compute box
	//     size from content, clamp to the window margins, center.
	var box Rect
	if d.window == nil {
		box = rect
	} else {
		size := d.Measure(Size{W: rect.W, H: rect.H})
		if maxByWin := rect.W - 2*m.WindowMargin; size.W > maxByWin {
			size.W = maxByWin
		}
		if maxByWin := rect.H - 2*m.WindowMargin; size.H > maxByWin {
			size.H = maxByWin
		}
		box = Rect{
			X: rect.X + (rect.W-size.W)/2,
			Y: rect.Y + (rect.H-size.H)/2,
			W: size.W,
			H: size.H,
		}
	}
	d.contentBounds = box

	headlineH := d.headerHeight(box.W)
	if d.Header != nil {
		d.Header.Layout(Rect{
			X: box.X + m.HeadlinePadX,
			Y: box.Y + m.HeadlinePadTop,
			W: box.W - 2*m.HeadlinePadX,
			H: headlineH - m.HeadlinePadTop,
		})
	}
	actionsH := d.actionsHeight(box.W)
	if d.Content != nil {
		h := box.H - headlineH - m.ContentPadTop - d.contentPadBottom() - actionsH
		if h < 0 {
			h = 0
		}
		d.Content.Layout(Rect{
			X: box.X + m.ContentPadX,
			Y: box.Y + headlineH + m.ContentPadTop,
			W: box.W - 2*m.ContentPadX,
			H: h,
		})
	}
	if len(d.Actions) > 0 {
		d.layoutActions(box, actionsH)
	}
}

// layoutActions places the action row at the bottom of box per
// ActionsAlign, each action at its measured width (Stretch: equal
// shares) and the row's height.
func (d *Dialog) layoutActions(box Rect, actionsH float32) {
	m := d.metrics()
	n := len(d.Actions)
	rowH := actionsH - m.ActionsPadTop - m.ActionsPadBottom
	y := box.Y + box.H - m.ActionsPadBottom - rowH
	left := box.X + m.ActionsPadX
	avail := box.W - 2*m.ActionsPadX

	widths := make([]float32, n)
	var total float32
	for i, a := range d.Actions {
		widths[i] = MeasureConstrained(a, Size{W: avail, H: rowH}).W
		total += widths[i]
	}
	gap := m.ActionsGap
	x := left
	switch d.ActionsAlign {
	case DialogActionsStretch:
		share := (avail - gap*float32(n-1)) / float32(n)
		for i := range widths {
			widths[i] = share
		}
	case DialogActionsSpaceBetween:
		if n > 1 {
			if g := (avail - total) / float32(n-1); g > gap {
				gap = g
			}
		}
	case DialogActionsStart:
	default: // DialogActionsEnd
		x = left + avail - total - gap*float32(n-1)
	}
	for i, a := range d.Actions {
		a.Layout(Rect{X: x, Y: y, W: widths[i], H: rowH})
		x += widths[i] + gap
	}
}

func (d *Dialog) Draw(canvas Canvas) {
	// Scrim only paints when mounted (golden snapshots + embedded
	// usage render just the content box). A zero-A ScrimColor also
	// suppresses the scrim.
	if d.window != nil && d.ScrimColor.A > 0 {
		canvas.FillRect(d.Bounds(), d.ScrimColor)
	}

	// Container. Elevation renders BEFORE fill so shadow sits under
	// the surface.
	box := d.contentBounds
	if d.ContainerElevation > 0 {
		DrawElevation(canvas, box, d.ContainerRadius, d.ContainerElevation)
	}
	fill := d.ContainerColor
	if fill == (Color{}) {
		fill = CurrentTheme().SurfaceOverlay
	}
	if fill.A > 0 {
		canvas.FillRoundedRect(box, d.ContainerRadius, fill)
	}

	m := d.metrics()
	if d.Header != nil {
		d.Header.Draw(canvas)
	} else if title := d.DisplayTitle(); title != "" {
		titleColor := d.TitleColor
		if titleColor == (Color{}) {
			titleColor = CurrentTheme().Text
		}
		titleRect := Rect{
			X: box.X + m.HeadlinePadX,
			Y: box.Y + m.HeadlinePadTop,
			W: box.W - 2*m.HeadlinePadX,
			H: m.HeadlineHeight,
		}
		canvas.DrawText(title, titleRect, titleColor, ThemeFont(TextHeading))
	}

	if d.Content != nil {
		// Clip to the content slot so content taller than the box never
		// paints over the headline or the actions.
		depth := canvas.Save()
		canvas.ClipRect(d.Content.Bounds())
		d.Content.Draw(canvas)
		canvas.RestoreTo(depth)
	}
	for _, a := range d.Actions {
		a.Draw(canvas)
	}
}

func (d *Dialog) Handle(event Event) bool {
	if ke, ok := event.(KeyEvent); ok {
		// Keys act on the way back up: during capture the dialog is an
		// ancestor of the focused widget, which gets first say over
		// Escape (closing its own popup) and Enter (a TextArea newline).
		if ke.Phase() == PhaseCapture || ke.Type() != EventKeyDown {
			return false
		}
		switch ke.Key {
		case KeyEscape:
			d.CloseWith(DialogCloseEscape)
			return true
		case KeyEnter:
			return d.activateDefault()
		}
		return false
	}
	// Mouse clicks outside the content box are absorbed so the backdrop
	// blocks interaction with whatever's underneath; they dismiss only
	// with DismissOnBackdrop.
	if me, ok := event.(MouseEvent); ok && me.Type() == EventMouseDown {
		if !d.contentBounds.Contains(Point{X: me.X, Y: me.Y}) {
			if d.DismissOnBackdrop {
				d.CloseWith(DialogCloseBackdrop)
			}
			return true
		}
	}
	return false
}

// HitTest: content box routes to children; outside routes to dialog
// itself so Handle can absorb clicks on the backdrop (modal).
func (d *Dialog) HitTest(p Point) Widget {
	if d.contentBounds.Contains(p) {
		for _, a := range d.Actions {
			if hit := a.HitTest(p); hit != nil {
				return hit
			}
		}
		if d.Header != nil {
			if hit := d.Header.HitTest(p); hit != nil {
				return hit
			}
		}
		if d.Content != nil && d.Content.Bounds().Contains(p) {
			if hit := d.Content.HitTest(p); hit != nil {
				return hit
			}
		}
	}
	// Absorb everything else — modal.
	return d
}
