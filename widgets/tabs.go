package widgets

import . "github.com/qizhanchan/qui"

// TabView is a tab strip + content area. Only the selected tab's
// content is drawn and laid out; focus/tick/hit-test traversal skips
// hidden tabs so Tab navigation can't reach invisible widgets.
//
// Layout convention:
//   - Top StripHeight pixels = tab row
//   - Remaining area = content for the selected tab
//
// Tabs share the strip equally by default. With FitTabs each tab is as
// wide as its caption (icon, badge and close button included); when they
// don't fit, the strip scrolls horizontally (wheel / trackpad over it, and
// selecting a tab scrolls it into view).
//
// Design note: tab headers are rendered inline (no separate Widget)
// because their hit test and drawing are tightly coupled to the
// strip layout — wrapping each in a Button-subwidget would complicate
// focus collection (hidden tabs' headers would otherwise be Tab-able).
// They are published to the AX tree as role=tab children instead.
type TabView struct {
	BaseWidget
	Tabs        []Tab
	SelectedIdx int
	OnSelect    func(idx int)
	// CanSelect may veto a switch (unsaved form on the current tab) by
	// returning false. It is not consulted by programmatic SetSelected.
	CanSelect func(from, to int) bool
	// OnCloseTab fires when a Closable tab's × is clicked (or Cmd/Ctrl+W
	// pressed with the strip focused). The app decides: call RemoveTab to
	// close it, or do nothing to keep it.
	OnCloseTab func(idx int)

	// StripHeight is the tab row height (0 = 48).
	StripHeight float32
	// FitTabs sizes each tab to its content instead of sharing the strip
	// equally; MinTabWidth / MaxTabWidth bound it (0 = 64 / unbounded).
	FitTabs     bool
	MinTabWidth float32
	MaxTabWidth float32

	// Color knobs. NewTabView fills them for a plain HTML look; set
	// them (e.g. from CurrentTheme) for a themed strip. Zero fields fall
	// back to the theme. The html-css layer styles tabs with plain CSS.
	StripColor      Color // strip + content area background
	IndicatorColor  Color // painted under the selected tab label
	SelectedColor   Color // selected tab label color
	InactiveColor   Color // inactive tab label color
	StateLayerColor Color // hover overlay on inactive tabs; A=0 disables
	BadgeColor      Color // badge pill fill

	hoverIdx    int
	hoverClose  int
	focused     bool
	stripScroll float32
}

// Tab is a single entry in a TabView.
type Tab struct {
	Title string
	// TitleKey makes the caption come from the message catalog (Title is
	// the fallback).
	TitleKey string
	// Icon is drawn before the caption, tinted like it.
	Icon VectorSource
	// Badge is a short count or marker shown after the caption ("3",
	// "•"); empty for none.
	Badge string
	// Closable shows a × that fires TabView.OnCloseTab.
	Closable bool
	// Disabled tabs can't be selected and are dimmed.
	Disabled bool
	// ID is published as the tab's AX id (`#id` selectors).
	ID      string
	Content Widget
}

// DisplayTitle is the caption: TitleKey resolved, or Title.
func (t Tab) DisplayTitle() string {
	if t.TitleKey == "" {
		return t.Title
	}
	return TranslateOr("", t.TitleKey, t.Title, nil)
}

// tabStripH is the default tab strip height: 48 px.
const tabStripH = 48

const (
	tabPadX      = 16
	tabIconSize  = 18
	tabIconGap   = 8
	tabCloseSize = 16
	tabBadgeGap  = 6
)

