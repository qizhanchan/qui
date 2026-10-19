package widgets

import . "github.com/qizhanchan/qui"

// Anchor is the hyperlink element — the qui equivalent of HTML's <a>.
//
// It renders link-styled text (link color, hand cursor on hover) and,
// on a press-release click, fires OnClick if set, otherwise opens Href
// via qui.OpenURL. Text rendering / measurement / layout are inherited
// from the embedded Label; Anchor owns the whole-widget click + cursor
// so the entire link region is clickable (not just a text span).
type Anchor struct {
	Label

	// Href is the navigation target. Opened with qui.OpenURL on click
	// when OnClick is nil.
	Href string
	// OnClick, when set, runs instead of opening Href — use it for
	// in-app navigation / routing.
	OnClick func()

	pressed bool
}

// DefaultLinkColor is the resting foreground for anchors. Callers can
// override via Anchor.Style().Foreground.
var DefaultLinkColor = Color{R: 0.10, G: 0.40, B: 0.90, A: 1}

// NewAnchor builds a link with the given text and href.
func NewAnchor(text, href string) *Anchor {
	a := &Anchor{Href: href}
	a.Label = *NewLabel(text)
	a.Selectable = false
	a.Style().Foreground = DefaultLinkColor
	a.SetSelf(a)
	return a
}

// Draw adds a focus underline under the label's box so keyboard focus is
// visible (Anchor is a Tab stop now).
func (a *Anchor) Draw(canvas Canvas) {
	a.Label.Draw(canvas)
	if a.focused {
		b := a.Bounds()
		canvas.FillRect(Rect{X: b.X, Y: b.Y + b.H - 2, W: b.W, H: 2}, CurrentTheme().Accent)
	}
}

// Focusable/TabStop make a link keyboard reachable (Space/Enter activates).
func (a *Anchor) Focusable() bool { return a.Enabled() }
func (a *Anchor) TabStop() bool   { return true }

func (a *Anchor) CancelInteraction() {
	if a.pressed {
		a.pressed = false
		a.Invalidate()
	}
}

// WidgetCursor puts a hand over the whole link. A disabled anchor declines
// so the shape falls back to whatever encloses it (see qui/cursor.go).
func (a *Anchor) WidgetCursor() (CursorShape, bool) {
	return CursorHand, a.Enabled()
}

func (a *Anchor) Handle(event Event) bool {
	if !a.Enabled() {
		return false
	}
	switch e := event.(type) {
	case MouseEvent:
		switch e.Type() {
		case EventMouseLeave:
			a.pressed = false
		case EventMouseDown:
			if e.Button == MouseButtonLeft && a.Bounds().Contains(Point{X: e.X, Y: e.Y}) {
				a.pressed = true
				return true
			}
		case EventMouseUp:
			if a.pressed && e.Button == MouseButtonLeft {
				a.pressed = false
				if a.Bounds().Contains(Point{X: e.X, Y: e.Y}) {
					a.navigate()
					return true
				}
			}
		}
	case KeyEvent:
		if a.focused && e.Type() == EventKeyDown && !e.Repeat &&
			(e.Key == KeyEnter || e.Key == KeySpace) {
			a.navigate()
			return true
		}
	}
	return false
}

func (a *Anchor) navigate() {
	if a.OnClick != nil {
		a.OnClick()
		return
	}
	if a.Href != "" {
		_ = OpenURL(a.Href)
	}
}
