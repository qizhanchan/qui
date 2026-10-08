package html

import (
	"github.com/qizhanchan/qui"
	"github.com/qizhanchan/qui/htmlcss"
	"github.com/qizhanchan/qui/reactive"
)

// ToolbarGroup is one run of related toolbar controls (undo/redo, the font
// controls, …). A toolbar that runs out of width moves whole trailing
// groups — never single buttons — into its "⋮ More" panel, the way the
// Google Docs / Sheets / Slides toolbars collapse.
type ToolbarGroup struct {
	// Key identifies the group (stable across renders, unique within the
	// toolbar).
	Key string
	// Items are the group's controls, rendered in order.
	Items []Node
	// Joined drops the divider in front of the group, for a group that
	// reads as a continuation of the previous one (font size after the
	// font family). It still overflows on its own.
	Joined bool
}

// ToolGroup builds a ToolbarGroup.
func ToolGroup(key string, items ...Node) ToolbarGroup {
	return ToolbarGroup{Key: key, Items: items}
}

// NoDivider returns the group with its leading divider removed (Joined).
func (g ToolbarGroup) NoDivider() ToolbarGroup { g.Joined = true; return g }

// ToolbarOptions configures Toolbar.
type ToolbarOptions struct {
	// Key keeps the toolbar component stable among dynamic siblings.
	Key string
	// Class is added to the toolbar row (next to q-toolbar).
	Class string
	// DividerClass styles the divider in front of each group (default
	// "q-toolbar-divider").
	DividerClass string
	// MoreClass is added to the More button (next to q-toolbar-more) —
	// usually the app's toolbar-button class, so it matches its siblings.
	MoreClass string
	// MoreIcon is the More button's icon; nil shows "⋮".
	MoreIcon qui.VectorSource
	// MoreTitle is the More button's tooltip and accessible name (default
	// the "qui.more" message, "More").
	MoreTitle string
	// PanelClass is added to the overflow panel (next to q-toolbar-overflow).
	PanelClass string
	// OnPanelClose fires when the overflow panel closes (dismissed, toggled
	// off, or no longer needed because everything fits) — the place to put
	// focus back into the document.
	OnPanelClose func()
}

type toolbarProps struct {
	opts   ToolbarOptions
	groups []ToolbarGroup
}

// Toolbar lays groups out in one row and, when they don't all fit, keeps
// the leading groups that do and moves the rest behind a trailing "⋮ More"
// button whose popover panel shows them. The fit is recomputed on every
// layout of the row — a window resize included — from measured widths, so
// it needs no breakpoints:
//
//	h.Toolbar(h.ToolbarOptions{Class: "toolbar", MoreClass: "tbtn"},
//		h.ToolGroup("history", undoBtn, redoBtn),
//		h.ToolGroup("font", fontSelect),
//		h.ToolGroup("size", sizeSelect).NoDivider(),
//	)
//
// Overflowed groups stay mounted (hidden: visibility:hidden, out of flow)
// so their widths remain measurable; the panel renders a second copy of
// their items. Item builders must therefore not share per-instance state —
// a dropdown trigger anchors its menu with OnClickEl, not a shared UseRef —
// and scripts and tests should address controls with :visible or a
// :layer(...) selector.
func Toolbar(opts ToolbarOptions, groups ...ToolbarGroup) Node {
	return Component("h.Toolbar", opts.Key, toolbarProps{opts: opts, groups: groups}, renderToolbar)
}

// toolbarHiddenStyle takes an overflowed group out of flow without losing
// its measurement: visibility:hidden keeps it out of paint, hit testing,
// focus and the accessibility tree, and position:absolute at the row's
// origin keeps it from pushing anything or tripping overflow diagnostics.
const toolbarHiddenStyle = "position:absolute;left:0;top:0;visibility:hidden"

