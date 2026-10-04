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
//	│              │  Title                               │          │
//	│              │  ────────────                        │          │
//	│              │                                      │          │
//	│              │  Content widget                      │          │
//	│              │                                      │          │
//	│              │                         [Cancel][OK] │          │
//	│              └──────────────────────────────────────┘          │
//	└────────────────────────────────────────────────────────────────┘
//
// Usage:
//
//	dlg := qui.NewDialog("Save changes?", bodyContent)
//	dlg.AddButton("Cancel", nil)  // nil onClick = just close
//	dlg.AddButton("Save", func() { saveDocument() })
//	dlg.Show(window)
type Dialog struct {
	BaseWidget
	Title   string
	Content Widget
	Buttons []*Button
	OnClose func()

	// Width / Height override the default box dimensions when > 0.
	// Set them directly or via SetSize. The 280–560 width band only
	// applies when Width is zero (the "alert" preset); a non-zero Width
	// is honored as-is, clamped only against the window edges (rect-48).
	// Height defaults to whatever the content+actions sum to. This lets
	// callers build wider "settings"/"form" dialogs without forking.
	Width  float32
	Height float32

	// Color knobs. NewDialog fills them with a plain, HTML-modal look;
	// set them for a designed one (raised container fill, scrim,
	// elevation level), or render a dialog on the htmlcss layer inside
	// h.ModalPortal.
	ScrimColor      Color // painted over the whole window; A=0 skips
	ContainerColor  Color
	TitleColor      Color
	ContainerRadius float32
	// ContainerElevation is the Theme.Elevation index for the box
	// shadow. 0 = flat.
	ContainerElevation int

	window        *Window
	contentBounds Rect
}

// Dialog spacing. These are the plain numbers the layout uses — a
// 24 px content inset, a 40 px action-button row, an alert box that
// stays inside a 280–560 band.
const (
	dialogHeadlinePadTop   float32 = 24 // slotted[headline] padding 24 24 0
	dialogHeadlinePadX     float32 = 24
	dialogHeadlineLineH    float32 = 32 // headline-small line-height
	dialogContentPadTop    float32 = 24 // slotted[content] padding 24
	dialogContentPadX      float32 = 24
	dialogContentPadBottom float32 = 8  // has-actions override on content
	dialogActionsPadTop    float32 = 16 // slotted[actions] padding 16 24 24
	dialogActionsPadX      float32 = 24
	dialogActionsPadBottom float32 = 24
	dialogActionsButtonH   float32 = 40 // action-button container height
	dialogActionsGap       float32 = 8  // slotted[actions] gap
	dialogMinWidth         float32 = 280
	dialogMaxWidth         float32 = 560
	dialogScrimOpacity     float32 = 0.32
	dialogDefaultWidth     float32 = 312 // typical "alert" width for snapshot
)

