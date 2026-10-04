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

func (a *Anchor) HitTest(p Point) Widget {
	if a.Bounds().Contains(p) {
		return a
	}
	return nil
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
	e, ok := event.(MouseEvent)
	if !ok {
		return false
	}
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
