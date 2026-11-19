package htmlcss

import (
	"bytes"
	"encoding/base64"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"

	_ "golang.org/x/image/bmp"
	_ "golang.org/x/image/tiff"
	_ "golang.org/x/image/webp"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/qizhanchan/qui"
	"github.com/qizhanchan/qui/widgets"
)

// QuirksMode is a document's rendering mode.
type QuirksMode uint8

const (
	NoQuirksMode QuirksMode = iota
	LimitedQuirksMode
	QuirksModeFull
)

// Options tune box construction.
type Options struct {
	// BaseDir resolves relative <img src> paths. Empty = current dir.
	BaseDir string
	// Window, when set, lets <select> build a real Select widget (which
	// needs a *Window). Nil → <select> renders as a disabled placeholder.
	Window *qui.Window
	// ViewportWidth overrides the width used to evaluate @media queries
	// (one-shot at parse). 0 → use the Window width, else a desktop default.
	ViewportWidth float32
	// ViewportHeight is what vh units resolve against (0 keeps the last
	// value; vw follows ViewportWidth).
	ViewportHeight float32
	// ClassicScrollbarWidth is the width reserved by scrollbar-gutter:stable.
	// Zero models overlay scrollbars, which consume no layout space.
	ClassicScrollbarWidth float32
	// DocumentRoot builds the tree from the <html> element instead of
	// <body>, the way a browser lays out a whole document: body is an
	// ordinary block inside it (its UA 8px margin applies) and the root
	// carries html's own styles. Apps leave it off and treat body as the
	// window's root.
	DocumentRoot bool
	// Quirks is the document's mode (HTML § 13.2.6.4.1, from its DOCTYPE):
	// in QuirksMode and LimitedQuirksMode a line's strut — the block's own
	// font and line-height — only counts on lines with text directly in
	// the block (the quirks spec's line height calculation quirk).
	Quirks QuirksMode
	// Resources, when set, loads the document's external images (remote
	// URLs and anything else the host resolves). See ResourceLoader.
	Resources ResourceLoader
	// FontFamilies maps authored family names (lowercase) to host-scoped registrations.
	FontFamilies map[string]string
	// ScriptDOM builds the elements that never render — <head>, <script>,
	// <style>, <meta>, <link>, <title> — as hidden (display:none) elements
	// too, so a script engine sees the whole document: document.head,
	// meta tags, JSON data blocks, the running script's own element.
	// Script and style text is kept verbatim.
	ScriptDOM bool
}

// minIsAuto reports a min-width / min-height value of `auto` (the
// initial value, so also an absent one).
func minIsAuto(v string) bool {
	v = strings.TrimSpace(strings.ToLower(v))
	return v == "" || v == "auto"
}

// textLikeInline are inline elements whose content folds into the parent
// text run as styled spans (rather than becoming standalone widgets).
var textLikeInline = map[string]bool{
	"span": true, "strong": true, "b": true, "em": true, "i": true,
	"small": true, "code": true, "label": true, "u": true, "mark": true,
	"abbr": true, "sub": true, "sup": true, "a": true, "br": true,
	"s": true, "del": true, "strike": true, "ins": true,
	"time": true, "cite": true, "q": true, "dfn": true, "kbd": true,
	"samp": true, "var": true, "bdi": true, "bdo": true, "data": true,
	"font": true, "big": true, "tt": true, "nobr": true, "acronym": true,
	"output": true, "ruby": true, "rt": true, "rp": true,
}

// inlineVAlignOf maps an element's computed vertical-align to the engine's
// alignment mode. Default (and unrecognized) is baseline, per CSS.
func inlineVAlignOf(cs *ComputedStyle) qui.InlineVAlign {
	if cs == nil {
		return qui.InlineBaseline
	}
	switch strings.ToLower(strings.TrimSpace(cs.raw["vertical-align"])) {
	case "middle":
		return qui.InlineMiddle
	case "top", "text-top":
		return qui.InlineTop
	case "bottom", "text-bottom":
		return qui.InlineBottom
	default:
		return qui.InlineBaseline
	}
}

// foldSpans converts an inline node into styled text spans.
// buildSVGImage parses an .svg file and returns a vector-drawing widget
// sized from CSS width/height (default 20px square) and tinted with the
// computed text color (icon glyphs are monochrome).
// loadRasterImage decodes a local raster image, resolving relative paths
// against baseDir. Remote / data URLs are unsupported and return nil.
func loadRasterImage(src, baseDir string) image.Image {
	if src == "" || strings.HasPrefix(src, "http") {
		return nil
	}
	// data: URI — decode an inline base64 (or raw) image payload.
	if strings.HasPrefix(src, "data:") {
		return decodeDataURIImage(src)
	}
	path := src
	if !filepath.IsAbs(path) {
		path = filepath.Join(baseDir, src)
	}
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()
	img, _, err := image.Decode(f)
	if err != nil {
		return nil
	}
	return img
}

// decodeDataURIImage decodes a `data:[<mime>][;base64],<payload>` image URI.
func decodeDataURIImage(uri string) image.Image {
	comma := strings.IndexByte(uri, ',')
	if comma < 0 {
		return nil
	}
	meta, payload := uri[len("data:"):comma], uri[comma+1:]
	var raw []byte
	if strings.Contains(strings.ToLower(meta), "base64") {
		b, err := base64.StdEncoding.DecodeString(strings.TrimSpace(payload))
		if err != nil {
			return nil
		}
		raw = b
	} else {
		// percent-decoded text payloads aren't images we can decode; try raw.
		raw = []byte(payload)
	}
	img, _, err := image.Decode(bytes.NewReader(raw))
	if err != nil {
		return nil
	}
	return img
}

