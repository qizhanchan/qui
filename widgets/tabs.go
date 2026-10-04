package widgets

import . "github.com/qizhanchan/qui"

// TabView is a tab strip + content area. Only the selected tab's
// content is drawn and laid out; focus/tick/hit-test traversal skips
// hidden tabs so Tab navigation can't reach invisible widgets.
//
// Layout convention:
//   - Top `tabStripH` pixels = tab row (split evenly per tab)
//   - Remaining area = content for the selected tab
//
// Design note: tab headers are rendered inline (no separate Widget)
// because their hit test and drawing are tightly coupled to the
// strip layout — wrapping each in a Button-subwidget would complicate
// focus collection (hidden tabs' headers would otherwise be Tab-able).
type TabView struct {
	BaseWidget
	Tabs        []Tab
	SelectedIdx int
	OnSelect    func(idx int)

	// Color knobs. NewTabView fills them for a plain HTML look; set
	// them (e.g. from CurrentTheme) for a themed strip. The html-css
	// layer styles tabs with plain CSS.
	StripColor      Color // strip + content area background
	IndicatorColor  Color // painted under the selected tab label
	SelectedColor   Color // selected tab label color
	InactiveColor   Color // inactive tab label color
	StateLayerColor Color // hover overlay on inactive tabs; A=0 disables

	hoverIdx int
	focused  bool
}

// Tab is a single entry in a TabView.
type Tab struct {
	Title   string
	Content Widget
}

// tabStripH is the tab strip height: 48 px.
const tabStripH = 48

// NewTabView constructs a TabView. The first tab is selected by default.
// Tab contents are wired back as children via SetParent so event
// dispatch walks into them correctly.
func NewTabView(tabs ...Tab) *TabView {
	t := &TabView{
		BaseWidget:     NewBaseWidget(),
		Tabs:           tabs,
		SelectedIdx:    0,
		hoverIdx:       -1,
		StripColor:     Color{R: 1, G: 1, B: 1, A: 1},
		IndicatorColor: Color{R: 0.30, G: 0.55, B: 0.85, A: 1},
		SelectedColor:  Color{R: 0.30, G: 0.55, B: 0.85, A: 1},
		InactiveColor:  Color{R: 0.35, G: 0.35, B: 0.35, A: 1},
	}
	t.Style().Font = Font{Size: 14}
	for i := range tabs {
		if tabs[i].Content != nil {
			tabs[i].Content.SetParent(t)
		}
	}
	return t
}

func (t *TabView) Focusable() bool { return t.Enabled() }
func (t *TabView) SetFocused(f bool) {
	if t.focused == f {
		return
	}
	t.focused = f
	t.Invalidate()
}

// SelectedContent returns the currently visible content widget, or nil.
func (t *TabView) SelectedContent() Widget {
	if t.SelectedIdx < 0 || t.SelectedIdx >= len(t.Tabs) {
		return nil
	}
	return t.Tabs[t.SelectedIdx].Content
}

// ChildList returns ONLY the selected content so focus collection and
// broadcast traversal skip widgets in hidden tabs.
func (t *TabView) ChildList() []Widget {
	if c := t.SelectedContent(); c != nil {
		return []Widget{c}
	}
	return nil
}

func (t *TabView) Measure(available Size) Size { return available }

func (t *TabView) Layout(rect Rect) {
	t.BaseWidget.Layout(rect)
	contentRect := Rect{X: rect.X, Y: rect.Y + tabStripH, W: rect.W, H: rect.H - tabStripH}
	if c := t.SelectedContent(); c != nil {
		c.Layout(contentRect)
	}
}

// tabWidth returns the per-tab width given the current tab count.
func (t *TabView) tabWidth() float32 {
	n := len(t.Tabs)
	if n == 0 {
		return 0
	}
	return t.Bounds().W / float32(n)
}

