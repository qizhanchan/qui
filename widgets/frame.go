package widgets

import (
	"reflect"

	. "github.com/qizhanchan/qui"
)

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
	// titleFont is the font FieldSet last copied onto the title. The title
	// follows Style().Font only while it still holds that font, so a legend
	// font the app set itself is never overwritten.
	titleFont Font
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
	g.syncTitleFont()
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
	g.syncTitleFont()
	g.Container.Layout(rect)
	g.layoutTitle()
}

// syncTitleFont copies Style().Font onto the title unless the app has
// replaced the title's font with its own.
func (g *FieldSet) syncTitleFont() {
	st := g.title.Style()
	if !reflect.DeepEqual(st.Font, g.titleFont) && !reflect.DeepEqual(g.titleFont, Font{}) {
		return // the app set the legend's font
	}
	if f := g.Style().Font; !reflect.DeepEqual(st.Font, f) {
		st.Font = f
		g.title.InvalidateLayout()
	}
	g.titleFont = st.Font
}

func (g *FieldSet) layoutTitle() {
	b := g.Bounds()
	pad := g.Style().Padding
	g.title.Layout(Rect{X: b.X + pad.Left, Y: b.Y + 4, W: b.W - pad.Left - pad.Right, H: g.titleStripH()})
}

func (g *FieldSet) Draw(canvas Canvas) {
	theme := CurrentTheme()
	b := g.Bounds()
	st := g.Style()

	var clip Rect
	if ca, ok := canvas.(ClipAware); ok {
		clip = ca.ClipBoundsLogical()
	}
	// Both sides in the canvas's local space: the logical clip is mapped back
	// through any ancestor scroll translate, so a window-space rect would miss
	// it once the FieldSet sits in a scrolled ScrollView.
	drawSelf := clip.IsEmpty() || PaintBoundsOf(g).Intersects(clip)

	if drawSelf {
		bg := themed(st.Background)
		if bg.A == 0 {
			bg = theme.SurfaceRaised
		}
		border := themed(st.Border)
		if border.A == 0 {
			border = theme.Border
		}
		radius := st.Radius
		if radius <= 0 {
			radius = theme.RadiusMedium
		}
		canvas.FillRoundedRect(b, radius, bg)
		if st.BorderSize > 0 {
			canvas.StrokeRoundedRect(b, radius, border, st.BorderSize)
		}
		if (g.title.Text() != "" || g.title.TextKey() != "") && !boxChildHidden(g.title) {
			g.title.Draw(canvas)
		}
	}
	for _, child := range g.Container.ChildList() {
		if boxChildHidden(child) {
			continue
		}
		if !clip.IsEmpty() && !PaintBoundsOf(child).Intersects(clip) {
			continue
		}
		child.Draw(canvas)
	}
}