// buildListItem builds a <li> as a horizontal row [marker | content] so
// the bullet / number sits to the left of the (possibly multi-line) item
// content. The li's own box model (margin/padding/background/border) is
// applied to the row. Returns nil to fall through to the generic builder
// when the li isn't inside a list (no marker context).
// listMarker returns the bullet / number string for a <li>, honoring
// list-style-type (inherited from the parent list). Empty string means no
// marker (list-style-type: none, or the li isn't directly inside a list).
func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

// chooseLayout maps computed display to a qui layout engine.
func chooseLayout(cs *ComputedStyle) qui.Layout {
	if isFlexDisplay(cs.Display) {
		// column-gap separates items in a row and row-gap the lines a
		// row wraps into; a column flips them
		gap, cross := cs.ColumnGap, cs.RowGap
		if cs.FlexDirection == qui.Vertical {
			gap, cross = cross, gap
		}
		// A column wraps only at a definite height: with an auto one the
		// container grows to its items (one line), as Chrome lays out
		// go.dev's `flex-flow: column wrap` menus.
		wrap := cs.Wrap
		if wrap && cs.FlexDirection == qui.Vertical && !cs.HasHeight && cs.HeightPct == 0 &&
			cs.MaxHeight == 0 && cs.MaxHeightPct == 0 &&
			!(cs.HasTop && cs.HasBottom && (cs.Position == "absolute" || cs.Position == "fixed")) {
			wrap = false
		}
		return qui.FlexLayout{
			CSSSizing: webCompat, Direction: cs.FlexDirection,
			Justify:      cs.Justify,
			AlignItems:   cs.AlignItems,
			Gap:          gap,
			CrossGap:     cross,
			Wrap:         wrap,
			AlignContent: cs.AlignContent,
		}
	}
	if n, w := columnDimensions(cs); n > 1 || w > 0 {
		gap := cs.FontSize
		if v, ok := cs.raw["column-gap"]; ok && v != "normal" {
			gap = cs.ColumnGap
		}
		return qui.ColumnsLayout{Count: n, Width: w, Gap: gap}
	}
	return qui.FlowLayout{Align: cs.TextAlign}
}

// --- style application ---
//
// The appliers below translate a *ComputedStyle into qui widget styling.
// They are free functions so every El — whether mounted by the reactive
// layer or compiled statically by Render (static.go) — styles through
// the same code.

func applyBox(box *widgets.Box, cs *ComputedStyle, forceContainingBlock bool) {
	// El instances are reused across restyles, so every CSS-driven visual /
	// size property must be recomputed from scratch: clear them to their
	// initial values here, then let applyCommon (+ the gradient / position /
	// flex-item logic) set only what the new computed style specifies. Without
	// this, a declaration dropped by a class change lingers on the reused box
	// (e.g. a row losing its selected-state background, or a panel that had
	// flex-grow:1 in another view continuing to grow). This mirrors the
	// explicit resets position / gradient / filter / flex-item already do
	// below. applyBox is the El-box path only — backing form-control widgets
	// style through applyCommon directly and keep their built-in defaults, so
	// this reset never wipes native control chrome.
	//
	// NOTE the zero values are the CSS "unspecified" defaults: Opacity 0 is
	// treated as 1 by the renderer; Width/Height/Min/Max 0 mean "no
	// constraint"; a zero Color is transparent.
	qui.UpdateStyle(box, func(bst *qui.Style) {
		bst.Background = qui.Color{}
		bst.Border, bst.BorderSize = qui.Color{}, 0
		bst.BorderWidths, bst.BorderColors = qui.Insets{}, qui.SideColors{}
		bst.Radius, bst.Corners = 0, qui.CornerRadii{}
		bst.Width, bst.Height = 0, 0
		bst.MinWidth, bst.MinHeight, bst.MaxWidth, bst.MaxHeight = 0, 0, 0, 0
		bst.Opacity = 0
		bst.Shadow, bst.ExtraShadows = qui.ShadowStyle{}, nil
		applyCommonStyle(bst, cs)
	})
	applyFlexItem(box, cs)
	applyCursor(box, cs)
	applyPointerEvents(box, cs)
	box.BackgroundShader = nil

	// Reset both ways: a reused El whose display changed must not keep the
	// old flow level.
	box.Display = qui.FlowBlock
	if strings.HasPrefix(cs.Display, "inline") {
		box.Display = qui.FlowInline
	}
	// Interactive-state box styling — attach only when the state actually
	// changes a box decoration, so we don't repaint every box on every
	// hover / focus / press.
	box.Hover = stateBoxStyle(cs, cs.Hover)
	box.AncestorHover = stateBoxStyle(cs, cs.AncestorHover)
	box.Focus = stateBoxStyle(cs, cs.Focus)
	box.FocusVisible = stateBoxStyle(cs, cs.FocusVisible)
	box.Active = stateBoxStyle(cs, cs.Active)
	// Gradient background + CSS transform are Box-only paint features.
	if cs.Gradient != nil {
		g := cs.Gradient
		box.BackgroundShader = func(b qui.Rect) qui.Shader { return g.shaderFor(b) }
	}
	if cs.Transform != nil {
		t := cs.Transform
		box.Transform = &widgets.BoxTransform{
			TX: t.tx, TY: t.ty, Rotate: t.rotate, SX: t.sx, SY: t.sy,
			KX: t.kx, KY: t.ky, Matrix: t.matrix,
			OX: t.ox, OY: t.oy, HasOrigin: t.hasOrigin,
		}
		if ops := t.ops; len(ops) > 0 {
			box.Transform.Resolve = func(w, h float32) qui.Matrix { return resolveTransformOps(ops, w, h) }
		}
	} else {
		// Reused El: a dropped `transform` must clear, not linger.
		box.Transform = nil
	}
	// Reset first (reused El): a dropped `overflow` must stop clipping.
	box.ClipChildren = cs.OverflowClip
	box.Filter = cs.Filter // CSS filter: blur()/drop-shadow() (nil clears)
	box.BackdropFilter = cs.BackdropFilter
	applyPositioning(box, cs, forceContainingBlock)
	applyStacking(box, cs)
	// A focusable box (:focus styling) is an interactive control — make its
	// text content non-selectable so a click focuses the box itself rather
	// than being captured by a selectable child Label.
	if box.Focus != nil || box.FocusVisible != nil {
		disableTextSelection(box)
	}
}

