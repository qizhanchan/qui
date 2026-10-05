package widgets

import . "github.com/qizhanchan/qui"

// FieldSet — a simple container variant that is basically a Container
// with opinionated styling + a little extra chrome.

// -------------------------------------------------------------------
// FieldSet

// FieldSet is a titled frame that groups related controls. The title
// appears in a strip along the top edge; padding leaves room for it
// so children lay out below. Any Layout engine works.
//
// The title is rendered through an internal Label that is exposed to
// framework tree walks (via ChildList / HitTest) but kept OUT of the
// content LayoutEngine — it is positioned manually in the title strip.
// That makes the title participate in the window's text selection and
// copy exactly like any other text, without the caller doing anything.
type FieldSet struct {
	Container
	title  *Label
	titleH float32
}

const groupBoxTitleH = 22

// NewFieldSet constructs a FieldSet with the given title, layout, and
// children. Children are wired to the outer (FieldSet) pointer via
// SetSelf so the dispatch parent chain is correct.
func NewFieldSet(title string, layout Layout, children ...Widget) *FieldSet {
	g := &FieldSet{}
	g.BaseWidget = NewBaseWidget()
	g.LayoutEngine = layout
	g.SetSelf(g)
	// Colors from theme at Draw; keep structural fields here.
	g.Style().BorderSize = 1
	g.Style().Font = Font{Size: 13, Bold: true}
	g.Style().Padding = Insets{Top: groupBoxTitleH + 8, Right: 10, Bottom: 10, Left: 10}

	// The title is a selectable Label living in the title strip. It is a
	// real tree node (parent = the FieldSet) so window attachment, hit
	// testing, dispatch, and cross-widget selection all reach it — but it
	// is not in Children, so the content LayoutEngine never moves it.
	g.title = NewLabel(title)
	g.title.Style().Font = g.Style().Font
	g.title.Paragraph.Wrap = false
	g.title.SetParent(g)

	for _, c := range children {
		g.AddChild(c)
	}
	return g
}

// SetTitleHeight sets the title strip's height (default 22) and the top
// padding that keeps content below it. The title's font is Style().Font.
func (g *FieldSet) SetTitleHeight(h float32) {
	if h <= 0 {
		h = groupBoxTitleH
	}
	g.Style().Padding.Top += h - g.titleStripH()
	g.titleH = h
	g.InvalidateLayout()
}

func (g *FieldSet) titleStripH() float32 {
	if g.titleH > 0 {
		return g.titleH
	}
	return groupBoxTitleH
}

// Title returns the current title text.
func (g *FieldSet) Title() string { return g.title.Text() }

// SetTitle replaces the title text.
func (g *FieldSet) SetTitle(title string) { g.title.SetText(title) }

// ChildList reports the title Label ahead of the content children, so
// framework tree walks (window attachment, selection collection in reading
// order) see the title first. The content LayoutEngine only ever operates
// on Children, so the title stays where FieldSet places it.
func (g *FieldSet) ChildList() []Widget {
	out := make([]Widget, 0, g.ChildCount()+1)
	out = append(out, g.title)
	return append(out, g.Container.ChildList()...)
}

// HitTest checks the content children first, then the title strip, then
// falls back to the FieldSet itself.
func (g *FieldSet) HitTest(p Point) Widget {
	if !g.Bounds().Contains(p) {
		return nil
	}
	for i := g.ChildCount() - 1; i >= 0; i-- {
		if hit := g.ChildAt(i).HitTest(p); hit != nil {
			return hit
		}
	}
	if hit := g.title.HitTest(p); hit != nil {
		return hit
	}
	if s := g.Self(); s != nil {
		return s
	}
	return g
}

// Layout lays out the content children via the engine, then positions the
// title Label in the top strip.
func (g *FieldSet) Layout(rect Rect) {
	g.Container.Layout(rect)
	g.layoutTitle()
}

func (g *FieldSet) layoutTitle() {
	b := g.Bounds()
	g.title.Layout(Rect{X: b.X + 10, Y: b.Y + 4, W: b.W - 20, H: g.titleStripH()})
}

func (g *FieldSet) Draw(canvas Canvas) {
	theme := CurrentTheme()
	b := g.Bounds()

	var clip Rect
	if ca, ok := canvas.(ClipAware); ok {
		clip = ca.ClipBoundsLogical()
	}
	drawSelf := clip.IsEmpty() || b.Intersects(clip)

	if drawSelf {
		canvas.FillRoundedRect(b, theme.RadiusMedium, theme.SurfaceRaised)
		if g.Style().BorderSize > 0 {
			canvas.StrokeRect(b, theme.Border, g.Style().BorderSize)
		}
		if g.title.Text() != "" || g.title.TextKey() != "" {
			// Keep the title's style in sync with the theme, then let the
			// Label paint (including any selection highlight).
			g.title.Style().Font = g.Style().Font
			g.title.Style().Foreground = theme.Text
			g.layoutTitle()
			g.title.Draw(canvas)
		}
	}
	for _, child := range g.Container.ChildList() {
		if !clip.IsEmpty() && !child.Bounds().Intersects(clip) {
			continue
		}
		child.Draw(canvas)
	}
}