func renderToolbar(p toolbarProps) Node {
	opts, groups := p.opts, p.groups
	n := len(groups)
	fit, setFit := reactive.UseState(-1) // -1: everything fits
	open, setOpen := reactive.UseState(false)
	moreBtn := reactive.UseRef[*htmlcss.El](nil)
	moreWrap := reactive.UseRef[*htmlcss.El](nil)
	groupEls := reactive.UseRef[map[string]*htmlcss.El](nil)
	if *groupEls == nil {
		*groupEls = map[string]*htmlcss.El{}
	}
	rt := reactive.CurrentRuntime()

	k := n
	if fit >= 0 && fit < n {
		k = fit
	}
	dividerClass := opts.DividerClass
	if dividerClass == "" {
		dividerClass = "q-toolbar-divider"
	}
	title := opts.MoreTitle
	if title == "" {
		title = qui.TOr("qui.more", "More")
	}
	closePanel := func() {
		setOpen(false)
		if opts.OnPanelClose != nil {
			opts.OnPanelClose()
		}
	}
	divider := func() Node { return Span().Class(dividerClass) }

	kids := make([]any, 0, n+1)
	for i, g := range groups {
		key := g.Key
		items := make([]any, 0, len(g.Items)+1)
		if i > 0 && !g.Joined {
			items = append(items, divider())
		}
		for _, it := range g.Items {
			items = append(items, it)
		}
		els := *groupEls
		grp := Div(items...).Key("g:" + key).Class("q-toolbar-group").Ref(func(el *htmlcss.El) {
			if el == nil {
				delete(els, key)
				return
			}
			els[key] = el
		})
		if i >= k {
			grp.Attr("style", toolbarHiddenStyle)
		}
		kids = append(kids, grp)
	}

	var moreLabel any = "⋮"
	if opts.MoreIcon != nil {
		moreLabel = opts.MoreIcon
	}
	more := Div(
		divider(),
		Button(moreLabel).
			Class(joinClass("q-toolbar-more", opts.MoreClass)).
			Title(title).
			Attr("aria-label", title).
			Attr("aria-haspopup", "true").
			Attr("aria-expanded", boolAttr(open && k < n)).
			RefTo(moreBtn).
			OnClick(func() {
				if open {
					closePanel()
				} else {
					setOpen(true)
				}
			}),
	).Key("q-toolbar-more").Class("q-toolbar-group").RefTo(moreWrap)
	if k >= n {
		more.Attr("style", toolbarHiddenStyle)
	}
	kids = append(kids, more)

	if open && k < n && *moreBtn != nil {
		panel := make([]any, 0, 2*(n-k))
		for i, g := range groups[k:] {
			if i > 0 && !g.Joined {
				panel = append(panel, divider())
			}
			for _, it := range g.Items {
				panel = append(panel, it)
			}
		}
		kids = append(kids, Popover(*moreBtn, PopoverOptions{
			AlignEnd:  true,
			OnDismiss: closePanel,
			Label:     title,
		}, Div(panel...).Class(joinClass("q-toolbar-overflow", opts.PanelClass))))
	}

	els := *groupEls
	wrap := moreWrap
	return Div(kids...).
		Class(joinClass("q-toolbar", opts.Class)).
		Attr("style", "position:relative").
		OnLayout(func(row *htmlcss.El) {
			next, ok := toolbarFitFor(row, groups, els, *wrap)
			if !ok || next == k {
				return
			}
			apply := func() {
				setFit(next)
				if next >= n && open {
					closePanel()
				}
				rt.Flush()
			}
			if win := row.Window(); win != nil {
				win.AfterLayout(apply)
			} else {
				apply()
			}
		})
}

// toolbarFitFor reads the measured geometry of a laid-out toolbar row and
// returns how many leading groups fit; ok is false while any piece is not
// mounted yet.
func toolbarFitFor(row *htmlcss.El, groups []ToolbarGroup, els map[string]*htmlcss.El, more *htmlcss.El) (int, bool) {
	if row == nil || more == nil {
		return 0, false
	}
	widths := make([]float32, len(groups))
	for i, g := range groups {
		el := els[g.Key]
		if el == nil {
			return 0, false
		}
		widths[i] = el.Bounds().W
	}
	var gap float32
	switch l := row.LayoutEngine.(type) {
	case qui.FlexLayout:
		gap = l.Gap
	case *qui.FlexLayout:
		if l != nil {
			gap = l.Gap
		}
	}
	avail := row.Bounds().Inset(qui.StyleValue(row).Padding).W
	return overflowFit(widths, gap, more.Bounds().W, avail), true
}

// overflowFit returns how many leading items of widths w fit in avail when
// laid out gap apart: all of them if they fit as they are, otherwise the
// most that still leave room for the More button (another gap plus more).
// Every input is independent of the answer, so feeding the result back
// into the layout can't make it oscillate.
func overflowFit(w []float32, gap, more, avail float32) int {
	const tol = 0.5
	var total float32
	for i, x := range w {
		total += x
		if i > 0 {
			total += gap
		}
	}
	if total <= avail+tol {
		return len(w)
	}
	var used float32
	for i, x := range w {
		next := used + x
		if i > 0 {
			next += gap
		}
		if next+gap+more > avail+tol {
			return i
		}
		used = next
	}
	return len(w)
}

func joinClass(base, extra string) string {
	if extra == "" {
		return base
	}
	return base + " " + extra
}

func boolAttr(b bool) string {
	if b {
		return "true"
	}
	return "false"
}

// toolbarCSS is the Toolbar's built-in look (framework origin — see
// htmlcss.RegisterFrameworkCSS); app rules override it.
const toolbarCSS = `
.q-toolbar { display: flex; flex-direction: row; flex-wrap: nowrap; align-items: center; }
.q-toolbar-group {
	display: flex; flex-direction: row; align-items: center;
	flex-shrink: 0; gap: 1px;
}
.q-toolbar-group > * { flex-shrink: 0; }
.q-toolbar-divider { width: 1px; height: 20px; margin: 0 4px; background: #dadce0; }
.q-toolbar-more { min-width: 28px; height: 30px; }
.q-toolbar-overflow {
	display: flex; flex-direction: row; flex-wrap: wrap; align-items: center;
	gap: 2px; max-width: 480px; padding: 4px 6px;
	background: #ffffff; border-radius: 8px;
	box-shadow: 0 2px 10px rgba(0,0,0,0.22);
}
.q-toolbar-overflow > * { flex-shrink: 0; }
`

func init() { htmlcss.RegisterFrameworkCSS(toolbarCSS) }