// positionedBox is what applyPositioning needs from a widget.
type positionedBox interface {
	SetViewportPositioning(qui.ViewportPositioning)
	SetPositionOffset(qui.Point)
	SetOutOfFlow(bool)
	SetEstablishesAbsContainingBlock(bool)
	SetAbsolutePosition(qui.AbsolutePosition)
}

// applyPositioning maps CSS position + insets onto the box: a relative
// offset, or out-of-flow placement for absolute / fixed.
func applyPositioning(box positionedBox, cs *ComputedStyle, forceContainingBlock bool) {
	// Positioning. Compute the final state first, then commit each controlled
	// property once. Clearing and re-setting an unchanged absolute position
	// would otherwise create spurious layout invalidations on every restyle.
	positionOffset := qui.Point{}
	outOfFlow := false
	// A transform or a filter makes the box the containing block of its
	// absolute (and fixed) descendants, positioned or not (CSS Transforms 1
	// §2, Filter Effects 1 §4): react.dev's rotated photo cards hang an
	// `absolute inset-0` shadow layer on themselves.
	establishesContainingBlock := forceContainingBlock || cs.Transform != nil || cs.Filter != nil
	absolutePosition := qui.AbsolutePosition{}
	switch cs.Position {
	case "relative":
		// In flow, shifted by top/left/right/bottom; also a containing block.
		positionOffset = qui.Point{
			X: relOffset(cs.HasLeft, cs.Left, cs.HasRight, cs.Right),
			Y: relOffset(cs.HasTop, cs.Top, cs.HasBottom, cs.Bottom),
		}
		establishesContainingBlock = true
	case "sticky":
		// In flow; a document viewport shifts it while scrolling
		// (qui.StickyRect) by these insets. A containing block, being
		// positioned.
		establishesContainingBlock = true
		var anchor qui.AnchorSide
		if cs.HasLeft {
			absolutePosition.Left, anchor = cs.Left, anchor|qui.AnchorLeft
		}
		if cs.HasRight {
			absolutePosition.Right, anchor = cs.Right, anchor|qui.AnchorRight
		}
		if cs.HasTop {
			absolutePosition.Top, anchor = cs.Top, anchor|qui.AnchorTop
		}
		if cs.HasBottom {
			absolutePosition.Bottom, anchor = cs.Bottom, anchor|qui.AnchorBottom
		}
		absolutePosition.Anchor = anchor
	case "absolute", "fixed":
		// Out of flow; positioned relative to the nearest positioned ancestor
		// (Container.layoutAbsoluteDescendants). Being positioned, it is also a
		// containing block for its own absolute descendants.
		outOfFlow = true
		establishesContainingBlock = true
		extraH, extraV := cs.boxSizingExtra()
		var anchor qui.AnchorSide
		if cs.HasLeft {
			absolutePosition.Left, anchor = cs.Left, anchor|qui.AnchorLeft
		}
		if cs.HasRight {
			absolutePosition.Right, anchor = cs.Right, anchor|qui.AnchorRight
		}
		if cs.HasTop {
			absolutePosition.Top, anchor = cs.Top, anchor|qui.AnchorTop
		}
		if cs.HasBottom {
			absolutePosition.Bottom, anchor = cs.Bottom, anchor|qui.AnchorBottom
		}
		if cs.HasWidth {
			absolutePosition.Width = cs.Width + extraH
		}
		if cs.HasHeight {
			absolutePosition.Height = cs.Height + extraV
		}
		absolutePosition.TopPct, absolutePosition.RightPct = cs.InsetPct[0], cs.InsetPct[1]
		absolutePosition.BottomPct, absolutePosition.LeftPct = cs.InsetPct[2], cs.InsetPct[3]
		absolutePosition.WidthPct, absolutePosition.HeightPct = cs.WidthPct, cs.HeightPct
		absolutePosition.Anchor = anchor
		absolutePosition.InlineStatic = cs.InlineStatic
	}
	vpp := qui.ViewportNone
	switch cs.Position {
	case "fixed":
		vpp = qui.ViewportFixed
	case "sticky":
		vpp = qui.ViewportSticky
	}
	box.SetViewportPositioning(vpp)
	box.SetPositionOffset(positionOffset)
	box.SetOutOfFlow(outOfFlow)
	box.SetEstablishesAbsContainingBlock(establishesContainingBlock)
	box.SetAbsolutePosition(absolutePosition)
}

// relOffset resolves a relative-position axis offset: the start side
// (left/top) shifts positively, the end side (right/bottom) negatively;
// the start side wins when both are set.
func relOffset(hasStart bool, start float32, hasEnd bool, end float32) float32 {
	if hasStart {
		return start
	}
	if hasEnd {
		return -end
	}
	return 0
}

// disableTextSelection turns off Selectable on every descendant Label so
// the containing (focusable) box receives click focus instead.
func disableTextSelection(w qui.Widget) {
	if lbl, ok := w.(*widgets.Label); ok {
		lbl.Selectable = false
	}
	if cl, ok := w.(interface{ ChildList() []qui.Widget }); ok {
		for _, c := range cl.ChildList() {
			disableTextSelection(c)
		}
	}
}

