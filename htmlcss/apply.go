package htmlcss

import (
	"bytes"
	"encoding/base64"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"os"
	"path/filepath"
	"strings"

	"github.com/qizhanchan/qui"
	"github.com/qizhanchan/qui/widgets"
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
}

// textLikeInline are inline elements whose content folds into the parent
// text run as styled spans (rather than becoming standalone widgets).
var textLikeInline = map[string]bool{
	"span": true, "strong": true, "b": true, "em": true, "i": true,
	"small": true, "code": true, "label": true, "u": true, "mark": true,
	"abbr": true, "sub": true, "sup": true, "a": true, "br": true,
	"s": true, "del": true, "strike": true, "ins": true,
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
// listStyleFromShorthand extracts the list-style-type keyword from a
// `list-style` shorthand value (ignoring position / image parts).
func listStyleFromShorthand(v string) string {
	for _, f := range strings.Fields(strings.ToLower(v)) {
		switch f {
		case "none", "disc", "circle", "square", "decimal":
			return f
		}
	}
	return ""
}

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
	if cs.Display == "flex" {
		return qui.FlexLayout{
			Direction:    cs.FlexDirection,
			Justify:      cs.Justify,
			AlignItems:   cs.AlignItems,
			Gap:          cs.Gap,
			CrossGap:     cs.Gap,
			Wrap:         cs.Wrap,
			AlignContent: cs.AlignContent,
		}
	}
	return qui.FlowLayout{}
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
	} else {
		// Reused El: a dropped `transform` must clear, not linger.
		box.Transform = nil
	}
	// Reset first (reused El): a dropped `overflow` must stop clipping.
	box.ClipChildren = cs.OverflowClip
	box.Filter = cs.Filter // CSS filter: blur()/drop-shadow() (nil clears)
	// Positioning. Compute the final state first, then commit each controlled
	// property once. Clearing and re-setting an unchanged absolute position
	// would otherwise create spurious layout invalidations on every restyle.
	positionOffset := qui.Point{}
	outOfFlow := false
	establishesContainingBlock := forceContainingBlock
	absolutePosition := qui.AbsolutePosition{}
	switch cs.Position {
	case "relative":
		// In flow, shifted by top/left/right/bottom; also a containing block.
		positionOffset = qui.Point{
			X: relOffset(cs.HasLeft, cs.Left, cs.HasRight, cs.Right),
			Y: relOffset(cs.HasTop, cs.Top, cs.HasBottom, cs.Bottom),
		}
		establishesContainingBlock = true
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
		absolutePosition.Anchor = anchor
	}
	box.SetPositionOffset(positionOffset)
	box.SetOutOfFlow(outOfFlow)
	box.SetEstablishesAbsContainingBlock(establishesContainingBlock)
	box.SetAbsolutePosition(absolutePosition)
	// A focusable box (:focus styling) is an interactive control — make its
	// text content non-selectable so a click focuses the box itself rather
	// than being captured by a selectable child Label.
	if box.Focus != nil || box.FocusVisible != nil {
		disableTextSelection(box)
	}
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
		v.BorderWidth != base.BorderWidth || v.BorderColor != base.BorderColor ||
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
		st.BorderWidths = cs.SideWidths
		st.BorderColors = cs.SideColors
		st.BorderStyle = cs.BorderStyle
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
	if cs.HasWidth {
		st.Width = cs.Width + extraH
	}
	if cs.HasHeight {
		st.Height = cs.Height + extraV
	}
	// Percentage sizes: the layout engines resolve them against the
	// containing block; the resolved value is the outer frame (box-sizing
	// padding/border compensation can't apply — the base is unknown here).
	st.WidthPct = cs.WidthPct
	st.HeightPct = cs.HeightPct
	st.MarginLeftAuto = cs.MarginLeftAuto
	st.MarginRightAuto = cs.MarginRightAuto
	st.MarginTopAuto = cs.MarginTopAuto
	st.MarginBottomAuto = cs.MarginBottomAuto
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
	// block (like Width/HeightPct); box-sizing compensation can't apply.
	st.MinWidthPct = cs.MinWidthPct
	st.MinHeightPct = cs.MinHeightPct
	st.MaxWidthPct = cs.MaxWidthPct
	st.MaxHeightPct = cs.MaxHeightPct
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
	if cs.HasFlexBasis {
		st.Basis = cs.FlexBasis
	}
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
func applyControlColors(w qui.Widget, cs *ComputedStyle) {
	switch c := w.(type) {
	case *widgets.Input:
		if cs.HasColor {
			qui.UpdateStyle(c, func(style *qui.Style) { style.Foreground = cs.Color })
		}
		syncControlStateChrome(&c.States)
	case *widgets.TextArea:
		if cs.HasColor {
			qui.UpdateStyle(c, func(style *qui.Style) { style.Foreground = cs.Color })
		}
	case *widgets.Select:
		if cs.HasColor {
			qui.UpdateStyle(c, func(style *qui.Style) { style.Foreground = cs.Color })
		}
		syncControlStateChrome(&c.States)
	case *widgets.CheckBox:
		if cs.HasAccentColor {
			c.CheckedFillColor = cs.AccentColor
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

// applyTextStyle sets font/color/alignment on a Label.
func applyTextStyle(lbl *widgets.Label, cs *ComputedStyle) {
	qui.UpdateStyle(lbl, func(st *qui.Style) {
		st.Foreground = cs.Color
		st.Font = fontFrom(cs)
		st.Margin = cs.Margin
		st.Padding = cs.Padding
		if cs.HasBackground {
			st.Background = cs.Background
		}
	})
	lbl.Paragraph.Wrap = !cs.NoWrap
	// CSS default is overflow-wrap:normal — a word with no break opportunity
	// overflows. The native Label default is the opposite (always break), so
	// this must be set on every restyle, not only when BreakWord is on.
	lbl.Paragraph.BreakLongWords = cs.BreakWord
	lbl.Paragraph.Align = cs.TextAlign
	lbl.Paragraph.Decoration = cs.decoration()
	lbl.Paragraph.DecorationPaint = cs.decorationPaint()
	if cs.Ellipsis {
		lbl.Paragraph.Ellipsis = true
		if cs.NoWrap {
			lbl.Paragraph.MaxLines = 1
		}
	}
	if cs.LineHeight > 0 {
		lbl.Paragraph.LineHeightScale = cs.LineHeight
	}
	lbl.Paragraph.FirstIndent = cs.TextIndent
	if cs.TextTransform != "" && cs.TextTransform != "none" {
		if t := lbl.Text(); t != "" {
			lbl.SetText(transformText(t, cs.TextTransform))
		}
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
	return strings.TrimSpace(strings.Join(strings.Fields(s), " "))
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
	core := strings.Join(strings.Fields(s), " ")
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