func (t *TabView) Draw(canvas Canvas) {
	theme := CurrentTheme()
	b := t.Bounds()
	tw := t.tabWidth()
	labelFont := ThemeFont(TextLabel) // tab labels read as controls, not body

	if t.StripColor.A > 0 {
		canvas.FillRect(Rect{X: b.X, Y: b.Y, W: b.W, H: tabStripH}, t.StripColor)
	}

	const indicatorHeight float32 = 3 // active-indicator height
	for i, tab := range t.Tabs {
		tabRect := Rect{X: b.X + float32(i)*tw, Y: b.Y, W: tw, H: tabStripH}
		if i == t.hoverIdx && i != t.SelectedIdx && t.StateLayerColor.A > 0 {
			DrawStateLayer(canvas, tabRect, 0, t.StateLayerColor, theme.HoverOpacity)
		}
		labelColor := t.InactiveColor
		if i == t.SelectedIdx {
			labelColor = t.SelectedColor
		}
		// Label horizontally + vertically centered in the FULL 48-px
		// container, not in the 45 px above the indicator. The web
		// reference sits its .content at 48 px with the indicator
		// absolutely positioned at its bottom; the label uses the
		// full height for vertical centering and the indicator paints
		// behind the descender area. Without this the label sits ~3 px
		// lower than the web reference.
		labelW, _ := TextMetrics(tab.Title, labelFont)
		canvas.DrawText(tab.Title,
			Rect{
				X: tabRect.X + (tabRect.W-labelW)/2,
				Y: tabRect.Y,
				W: labelW,
				H: tabStripH,
			},
			labelColor, labelFont)

		// Active indicator — only drawn under the selected tab.
		// The indicator sits INSIDE the label's own box, so its width
		// matches the label text width, not the full tab cell. (A
		// full-cell indicator would be the `fullWidthIndicator=true`
		// branch; not implemented here yet.) Fully rounded at 3 px
		// height → effectively
		// a horizontal pill cap (the bottom corners sit on the strip
		// divider line so the rendered visual is "top half of a pill").
		if i == t.SelectedIdx {
			canvas.FillRoundedRect(Rect{
				X: tabRect.X + (tabRect.W-labelW)/2,
				Y: b.Y + tabStripH - indicatorHeight,
				W: labelW,
				H: indicatorHeight,
			}, indicatorHeight/2, t.IndicatorColor)
		}
	}

	// Content area background matches the strip.
	if t.StripColor.A > 0 {
		contentArea := Rect{X: b.X, Y: b.Y + tabStripH, W: b.W, H: b.H - tabStripH}
		canvas.FillRect(contentArea, t.StripColor)
	}
	if c := t.SelectedContent(); c != nil {
		c.Draw(canvas)
	}
}

func (t *TabView) Handle(event Event) bool {
	if !t.Enabled() {
		return false
	}
	switch e := event.(type) {
	case MouseEvent:
		switch e.Type() {
		case EventMouseMove:
			if t.pointOnStrip(Point{X: e.X, Y: e.Y}) {
				if idx := t.tabIndexAt(e.X); idx != t.hoverIdx {
					t.hoverIdx = idx
					t.Invalidate()
				}
			} else {
				if t.hoverIdx != -1 {
					t.hoverIdx = -1
					t.Invalidate()
				}
			}
		case EventMouseLeave:
			if t.hoverIdx != -1 {
				t.hoverIdx = -1
				t.Invalidate()
			}
		case EventMouseDown:
			if e.Button != MouseButtonLeft {
				return false
			}
			if t.pointOnStrip(Point{X: e.X, Y: e.Y}) {
				idx := t.tabIndexAt(e.X)
				if idx >= 0 {
					t.Select(idx)
					return true
				}
			}
		}
	case KeyEvent:
		if t.focused && e.Type() == EventKeyDown {
			switch e.Key {
			case KeyLeft:
				if t.SelectedIdx > 0 {
					t.Select(t.SelectedIdx - 1)
				}
				return true
			case KeyRight:
				if t.SelectedIdx < len(t.Tabs)-1 {
					t.Select(t.SelectedIdx + 1)
				}
				return true
			}
		}
	}
	return false
}

// Select changes the active tab and fires OnSelect. No-op if idx is
// out of range or already selected.
func (t *TabView) Select(idx int) {
	if idx < 0 || idx >= len(t.Tabs) || idx == t.SelectedIdx {
		return
	}
	old := t.SelectedContent()
	t.SelectedIdx = idx
	if old != nil {
		AttachWindowTree(old, nil)
	}
	// Re-layout the new content immediately so Bounds is correct
	// before the next frame's dispatch / draw.
	if c := t.SelectedContent(); c != nil {
		b := t.Bounds()
		AttachWindowTree(c, t.Window())
		c.Layout(Rect{X: b.X, Y: b.Y + tabStripH, W: b.W, H: b.H - tabStripH})
	}
	t.InvalidateLayout()
	if t.OnSelect != nil {
		t.OnSelect(idx)
	}
}

func (t *TabView) HitTest(p Point) Widget {
	if !t.Bounds().Contains(p) {
		return nil
	}
	// Tab strip always routes to self.
	if t.pointOnStrip(p) {
		return t
	}
	// Content area routes to the visible child.
	if c := t.SelectedContent(); c != nil {
		if hit := c.HitTest(p); hit != nil {
			return hit
		}
	}
	return t
}

func (t *TabView) pointOnStrip(p Point) bool {
	b := t.Bounds()
	return p.X >= b.X && p.X < b.X+b.W && p.Y >= b.Y && p.Y < b.Y+tabStripH
}

// tabIndexAt returns the tab index at x (within the strip), or -1.
func (t *TabView) tabIndexAt(x float32) int {
	b := t.Bounds()
	tw := t.tabWidth()
	if tw <= 0 {
		return -1
	}
	idx := int((x - b.X) / tw)
	if idx < 0 || idx >= len(t.Tabs) {
		return -1
	}
	return idx
}