// stateBoxStyle builds the Box decoration override for an interactive
// state variant, or nil when the variant doesn't change any decoration.
func stateBoxStyle(base, variant *ComputedStyle) *qui.Style {
	if variant == nil || !boxDecorationsDiffer(base, variant) {
		return nil
	}
	return &qui.Style{
		Background: variant.Background,
		Border:     variant.BorderColor,
		BorderSize: variant.BorderWidth,
		Radius:     variant.Radius,
	}
}

// defaultButtonStates synthesizes the built-in hover + press feedback a
// raw HTML <button> shows when the author CSS doesn't gate anything on
// :hover / :active. This restores the affordance that existed while
// <button> was backed by widgets.Button (which carries default hover
// states); the El/Box rewrite dropped it, so a plain <button> stopped
// highlighting under the cursor.
//
// An opaque button face darkens; a transparent button (e.g. a toolbar
// button whose author CSS set background:transparent) gets a subtle dark
// overlay that reads over whatever parent shows through — the Box overlay
// replaces (not composites) the fill, so a low-alpha black paints as a
// faint darkening of the parent.
func defaultButtonStates(cs *ComputedStyle) (hover, active *qui.Style) {
	mk := func(darkFrac, overlayA float32) *qui.Style {
		var bg qui.Color
		if cs.Background.A > 0 {
			c := cs.Background
			bg = qui.Color{R: c.R * (1 - darkFrac), G: c.G * (1 - darkFrac), B: c.B * (1 - darkFrac), A: c.A}
		} else {
			bg = qui.Color{A: overlayA} // black at overlayA over the parent
		}
		return &qui.Style{Background: bg}
	}
	return mk(0.08, 0.08), mk(0.16, 0.16)
}

// boxDecorationsDiffer reports whether v changes any painted box decoration
// relative to the resting style base.
func boxDecorationsDiffer(base, v *ComputedStyle) bool {
	return v.HasBackground != base.HasBackground || v.Background != base.Background ||
		(v.BorderWidth != base.BorderWidth || ((v.BorderWidth > 0 || base.BorderWidth > 0) && v.BorderColor != base.BorderColor)) ||
		v.SideWidths != base.SideWidths || ((v.HasSideBorders || base.HasSideBorders) && v.SideColors != base.SideColors) ||
		v.Radius != base.Radius
}

// applyCommon copies box-model + sizing + font CSS onto any widget's
// Style. The font matters for text-bearing controls (Button, Input,
// Select, CheckBox) so an inherited font-size / weight / family from an
// ancestor (e.g. a form's `font-size`) reaches them; it's a harmless
// no-op on containers/images that don't render text.
func applyCommon(w qui.Widget, cs *ComputedStyle) {
	qui.UpdateStyle(w, func(st *qui.Style) {
		applyCommonStyle(st, cs)
	})
	applyFlexItem(w, cs)
	applyCursor(w, cs)
	applyPointerEvents(w, cs)
}

// applyCursor pushes CSS `cursor` onto the widget as a DECLARED shape, which
// outranks any built-in shape below it in the hover path (the I-beam a
// selectable label or text field claims). Clearing on the way out matters
// for restyle: an element that no longer computes a cursor must hand the
// decision back. See qui/cursor.go for the resolution rule.
func applyCursor(w qui.Widget, cs *ComputedStyle) {
	type cursorSink interface {
		SetCursorShape(qui.CursorShape)
		ClearCursorShape()
	}
	sink, ok := w.(cursorSink)
	if !ok {
		return
	}
	if cs.HasCursor {
		sink.SetCursorShape(cs.Cursor)
	} else {
		sink.ClearCursorShape()
	}
}

// applyPointerEvents pushes CSS `pointer-events` onto the widget. Set in
// both directions: the property inherits, so restyling a subtree out of a
// `none` scope must make its elements hittable again.
func applyPointerEvents(w qui.Widget, cs *ComputedStyle) {
	if sink, ok := w.(interface{ SetPointerTransparent(bool) }); ok {
		sink.SetPointerTransparent(cs.PointerNone)
	}
}