// NewDialog creates a plain dialog with the given title and body
// content. Look: white rounded box, thin translucent-black scrim, no
// shadow. For a designed look, set the color knobs, or render a dialog
// on the htmlcss layer inside h.ModalPortal.
func NewDialog(title string, content Widget) *Dialog {
	d := &Dialog{
		BaseWidget:      NewBaseWidget(),
		Title:           title,
		Content:         content,
		ScrimColor:      Color{R: 0, G: 0, B: 0, A: 0.32},
		ContainerColor:  Color{R: 1, G: 1, B: 1, A: 1},
		TitleColor:      Color{R: 0.10, G: 0.10, B: 0.10, A: 1},
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

// SetSize overrides the dialog box dimensions. Pass 0 for either axis to
// fall back to the default for that axis — width 312 (clamped to
// 280–560), height = headline + content + actions. Call before Show, or
// re-call + Layout to re-size a visible dialog.
func (d *Dialog) SetSize(width, height float32) {
	d.Width = width
	d.Height = height
}

// AddButton appends an action button to the dialog's button row.
// onClick may be nil — in that case the button just closes the dialog
// (useful for "Cancel"-style buttons). A non-nil onClick fires, then
// the dialog auto-closes; callers that want to keep the dialog open
// after a button press (e.g., form validation) should call d.Show
// again or manage state externally.
func (d *Dialog) AddButton(text string, onClick func()) *Button {
	// Dialog uses the plain Button default; apps that want borderless
	// text-style actions (accent label, no fill) reassign btn.States
	// after AddButton returns.
	btn := NewButton(text, nil)
	btn.OnClick = func() {
		if onClick != nil {
			onClick()
		}
		d.Close()
	}
	btn.SetParent(d)
	d.Buttons = append(d.Buttons, btn)
	return btn
}

// Show pushes the dialog onto the window's overlay stack and auto-
// focuses the first focusable widget inside (title bar and buttons
// count). No-op if already shown.
func (d *Dialog) Show(w *Window) {
	if w == nil || d.window != nil {
		return
	}
	d.window = w
	// Full-window rect = the backdrop dims the whole view.
	d.Layout(Rect{X: 0, Y: 0, W: w.Size().W, H: w.Size().H})
	w.PushOverlay(d)
	// Auto-focus: find first focusable within this dialog after push.
	for _, wd := range w.CollectFocusables() {
		w.SetFocus(wd)
		break
	}
}

// OnWindowResize re-centers the dialog when the window is resized.
// Overlays aren't re-laid-out by the frame loop, so a centered dialog
// would otherwise drift off-center as the window grows/shrinks. Laying
// out against the new full-window rect re-centers via Dialog.Layout and
// invalidates the old+new extents. Satisfies OverlayResizer.
func (d *Dialog) OnWindowResize(newSize Size) {
	if d.window == nil {
		return
	}
	d.Layout(Rect{X: 0, Y: 0, W: newSize.W, H: newSize.H})
}

// Close removes the dialog from the window overlay stack and fires
// OnClose. Safe to call from button callbacks or externally.
func (d *Dialog) Close() {
	if d.window == nil {
		return
	}
	d.window.RemoveOverlay(d)
	d.window = nil
	if d.OnClose != nil {
		d.OnClose()
	}
}

// Tick fans frame ticks out to Content + action buttons. Without this
// Dialog's overlay subtree is invisible to Window.Step's tick loop
// (tickWidget doesn't recurse, and Dialog itself doesn't animate), so
// the action buttons' hover/ripple Transitions would never advance —
// MouseEnter would call hoverTrans.Begin once, the very next repaint
// captured t ≈ 0, and no further frames ever drove t→1. That's why a
// hovered Dialog action button looked stuck at a sub-transition tint
// while a normally-tree-mounted button reached its full state-layer.
// Returns the union of children's dirty rects so animation frames the
// window's dirty-region path correctly.
func (d *Dialog) Tick(now time.Time) Rect {
	var dirty Rect
	tick := func(w Widget) {
		if t, ok := w.(Tickable); ok {
			dirty = dirty.Union(t.Tick(now))
		}
	}
	if d.Content != nil {
		tick(d.Content)
	}
	for _, btn := range d.Buttons {
		tick(btn)
	}
	return dirty
}

// ChildList exposes content + buttons for the focus collector.
func (d *Dialog) ChildList() []Widget {
	var children []Widget
	if d.Content != nil {
		children = append(children, d.Content)
	}
	for _, btn := range d.Buttons {
		children = append(children, btn)
	}
	return children
}

// Measure returns the natural content box size. When Width/Height are
// unset, width is pinned to the alert width (clamped to 280–560) and
// height is the section sum (headline + content + actions). A
// non-zero Width or Height bypasses that band for that axis — the
// only clamp is the window-edge clamp in Layout. Used by the golden
// snapshot harness; full-window modal layout via Show() recomputes via
// its own path.
func (d *Dialog) Measure(available Size) Size {
	var boxW float32
	if d.Width > 0 {
		boxW = d.Width
	} else {
		boxW = dialogDefaultWidth
		if available.W > 0 && available.W < boxW {
			boxW = available.W
		}
		if boxW < dialogMinWidth {
			boxW = dialogMinWidth
		}
		if boxW > dialogMaxWidth {
			boxW = dialogMaxWidth
		}
	}
	boxH := d.Height
	if boxH <= 0 {
		boxH = d.boxHeight(boxW)
	}
	return Size{W: boxW, H: boxH}
}

// boxHeight sums the three sections for a content box of width
// boxW: headline = top-pad +
// headline-line-height + 0 bottom-pad; content = top-pad + body
// natural-height + (has-actions ? 8 : 24) bottom-pad; actions =
// 16 top-pad + 40 button + 24 bottom-pad. Sections without their
// optional element collapse to zero.
func (d *Dialog) boxHeight(boxW float32) float32 {
	var h float32
	if d.Title != "" {
		h += dialogHeadlinePadTop + dialogHeadlineLineH
	}
	if d.Content != nil {
		size := d.Content.Measure(Size{W: boxW - 2*dialogContentPadX, H: 1 << 20})
		bottom := dialogContentPadTop
		if len(d.Buttons) > 0 {
			bottom = dialogContentPadBottom
		}
		h += dialogContentPadTop + size.H + bottom
	}
	if len(d.Buttons) > 0 {
		h += dialogActionsPadTop + dialogActionsButtonH + dialogActionsPadBottom
	}
	return h
}

func (d *Dialog) Layout(rect Rect) {
	d.BaseWidget.Layout(rect)

	// Two layout modes:
	//   - Embedded (no window): rect IS the content box. Used by golden
	//     snapshots and any caller embedding the dialog inline.
	//   - Modal (after Show): rect is the full window. Compute box
	//     size from content + clamp to min/max, center within rect.
	var box Rect
	if d.window == nil {
		box = rect
	} else {
		size := d.Measure(Size{W: rect.W, H: rect.H})
		// Clamp against window dimensions (560 max, or 100% - 48).
		if maxByWin := rect.W - 48; size.W > maxByWin {
			size.W = maxByWin
		}
		if maxByWin := rect.H - 48; size.H > maxByWin {
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

	// Headline + content split: headline occupies rows 0..headlineH
	// from box top; content sits between headlineH and actionsTop.
	headlineH := float32(0)
	if d.Title != "" {
		headlineH = dialogHeadlinePadTop + dialogHeadlineLineH
	}
	actionsH := float32(0)
	if len(d.Buttons) > 0 {
		actionsH = dialogActionsPadTop + dialogActionsButtonH + dialogActionsPadBottom
	}
	if d.Content != nil {
		bottom := dialogContentPadTop
		if len(d.Buttons) > 0 {
			bottom = dialogContentPadBottom
		}
		d.Content.Layout(Rect{
			X: box.X + dialogContentPadX,
			Y: box.Y + headlineH + dialogContentPadTop,
			W: box.W - 2*dialogContentPadX,
			H: box.H - headlineH - dialogContentPadTop - bottom - actionsH,
		})
	}

	// Action button row — right-aligned, gap 8, natural button widths.
	if len(d.Buttons) > 0 {
		btnY := box.Y + box.H - dialogActionsPadBottom - dialogActionsButtonH
		btnRight := box.X + box.W - dialogActionsPadX
		for i := len(d.Buttons) - 1; i >= 0; i-- {
			btn := d.Buttons[i]
			size := btn.Measure(Size{W: box.W, H: dialogActionsButtonH})
			if size.H < dialogActionsButtonH {
				size.H = dialogActionsButtonH
			}
			btnRight -= size.W
			btn.Layout(Rect{X: btnRight, Y: btnY, W: size.W, H: size.H})
			btnRight -= dialogActionsGap
		}
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
	if d.ContainerColor.A > 0 {
		canvas.FillRoundedRect(box, d.ContainerRadius, d.ContainerColor)
	}

	if d.Title != "" {
		titleRect := Rect{
			X: box.X + dialogHeadlinePadX,
			Y: box.Y + dialogHeadlinePadTop,
			W: box.W - 2*dialogHeadlinePadX,
			H: dialogHeadlineLineH,
		}
		canvas.DrawText(d.Title, titleRect, d.TitleColor, ThemeFont(TextHeading))
	}

	if d.Content != nil {
		d.Content.Draw(canvas)
	}
	for _, btn := range d.Buttons {
		btn.Draw(canvas)
	}
}

func (d *Dialog) Handle(event Event) bool {
	// Esc cancels the dialog — convention on every platform.
	if ke, ok := event.(KeyEvent); ok && ke.Type() == EventKeyDown && ke.Key == KeyEscape {
		d.Close()
		return true
	}
	// Mouse clicks outside the content box are absorbed so the backdrop
	// blocks interaction with whatever's underneath — but we don't
	// auto-dismiss (clicking backdrop is not a confirm/cancel action;
	// user must use a button or Esc).
	if me, ok := event.(MouseEvent); ok && me.Type() == EventMouseDown {
		if !d.contentBounds.Contains(Point{X: me.X, Y: me.Y}) {
			return true
		}
	}
	return false
}

// HitTest: content box routes to children; outside routes to dialog
// itself so Handle can absorb clicks on the backdrop (modal).
func (d *Dialog) HitTest(p Point) Widget {
	if d.contentBounds.Contains(p) {
		for _, btn := range d.Buttons {
			if hit := btn.HitTest(p); hit != nil {
				return hit
			}
		}
		if d.Content != nil {
			if hit := d.Content.HitTest(p); hit != nil {
				return hit
			}
		}
	}
	// Absorb everything else — modal.
	return d
}