// NewTabView constructs a TabView. The first tab is selected by default.
// Tab contents are wired back as children via SetParent so event
// dispatch walks into them correctly.
func NewTabView(tabs ...Tab) *TabView {
	t := &TabView{
		BaseWidget:     NewBaseWidget(),
		Tabs:           tabs,
		SelectedIdx:    0,
		hoverIdx:       -1,
		hoverClose:     -1,
		StripColor:     Color{R: 1, G: 1, B: 1, A: 1},
		IndicatorColor: Color{R: 0.30, G: 0.55, B: 0.85, A: 1},
		SelectedColor:  Color{R: 0.30, G: 0.55, B: 0.85, A: 1},
		InactiveColor:  Color{R: 0.35, G: 0.35, B: 0.35, A: 1},
	}
	t.SetSelf(t)
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

// stripH is the resolved strip height.
func (t *TabView) stripH() float32 {
	if t.StripHeight > 0 {
		return t.StripHeight
	}
	return tabStripH
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

// AddTab appends a tab and returns its index.
func (t *TabView) AddTab(tab Tab) int {
	if tab.Content != nil {
		AdoptWidgetTree(tab.Content, t.selfOrT(), nil)
	}
	t.Tabs = append(t.Tabs, tab)
	if len(t.Tabs) == 1 {
		t.SelectedIdx = 0
		if tab.Content != nil {
			AttachWindowTree(tab.Content, t.Window())
		}
	}
	t.InvalidateLayout()
	return len(t.Tabs) - 1
}

// RemoveTab removes tab idx. When it was selected, the next tab (or the
// previous one at the end) becomes selected and OnSelect fires.
func (t *TabView) RemoveTab(idx int) {
	if idx < 0 || idx >= len(t.Tabs) {
		return
	}
	wasSelected := idx == t.SelectedIdx
	if c := t.Tabs[idx].Content; c != nil {
		DetachWidgetTree(c)
	}
	t.Tabs = append(t.Tabs[:idx:idx], t.Tabs[idx+1:]...)
	switch {
	case len(t.Tabs) == 0:
		t.SelectedIdx = -1
	case idx < t.SelectedIdx:
		t.SelectedIdx--
	case wasSelected:
		next := minInt(idx, len(t.Tabs)-1)
		t.SelectedIdx = -1
		t.setSelected(next)
	}
	t.hoverIdx, t.hoverClose = -1, -1
	t.clampStripScroll()
	t.InvalidateLayout()
}

func (t *TabView) selfOrT() Widget {
	if s := t.Self(); s != nil {
		return s
	}
	return t
}

func (t *TabView) Measure(available Size) Size { return available }

func (t *TabView) Layout(rect Rect) {
	t.BaseWidget.Layout(rect)
	h := t.stripH()
	contentRect := Rect{X: rect.X, Y: rect.Y + h, W: rect.W, H: rect.H - h}
	if c := t.SelectedContent(); c != nil {
		c.Layout(contentRect)
	}
	t.clampStripScroll()
}

func (t *TabView) labelFont() Font { return ThemeFont(TextLabel) }

// naturalTabWidth is a tab's content width (FitTabs).
func (t *TabView) naturalTabWidth(tab Tab) float32 {
	w, _ := TextMetrics(tab.DisplayTitle(), t.labelFont())
	w += 2 * tabPadX
	if tab.Icon != nil {
		w += tabIconSize + tabIconGap
	}
	if tab.Badge != "" {
		bw, _ := TextMetrics(tab.Badge, ThemeFont(TextLabelSmall))
		w += tabBadgeGap + max(bw+10, 18)
	}
	if tab.Closable {
		w += tabCloseSize + 4
	}
	minW := t.MinTabWidth
	if minW <= 0 {
		minW = 64
	}
	w = max(w, minW)
	if t.MaxTabWidth > 0 {
		w = min(w, t.MaxTabWidth)
	}
	return w
}

// tabRects returns every tab's header rect in window coordinates, after
// strip scrolling.
func (t *TabView) tabRects() []Rect {
	b := t.Bounds()
	n := len(t.Tabs)
	out := make([]Rect, n)
	if n == 0 {
		return out
	}
	h := t.stripH()
	if !t.FitTabs {
		tw := b.W / float32(n)
		for i := range out {
			out[i] = Rect{X: b.X + float32(i)*tw, Y: b.Y, W: tw, H: h}
		}
		return out
	}
	x := b.X - t.stripScroll
	for i, tab := range t.Tabs {
		w := t.naturalTabWidth(tab)
		out[i] = Rect{X: x, Y: b.Y, W: w, H: h}
		x += w
	}
	return out
}

// stripContentWidth is the total width of all tabs (FitTabs).
func (t *TabView) stripContentWidth() float32 {
	if !t.FitTabs {
		return t.Bounds().W
	}
	var w float32
	for _, tab := range t.Tabs {
		w += t.naturalTabWidth(tab)
	}
	return w
}

func (t *TabView) clampStripScroll() {
	t.stripScroll = ClampScroll(t.stripScroll, t.stripContentWidth(), t.Bounds().W)
}

// scrollTabIntoView adjusts the strip scroll so tab idx is fully visible.
func (t *TabView) scrollTabIntoView(idx int) {
	if !t.FitTabs || idx < 0 || idx >= len(t.Tabs) {
		return
	}
	rects := t.tabRects()
	b := t.Bounds()
	r := rects[idx]
	if r.X < b.X {
		t.stripScroll -= b.X - r.X
	} else if r.X+r.W > b.X+b.W {
		t.stripScroll += r.X + r.W - (b.X + b.W)
	}
	t.clampStripScroll()
}

// closeRect is the × hit area inside a tab rect.
func closeRect(tabRect Rect) Rect {
	return Rect{
		X: tabRect.X + tabRect.W - tabPadX/2 - tabCloseSize,
		Y: tabRect.Y + (tabRect.H-tabCloseSize)/2,
		W: tabCloseSize, H: tabCloseSize,
	}
}

func (t *TabView) colors() (strip, indicator, selected, inactive, badge Color) {
	th := CurrentTheme()
	strip, indicator, selected, inactive, badge = th.Surface, th.Accent, th.Accent, th.TextMuted, th.Accent
	pick(&strip, t.StripColor)
	pick(&indicator, t.IndicatorColor)
	pick(&selected, t.SelectedColor)
	pick(&inactive, t.InactiveColor)
	pick(&badge, t.BadgeColor)
	return
}

func (t *TabView) Draw(canvas Canvas) {
	theme := CurrentTheme()
	b := t.Bounds()
	h := t.stripH()
	labelFont := t.labelFont() // tab labels read as controls, not body
	stripColor, indicatorColor, selectedColor, inactiveColor, badgeColor := t.colors()
	strip := Rect{X: b.X, Y: b.Y, W: b.W, H: h}

	if stripColor.A > 0 {
		canvas.FillRect(strip, stripColor)
	}

	const indicatorHeight float32 = 3 // active-indicator height
	rects := t.tabRects()
	clip := canvas.Save()
	canvas.ClipRect(strip)
	for i, tab := range t.Tabs {
		tabRect := rects[i]
		if !tabRect.Intersects(strip) {
			continue
		}
		if i == t.hoverIdx && i != t.SelectedIdx && !tab.Disabled && t.StateLayerColor.A > 0 {
			DrawStateLayer(canvas, tabRect, 0, t.StateLayerColor, theme.HoverOpacity)
		}
		labelColor := inactiveColor
		if i == t.SelectedIdx {
			labelColor = selectedColor
		}
		if tab.Disabled {
			labelColor = LerpColor(stripColor, labelColor, 0.38)
		}
		title := tab.DisplayTitle()
		labelW, _ := TextMetrics(title, labelFont)

		// Lay the header out left to right: icon, caption, badge, ×.
		// Equal-width tabs center that run; fit tabs pad it.
		runW := labelW
		if tab.Icon != nil {
			runW += tabIconSize + tabIconGap
		}
		var badgeW float32
		if tab.Badge != "" {
			bw, _ := TextMetrics(tab.Badge, ThemeFont(TextLabelSmall))
			badgeW = max(bw+10, 18)
			runW += tabBadgeGap + badgeW
		}
		avail := tabRect.W
		if tab.Closable {
			avail -= tabCloseSize + tabPadX/2
		}
		x := tabRect.X + (avail-runW)/2
		if t.FitTabs {
			x = tabRect.X + tabPadX
		}
		if tab.Icon != nil {
			canvas.DrawVector(tab.Icon, Rect{X: x, Y: tabRect.Y + (h-tabIconSize)/2, W: tabIconSize, H: tabIconSize}, labelColor)
			x += tabIconSize + tabIconGap
		}
		// Label vertically centered in the FULL strip height, not in the
		// part above the indicator: the indicator paints behind the
		// descender area, as in the web reference.
		labelX := x
		canvas.DrawText(title, Rect{X: x, Y: tabRect.Y, W: labelW, H: h}, labelColor, labelFont)
		x += labelW
		if tab.Badge != "" {
			x += tabBadgeGap
			pill := Rect{X: x, Y: tabRect.Y + (h-18)/2, W: badgeW, H: 18}
			canvas.FillRoundedRect(pill, 9, badgeColor)
			sf := ThemeFont(TextLabelSmall)
			bw, _ := TextMetrics(tab.Badge, sf)
			canvas.DrawText(tab.Badge, Rect{X: pill.X + (pill.W-bw)/2, Y: pill.Y, W: bw, H: pill.H}, theme.AccentText, sf)
		}
		if tab.Closable {
			cr := closeRect(tabRect)
			if i == t.hoverClose {
				DrawStateLayer(canvas, cr, cr.W/2, labelColor, theme.PressedOpacity)
			}
			const inset = 4.5
			canvas.DrawLine(Point{X: cr.X + inset, Y: cr.Y + inset}, Point{X: cr.X + cr.W - inset, Y: cr.Y + cr.H - inset}, labelColor, 1.5)
			canvas.DrawLine(Point{X: cr.X + cr.W - inset, Y: cr.Y + inset}, Point{X: cr.X + inset, Y: cr.Y + cr.H - inset}, labelColor, 1.5)
		}

		// Active indicator — only under the selected tab, as wide as its
		// caption, a pill cap sitting on the strip's bottom edge.
		if i == t.SelectedIdx {
			canvas.FillRoundedRect(Rect{
				X: labelX,
				Y: b.Y + h - indicatorHeight,
				W: labelW,
				H: indicatorHeight,
			}, indicatorHeight/2, indicatorColor)
		}
	}
	canvas.RestoreTo(clip)

	// Content area background matches the strip.
	if stripColor.A > 0 {
		contentArea := Rect{X: b.X, Y: b.Y + h, W: b.W, H: b.H - h}
		canvas.FillRect(contentArea, stripColor)
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
		p := Point{X: e.X, Y: e.Y}
		switch e.Type() {
		case EventMouseMove:
			idx, onClose := -1, -1
			if t.pointOnStrip(p) {
				idx = t.tabIndexAt(p)
				if idx >= 0 && t.Tabs[idx].Closable && closeRect(t.tabRects()[idx]).Contains(p) {
					onClose = idx
				}
			}
			if idx != t.hoverIdx || onClose != t.hoverClose {
				t.hoverIdx, t.hoverClose = idx, onClose
				t.Invalidate()
			}
		case EventMouseLeave:
			if t.hoverIdx != -1 || t.hoverClose != -1 {
				t.hoverIdx, t.hoverClose = -1, -1
				t.Invalidate()
			}
		case EventScroll:
			if t.FitTabs && t.pointOnStrip(p) && t.stripContentWidth() > t.Bounds().W {
				d := e.DeltaX
				if d == 0 {
					d = e.DeltaY
				}
				t.stripScroll -= d * 40
				t.clampStripScroll()
				t.Invalidate()
				return true
			}
		case EventMouseDown:
			if e.Button != MouseButtonLeft || !t.pointOnStrip(p) {
				return false
			}
			idx := t.tabIndexAt(p)
			if idx < 0 {
				return false
			}
			if t.Tabs[idx].Closable && closeRect(t.tabRects()[idx]).Contains(p) {
				if t.OnCloseTab != nil {
					t.OnCloseTab(idx)
				}
				return true
			}
			t.Select(idx)
			return true
		}
	case KeyEvent:
		if t.focused && e.Type() == EventKeyDown {
			switch e.Key {
			case KeyLeft:
				if i := t.nextEnabled(t.SelectedIdx-1, -1); i >= 0 {
					t.Select(i)
				}
				return true
			case KeyRight:
				if i := t.nextEnabled(t.SelectedIdx+1, +1); i >= 0 {
					t.Select(i)
				}
				return true
			case KeyHome:
				if i := t.nextEnabled(0, +1); i >= 0 {
					t.Select(i)
				}
				return true
			case KeyEnd:
				if i := t.nextEnabled(len(t.Tabs)-1, -1); i >= 0 {
					t.Select(i)
				}
				return true
			case KeyW:
				if IsCommandMod(e.Mods) && t.SelectedIdx >= 0 && t.Tabs[t.SelectedIdx].Closable && t.OnCloseTab != nil {
					t.OnCloseTab(t.SelectedIdx)
					return true
				}
			}
		}
	}
	return false
}

// nextEnabled walks from i in direction step to the first enabled tab.
func (t *TabView) nextEnabled(i, step int) int {
	for ; i >= 0 && i < len(t.Tabs); i += step {
		if !t.Tabs[i].Disabled {
			return i
		}
	}
	return -1
}

// Select changes the active tab as a user action: disabled tabs are
// refused, CanSelect may veto, and OnSelect fires. No-op if idx is out of
// range or already selected.
func (t *TabView) Select(idx int) {
	if idx < 0 || idx >= len(t.Tabs) || idx == t.SelectedIdx || t.Tabs[idx].Disabled {
		return
	}
	if t.CanSelect != nil && !t.CanSelect(t.SelectedIdx, idx) {
		return
	}
	t.setSelected(idx)
}

// SetSelected changes the active tab programmatically (no veto, but
// OnSelect still fires).
func (t *TabView) SetSelected(idx int) {
	if idx < 0 || idx >= len(t.Tabs) || idx == t.SelectedIdx {
		return
	}
	t.setSelected(idx)
}

func (t *TabView) setSelected(idx int) {
	old := t.SelectedContent()
	t.SelectedIdx = idx
	if old != nil {
		AttachWindowTree(old, nil)
	}
	// Re-layout the new content immediately so Bounds is correct
	// before the next frame's dispatch / draw.
	if c := t.SelectedContent(); c != nil {
		b := t.Bounds()
		h := t.stripH()
		AttachWindowTree(c, t.Window())
		c.Layout(Rect{X: b.X, Y: b.Y + h, W: b.W, H: b.H - h})
	}
	t.scrollTabIntoView(idx)
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
	return p.X >= b.X && p.X < b.X+b.W && p.Y >= b.Y && p.Y < b.Y+t.stripH()
}

// tabIndexAt returns the tab index at p (within the strip), or -1.
func (t *TabView) tabIndexAt(p Point) int {
	for i, r := range t.tabRects() {
		if p.X >= r.X && p.X < r.X+r.W {
			return i
		}
	}
	return -1
}

// AccessibleChildren publishes each tab header as role=tab (selected /
// disabled state, the close button as its own button), so an agent can
// `click '[role=tab][name="Settings"]'`.
func (t *TabView) AccessibleChildren() []AXChild {
	rects := t.tabRects()
	strip := Rect{X: t.Bounds().X, Y: t.Bounds().Y, W: t.Bounds().W, H: t.stripH()}
	out := make([]AXChild, 0, len(t.Tabs))
	for i, tab := range t.Tabs {
		r := rects[i].Intersect(strip)
		if r.IsEmpty() {
			continue
		}
		var st AccessibleState
		if i == t.SelectedIdx {
			st |= AXStateSelected
		}
		if tab.Disabled {
			st |= AXStateDisabled
		}
		out = append(out, AXChild{ID: tab.ID, Role: RoleTab, Name: tab.DisplayTitle(), Value: tab.Badge, State: st, Bounds: r})
		if tab.Closable {
			out = append(out, AXChild{Role: RoleButton, Name: TOr("qui.tab.close", "Close") + " " + tab.DisplayTitle(), Bounds: closeRect(rects[i])})
		}
	}
	return out
}