func applyCommonStyle(st *qui.Style, cs *ComputedStyle) {
	st.Font = fontFrom(cs)
	if cs.HasBackground {
		st.Background = cs.Background
	}
	if cs.HasSideBorders {
		// The sides are complete (seeded from the uniform border, then the
		// per-side overrides), so they replace the control's native uniform
		// border: `border: none; border-right: 1px …` on an <input> is one
		// right-hand line, not that line plus the native frame.
		st.BorderWidths = cs.SideWidths
		st.BorderColors = cs.SideColors
		st.BorderStyle = cs.BorderStyle
		st.BorderSize = 0
		st.Border = qui.Color{}
	} else if cs.BorderWidth > 0 {
		st.BorderSize = cs.BorderWidth
		st.Border = cs.BorderColor
		st.BorderStyle = cs.BorderStyle
	} else if cs.HasBorder {
		// `border: none` / zero width authored — clear any native border.
		st.BorderSize = 0
		st.Border = qui.Color{}
	}
	if cs.Radius > 0 {
		st.Radius = cs.Radius
	}
	if !cs.Corners.IsZero() {
		st.Corners = cs.Corners
	}
	st.Padding = cs.Padding
	st.Margin = cs.Margin
	// box-sizing: for the default content-box, the declared width/height is
	// the CONTENT box, so add padding+border to reach the outer box that
	// qui.Style.Width/Height denote. border-box adds nothing (already outer).
	extraH, extraV := cs.boxSizingExtra()
	st.WidthSet, st.HeightSet = cs.HasWidth, cs.HasHeight
	if cs.HasWidth {
		st.Width = cs.Width + extraH
	}
	if cs.HasHeight {
		st.Height = cs.Height + extraV
	}
	// Percentage sizes: the layout engines resolve them against the
	// containing block, then add PctExtra — the same content-box padding +
	// border compensation the absolute sizes get above.
	st.WidthPct = cs.WidthPct
	st.HeightPct = cs.HeightPct
	st.PctExtra = qui.Size{W: extraH + cs.WidthPctOffset, H: extraV + cs.HeightPctOffset}
	st.AspectRatio, st.AspectBorderBox = cs.AspectRatio, cs.BoxSizing == "border-box"
	st.MarginLeftAuto = cs.MarginLeftAuto
	st.MarginRightAuto = cs.MarginRightAuto
	st.MarginTopAuto = cs.MarginTopAuto
	st.MarginBottomAuto = cs.MarginBottomAuto
	st.Float, st.Clear = qui.FloatNone, qui.ClearNone
	switch cs.Float {
	case "left":
		st.Float = qui.FloatLeft
	case "right":
		st.Float = qui.FloatRight
	}
	switch cs.Clear {
	case "left":
		st.Clear = qui.ClearLeft
	case "right":
		st.Clear = qui.ClearRight
	case "both":
		st.Clear = qui.ClearBoth
	}
	st.NewFormattingContext = cs.newFormattingContext()
	// Web-compat: a flex item's min-width/min-height of `auto` is its
	// content size (scroll containers' is 0).
	st.FlexAutoMinWidth, st.FlexAutoMinHeight = false, false
	if webCompat && !cs.OverflowScroll && !cs.OverflowClip {
		st.FlexAutoMinWidth = minIsAuto(cs.raw["min-width"])
		st.FlexAutoMinHeight = minIsAuto(cs.raw["min-height"])
	}
	if cs.MinWidth > 0 {
		st.MinWidth = cs.MinWidth + extraH
	}
	if cs.MinHeight > 0 {
		st.MinHeight = cs.MinHeight + extraV
	}
	if cs.MaxWidth > 0 {
		st.MaxWidth = cs.MaxWidth + extraH
	}
	if cs.MaxHeight > 0 {
		st.MaxHeight = cs.MaxHeight + extraV
	}
	// Percentage min/max resolve at layout time against the containing
	// block (like Width/HeightPct), plus PctExtra.
	st.MinWidthPct = cs.MinWidthPct
	st.MinHeightPct = cs.MinHeightPct
	st.MaxWidthPct = cs.MaxWidthPct
	st.MaxHeightPct = cs.MaxHeightPct
	// Chrome's table algorithm determines a cell's used width; max-width
	// must not shrink that cell inside the column after tracks are solved.
	// Doing so also wraps its text at the smaller cap and inflates rows.
	if webCompat && cs.Display == "table-cell" {
		st.MaxWidth, st.MaxWidthPct = 0, 0
	}
	// Flex sizing is reset every restyle — El instances are reused across
	// renders (and across conditionally-swapped subtrees), so a dropped
	// flex-grow / flex-basis / align-self must clear rather than linger.
	// (Mirrors the unconditional reset the FlexItem block below already does;
	// without this, e.g. a container that had flex-grow:1 in one view keeps
	// growing after the class changes to one that never sets it.)
	st.Grow = cs.FlexGrow
	st.Basis = 0
	// Per-item flex: align-self / flex-basis via Style; flex-shrink (incl. the
	// non-shrink 0 case) and order go through the FlexItem interface, which
	// Style can't express (Shrink==0 collapses with "unset" → CSS default 1).
	st.AlignSelf = cs.AlignSelf
	st.JustifySelf = cs.JustifySelf
	if cs.HasFlexBasis {
		st.Basis = cs.FlexBasis
	}
	st.BasisPct, st.BasisExtra, st.BasisExtraV = cs.FlexBasisPct, 0, 0
	if cs.FlexBasisPct > 0 {
		// flex-basis sizes the content box like width does (content-box).
		st.BasisExtra = cs.FlexBasisPctOffset + extraH
		st.BasisExtraV = cs.FlexBasisPctOffset + extraV
	}
	st.BasisAuto = cs.FlexBasisAuto
	if cs.HasShadow {
		st.Shadow = cs.Shadow
		st.ExtraShadows = cs.ExtraShadows
	}
	if cs.HasOpacity {
		st.Opacity = cs.Opacity
	}
}

func applyFlexItem(w qui.Widget, cs *ComputedStyle) {
	// Reset each restyle (El instances are reused): a dropped rule must clear
	// the old value rather than linger.
	qui.UpdateFlexItem(w, func(f *qui.FlexItem) {
		f.Order = cs.Order
		f.NoShrink = cs.HasFlexShrink && cs.FlexShrink == 0
		if cs.HasFlexShrink && cs.FlexShrink > 0 {
			f.Shrink = cs.FlexShrink
		} else {
			f.Shrink = 0
		}
	})
}

// applyControlColors pushes CSS `color` and `accent-color` onto a backing
// form control's chrome — the parts applyCommon doesn't reach. This is
// what lets a dark stylesheet fully retint controls whose native default
// is light (see widgets/native.go): `color` drives editable text, and
// `accent-color` drives the checkbox tick box / radio dot / switch track /
// slider active track. Each is applied only when authored (HasColor /
// HasAccentColor) so unstyled controls keep their UA look and the
// native-alignment harness stays intact.
func applyControlColors(w qui.Widget, cs *ComputedStyle, native **qui.StateStyle) {
	switch c := w.(type) {
	case *widgets.Input:
		if cs.HasColor {
			qui.UpdateStyle(c, func(style *qui.Style) { style.Foreground = cs.Color })
		}
		syncControlStates(&c.States, cs, native)
	case *widgets.TextArea:
		if cs.HasColor {
			qui.UpdateStyle(c, func(style *qui.Style) { style.Foreground = cs.Color })
		}
		// overflow: hidden on a <textarea> hides its scrollbar on that axis
		// (the text still scrolls with the caret, as in a browser).
		c.ShowScrollBarX, c.ShowScrollBarY = !cs.OverflowClipX, !cs.OverflowClipY
		// TextArea has no state table, only per-state overrides; a disabled
		// element's rules are already in cs (see syncControlStates), so the
		// resting style carries the disabled look.
		c.FocusedStyle, c.HoverStyle = nil, nil
		if v := focusVariant(cs); v != nil {
			st := *c.Style()
			overlayStateChrome(&st, cs, v)
			c.FocusedStyle = &st
		}
		if v := cs.Hover; v != nil && (boxDecorationsDiffer(cs, v) || v.Color != cs.Color) {
			st := *c.Style()
			overlayStateChrome(&st, cs, v)
			c.HoverStyle = &st
		}
	case *widgets.Select:
		c.NoAppearance = cs.raw["appearance"] == "none" || cs.raw["-webkit-appearance"] == "none"
		if cs.HasColor {
			qui.UpdateStyle(c, func(style *qui.Style) { style.Foreground = cs.Color })
		}
		syncControlStates(&c.States, cs, native)
	case *widgets.CheckBox:
		if cs.HasAccentColor {
			c.CheckedFillColor = cs.AccentColor
		}
		// An authored `color` on the checkbox itself tints the check mark,
		// and an authored `border-color` its outline in both states — so
		// a dark stylesheet can draw a dark box with a light mark.
		if cs.ColorOwn {
			c.CheckMarkColor = cs.Color
		}
		if cs.HasBorder && cs.BorderColor.A > 0 {
			c.OutlineColor = cs.BorderColor
			c.CheckedOutlineColor = cs.BorderColor
		}
		// An authored background fills the box in both states (a checked
		// box keeps it unless accent-color says otherwise — the mark then
		// carries the state, as in JetBrains' dark checkboxes); an
		// authored border-radius rounds it, and an explicit square
		// width / height sizes it.
		if cs.HasBackground && cs.Background.A > 0 {
			c.FillColor = cs.Background
			if !cs.HasAccentColor {
				c.CheckedFillColor = cs.Background
			}
		}
		if cs.Radius > 0 {
			c.BoxRadius = cs.Radius
		}
		if cs.HasWidth && cs.HasHeight && cs.Width > 0 && cs.Width == cs.Height {
			c.BoxSize = cs.Width
		}
	case *widgets.RadioButton:
		if cs.HasAccentColor {
			c.SelectedColor = cs.AccentColor
		}
		if cs.HasColor {
			c.LabelColor = cs.Color
		}
	case *widgets.Switch:
		if cs.HasAccentColor {
			c.OnTrackColor = cs.AccentColor
		}
	case *widgets.Slider:
		if cs.HasAccentColor {
			c.ActiveTrackColor = cs.AccentColor
			c.HandleColor = cs.AccentColor
			c.StateLayerColor = cs.AccentColor
		}
	}
}

// syncControlStates rebuilds a backing control's interactive state styles
// for this restyle: start from the control's native states (snapshotted
// into *native on first use), carry the CSS base chrome into them
// (syncControlStateChrome), then lay the author's :hover / :focus /
// :active rules on top. Without that last step `input:focus { border-color }`
// never painted — focus lives on the backing widget, not the El, so the
// El's own state styling can't reach it, and the native focus ring won.
// Starting over from the snapshot each time is what lets a dropped rule
// (a class change) hand the state back to the native look.
func syncControlStates(ss *qui.StateStyle, cs *ComputedStyle, native **qui.StateStyle) {
	if *native == nil {
		snap := qui.StateStyle{Base: ss.Base}
		clone := func(p *qui.Style) *qui.Style {
			if p == nil {
				return nil
			}
			c := *p
			return &c
		}
		snap.Hover, snap.Pressed, snap.Focused = clone(ss.Hover), clone(ss.Pressed), clone(ss.Focused)
		snap.Disabled, snap.Checked, snap.Errored = clone(ss.Disabled), clone(ss.Checked), clone(ss.Errored)
		*native = &snap
	}
	restore := func(dst **qui.Style, src *qui.Style) {
		if src == nil {
			*dst = nil
			return
		}
		c := *src
		*dst = &c
	}
	n := *native
	restore(&ss.Hover, n.Hover)
	restore(&ss.Pressed, n.Pressed)
	restore(&ss.Focused, n.Focused)
	restore(&ss.Disabled, n.Disabled)
	syncControlStateChrome(ss)
	// :disabled (attribute-driven, so already folded into cs when the
	// control is disabled): the control's own color and border rules beat
	// the native dimmed look, as author rules beat a browser's UA sheet.
	if d := ss.Disabled; d != nil {
		if cs.ColorOwn {
			d.Foreground = cs.Color
		}
		if cs.HasSideBorders {
			d.BorderWidths = cs.SideWidths
			d.BorderColors = cs.SideColors
			d.BorderSize, d.Border = 0, qui.Color{}
		} else if cs.HasBorder {
			d.Border = cs.BorderColor
			d.BorderSize = cs.BorderWidth
		}
	}
	overlay := func(slot **qui.Style, v *ComputedStyle) {
		if v == nil || (!boxDecorationsDiffer(cs, v) && v.Color == cs.Color) {
			return
		}
		if *slot == nil {
			c := ss.Base
			*slot = &c
		}
		overlayStateChrome(*slot, cs, v)
	}
	overlay(&ss.Hover, cs.Hover)
	// `outline: none` (on the control or its :focus rule) drops the native
	// focus ring: focus keeps the resting border, and only an author :focus
	// border — overlaid next — changes it.
	if fv := focusVariant(cs); ss.Focused != nil && (cs.OutlineNone || (fv != nil && fv.OutlineNone)) {
		f := ss.Focused
		f.Border, f.BorderSize = ss.Base.Border, ss.Base.BorderSize
		f.BorderWidths, f.BorderColors = ss.Base.BorderWidths, ss.Base.BorderColors
	}
	overlay(&ss.Focused, focusVariant(cs))
	overlay(&ss.Pressed, cs.Active)
}

// focusVariant picks the author's focus styling for a text-entry control.
// A text field matches :focus-visible on any focus (the CSS heuristic —
// the user is about to type), so a :focus-visible rule applies just like
// :focus; when both exist :focus-visible, the narrower one, wins.
func focusVariant(cs *ComputedStyle) *ComputedStyle {
	if cs.FocusVisible != nil {
		return cs.FocusVisible
	}
	return cs.Focus
}

// overlayStateChrome copies onto s the decorations the state variant v
// changes relative to the resting style base: background, border, text
// color. Fields v leaves alone keep s's (native) value — a :focus rule that
// only sets `background` keeps the native focus border.
func overlayStateChrome(s *qui.Style, base, v *ComputedStyle) {
	if v.Background != base.Background || v.HasBackground != base.HasBackground {
		s.Background = v.Background
	}
	if v.HasSideBorders {
		s.BorderWidths = v.SideWidths
		s.BorderColors = v.SideColors
	} else if v.BorderColor != base.BorderColor || v.BorderWidth != base.BorderWidth {
		s.Border = v.BorderColor
		s.BorderSize = v.BorderWidth
	}
	if v.Radius != base.Radius {
		s.Radius = v.Radius
	}
	if v.Color != base.Color {
		s.Foreground = v.Color
	}
}

// syncControlStateChrome re-derives the interactive state styles' shared
// chrome (background / foreground / font / corner) from Base, preserving
// each state's own accent (Focused's border, Disabled's dimmed text +
// border). The native control states (widgets/native.go) are full Style
// copies frozen at widget construction, so a CSS `background`/`color`
// change on Base would otherwise vanish the instant the control is
// focused or hovered — StateStyle.Resolve returns the stale copy. Runs on
// every restyle (idempotent); a no-op for unstyled controls whose states
// already mirror Base.
func syncControlStateChrome(ss *qui.StateStyle) {
	base := ss.Base
	sync := func(s *qui.Style, keepForeground bool) {
		if s == nil {
			return
		}
		s.Background = base.Background
		s.Font = base.Font
		s.Radius = base.Radius
		s.Corners = base.Corners
		if !keepForeground {
			s.Foreground = base.Foreground
		}
	}
	sync(ss.Hover, false)
	sync(ss.Pressed, false)
	sync(ss.Focused, false)
	sync(ss.Checked, false)
	sync(ss.Errored, false)
	sync(ss.Disabled, true) // keep the disabled dimmed text + its own border
}

// textLabelOpts says how an element's internal label relates to it.
type textLabelOpts struct {
	// boxless keeps margin, padding and background off the label: the
	// element's own box carries them and the label is only its typography
	// (otherwise padding would apply twice).
	boxless bool
	// noWrap keeps the label on one line whatever white-space says (list
	// markers).
	noWrap bool
}

// applyTextStyle sets a Label's text, font, color and paragraph layout
// from cs in one step, so restyling an unchanged element changes nothing:
// no transient style or text whose round trip would invalidate layout and
// repaint on every pass. Paragraph settings are plain fields, so a change
// to them invalidates layout here — the label's cached measurement
// depends on them.
func applyTextStyle(lbl *widgets.Label, text string, cs *ComputedStyle, o textLabelOpts) {
	if text != "" && cs.TextTransform != "" && cs.TextTransform != "none" {
		text = transformText(text, cs.TextTransform)
	}
	lbl.SetText(text)
	lbl.TextShadows = cs.TextShadows
	qui.UpdateStyle(lbl, func(st *qui.Style) {
		st.Foreground = cs.Color
		st.Font = fontFrom(cs)
		if o.boxless {
			st.Margin = qui.Insets{}
			st.Padding = qui.Insets{}
			st.Background = qui.Color{}
			return
		}
		st.Margin = cs.Margin
		st.Padding = cs.Padding
		if cs.HasBackground {
			st.Background = cs.Background
		}
	})
	p := lbl.Paragraph
	p.CSSBaseline = webCompat
	p.Wrap = !cs.NoWrap && !o.noWrap
	// CSS default is overflow-wrap:normal — a word with no break opportunity
	// overflows. The native Label default is the opposite (always break), so
	// this must be set on every restyle, not only when BreakWord is on.
	p.BreakLongWords = cs.BreakWord
	p.Align = cs.TextAlign
	p.Decoration = cs.decoration()
	p.DecorationPaint = cs.decorationPaint()
	if cs.Ellipsis {
		p.Ellipsis = true
		if cs.NoWrap {
			p.MaxLines = 1
		}
	}
	if cs.LineHeight > 0 {
		p.LineHeightScale = cs.LineHeight
	}
	p.FirstIndent = cs.TextIndent
	if p == lbl.Paragraph {
		return
	}
	// Decoration only paints; everything else shapes the lines.
	layoutChanged := p.Wrap != lbl.Paragraph.Wrap || p.BreakLongWords != lbl.Paragraph.BreakLongWords ||
		p.Align != lbl.Paragraph.Align || p.Ellipsis != lbl.Paragraph.Ellipsis ||
		p.MaxLines != lbl.Paragraph.MaxLines || p.LineHeightScale != lbl.Paragraph.LineHeightScale ||
		p.FirstIndent != lbl.Paragraph.FirstIndent || p.Direction != lbl.Paragraph.Direction ||
		p.CSSBaseline != lbl.Paragraph.CSSBaseline
	lbl.Paragraph = p
	if layoutChanged {
		lbl.InvalidateLayout()
	} else if win := lbl.Window(); win != nil {
		win.InvalidateRect(qui.PaintBoundsInWindow(lbl))
	}
}

// --- helpers ---

func fontFrom(cs *ComputedStyle) qui.Font {
	return qui.Font{
		Family:        cs.FontFamily,
		Size:          cs.FontSize,
		Weight:        cs.FontWeight,
		Italic:        cs.Italic,
		LetterSpacing: cs.LetterSpacing,
		WordSpacing:   cs.WordSpacing,
		Features:      cs.FontFeatures(),
	}
}

// isTextContainer reports whether an element's content should collapse
// into a single Label (all children are text / text-like inline).
// textContent returns the concatenated descendant text.
func textContent(n *Node) string {
	var sb strings.Builder
	var walk func(*Node)
	walk = func(x *Node) {
		if x.Type == TextNode {
			sb.WriteString(x.Text)
			return
		}
		for _, c := range x.Children {
			walk(c)
		}
	}
	walk(n)
	return sb.String()
}

// collapseText collapses runs of whitespace to single spaces and trims.
func collapseText(s string) string {
	// Only HTML's ASCII white space collapses: a no-break space (U+00A0)
	// or an ideographic space is content (CSS Text § 4.1.1).
	return strings.Join(strings.FieldsFunc(s, func(r rune) bool { return r < 0x80 && isSpace(byte(r)) }), " ")
}

// collapseFor applies the element's white-space rule to its text content:
//   - pre / pre-wrap (PreserveWS): keep spaces + newlines verbatim, only
//     dropping a single leading newline (HTML drops the first newline right
//     after a <pre> open tag);
//   - pre-line (PreserveNL): collapse space runs but keep explicit newlines;
//   - otherwise: the normal whitespace collapse.
func collapseFor(s string, cs *ComputedStyle) string {
	switch {
	case cs != nil && cs.PreserveWS:
		return strings.TrimPrefix(strings.TrimPrefix(s, "\r\n"), "\n")
	case cs != nil && cs.PreserveNL:
		lines := strings.Split(s, "\n")
		for i, ln := range lines {
			lines[i] = collapseText(ln)
		}
		// Drop leading/trailing all-blank lines but keep interior structure.
		return strings.Trim(strings.Join(lines, "\n"), "\n")
	default:
		return collapseText(s)
	}
}

// collapseInline collapses whitespace runs to a single space but keeps a
// leading/trailing space when present — this preserves the word gaps
// between adjacent inline pieces ("text <a>link</a> more") that a full
// trim would swallow. CSS inline whitespace collapsing.
func collapseInline(s string) string {
	if s == "" {
		return ""
	}
	leading := isSpace(s[0])
	trailing := isSpace(s[len(s)-1])
	// Only ASCII white space collapses (as in collapseText): a no-break
	// space is content — editors type U+00A0 to keep a trailing space.
	core := strings.Join(strings.FieldsFunc(s, func(r rune) bool { return r < 0x80 && isSpace(byte(r)) }), " ")
	if core == "" {
		return " " // whitespace-only node → a single separating space
	}
	if leading {
		core = " " + core
	}
	if trailing {
		core = core + " "
	}
	return core
}

// findTag returns the first descendant element with the given tag.
func findTag(n *Node, tag string) *Node {
	if n.isElement(tag) {
		return n
	}
	for _, c := range n.Children {
		if r := findTag(c, tag); r != nil {
			return r
		}
	}
	return nil
}

// applyStacking marks positioned boxes and the boxes that form a stacking
// context, so painting and hit testing layer them (widgets/stacking.go).
func applyStacking(box *widgets.Box, cs *ComputedStyle) {
	switch cs.Position {
	case "relative", "absolute", "fixed", "sticky":
		box.Positioned = true
	default:
		// A flex / grid item with a z-index stacks like a positioned box
		// (go.dev's static header, z-index: 10 in a flex body, keeps its
		// menus above the page).
		box.Positioned = cs.ZIndexItem && cs.ZIndexSet
	}
	op := cs.Opacity
	box.StackingContext = box.Positioned && cs.ZIndexSet ||
		cs.Position == "fixed" || cs.Position == "sticky" ||
		(op > 0 && op < 1) || cs.Transform != nil || cs.Filter != nil ||
		cs.ClipPath != nil || cs.Mask != nil || cs.BackdropFilter != nil ||
		strings.EqualFold(strings.TrimSpace(cs.raw["isolation"]), "isolate")
}

func columnDimensions(cs *ComputedStyle) (int, float32) {
	count, width := cs.raw["column-count"], cs.raw["column-width"]
	for _, t := range strings.Fields(cs.raw["columns"]) {
		if _, err := strconv.Atoi(t); err == nil {
			if count == "" {
				count = t
			}
		} else if t != "auto" && width == "" {
			width = t
		}
	}
	n, err := strconv.Atoi(count)
	if err != nil || n < 1 || n > 1000 {
		n = 0
	}
	w, _ := parseLength(width, cs.FontSize, 0)
	return n, max(w, 0)
}
