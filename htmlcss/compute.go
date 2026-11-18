package htmlcss

import (
	"cmp"
	"maps"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"
	"unicode"

	"github.com/qizhanchan/qui"
)

// ComputedStyle is the resolved, ready-to-render style for one element.
// It is what build.go compiles into qui.Style + a layout engine.
type ComputedStyle struct {
	HyphensAuto bool
	Language    string
	Display     string // block | inline | inline-block | flex | none
	Hidden      bool   // visibility: hidden / collapse (paints nothing, keeps box)

	// Text / font (inheritable).
	Color qui.Color
	// HasColor records whether `color` was explicitly set anywhere up the
	// inheritance chain. Form controls only override their native text
	// color when it was authored — otherwise they keep the UA black.
	HasColor bool
	// ColorOwn reports a `color` declared on this element itself (not
	// inherited). A disabled form control keeps its dimmed UA text unless
	// its own rules set a color — author rules beat the UA :disabled color.
	ColorOwn bool
	// AccentColor is CSS `accent-color` (inherited): the fill for form
	// controls' active parts — checkbox tick box, radio dot, switch track,
	// slider active track. Lets a dark stylesheet retint controls whose
	// chrome otherwise comes from the light native defaults.
	AccentColor    qui.Color
	HasAccentColor bool
	// ScrollbarThumb / ScrollbarTrack are CSS `scrollbar-color` (inherited).
	ScrollbarThumb, ScrollbarTrack qui.Color
	HasScrollbarColor              bool
	// ScrollbarNone is `scrollbar-width: none` (not inherited): the element
	// still scrolls, without a visible scrollbar.
	ScrollbarNone bool

	FontSize   float32
	FontWeight qui.FontWeight
	Italic     bool
	FontFamily string
	// FontSizeKeyword is the absolute-size keyword the font size comes
	// from (1 xx-small … 4 medium … 8 xxx-large; 0 = a length), and
	// FontMonoOnly marks a font-family of just the generic `monospace`.
	// Together they give Chrome's monospace default: a keyword size under
	// `font-family: monospace` alone resolves against 13px, not 16px.
	FontSizeKeyword int8
	FontMonoOnly    bool
	// RootFontSize is the document root's font size — what rem resolves
	// against (16px, the initial size, when the root is not <html>).
	RootFontSize float32
	LineHeight   float32 // multiplier over font size; -1 = explicit zero, 0 = normal
	// LineHeightPx is the computed line height in px when it was declared
	// with a length or percentage (0 for a unitless number). CSS inherits
	// such a line-height as that absolute length, not as a factor: a 32px
	// heading under `line-height: 20px` still gets 20px lines.
	LineHeightPx float32
	// LineHeightOwn reports a line-height declared on this element itself
	// (not inherited). Form controls use only their own: a browser's UA
	// sheet resets theirs to normal.
	LineHeightOwn bool
	Direction     string // inherited CSS inline direction (ltr or rtl)
	TextAlign     qui.TextAlign
	// Float / Clear are CSS float and clear ("" = none). A float's display
	// is blockified.
	Float string
	Clear string
	// CenterBlocks is text-align: -webkit-center (<center>, align=center):
	// besides centering inline content, block children with auto side
	// margins are centered. Inherited along with text-align.
	CenterBlocks bool
	// LetterSpacing / WordSpacing (px) are CSS tracking, inherited; they flow
	// into qui.Font so the root text engine measures + paints them.
	LetterSpacing float32
	WordSpacing   float32
	// TextIndent (px) indents the first line of a block's text (inherited).
	TextIndent float32
	// LigatureFeatures (font-variant-ligatures) and FeatureSettings
	// (font-feature-settings) are OpenType feature switches in qui.Font
	// form ("-calt", "ss01=2"), both inherited; the shaper applies them in
	// that order, so the low-level setting wins (CSS Fonts 4 §7.2).
	LigatureFeatures []string
	FeatureSettings  []string
	Underline        bool
	LineThrough      bool
	Overline         bool
	// Text-decoration line styling (CSS text-decoration-color/-style/
	// -thickness). Not inherited (each element resolves its own).
	DecorationColor     qui.Color
	HasDecorationColor  bool
	DecorationStyle     qui.DecorationLineStyle
	DecorationThickness float32 // px; 0 = auto
	NoWrap              bool    // white-space: nowrap / pre (no soft-wrapping)
	PreserveWS          bool    // white-space: pre / pre-wrap (keep spaces + newlines)
	PreserveNL          bool    // white-space: pre-line (collapse spaces, keep newlines)
	Ellipsis            bool    // text-overflow: ellipsis
	TextTransform       string  // "", uppercase, lowercase, capitalize
	// BreakWord allows breaking inside a word with no break opportunity that
	// does not fit its line: overflow-wrap: break-word | anywhere, its legacy
	// alias word-wrap, and word-break: break-all (approximated — a word that
	// would fit on the next line still moves whole rather than being cut).
	// The CSS default (normal) overflows instead, which is what the zero
	// value means.
	BreakWord bool

	// Box.
	Background    qui.Color
	HasBackground bool
	BorderColor   qui.Color
	BorderWidth   float32
	BorderStyle   qui.BorderStyle
	// HasBorder is set when the author declared border/border-width/
	// border-style at all — distinguishes `border: none` (clear a native
	// border) from "nothing declared" (keep it).
	HasBorder bool
	// Per-side borders. HasSideBorders is set when any border-<side>*
	// property is seen; SideWidths / SideColors are then seeded from the
	// uniform border and overridden per side.
	HasSideBorders        bool
	SideWidths            qui.Insets
	SideColors            qui.SideColors
	Radius                float32
	borderRadii           *borderRadiiSpec
	widthExpr, heightExpr string // nonlinear percentage math, resolved by the containing block
	// Corners holds per-corner radii (border-*-radius or a multi-value
	// border-radius). Zero means "uniform Radius applies to all corners".
	Corners qui.CornerRadii
	Padding qui.Insets
	Margin  qui.Insets
	// Outline draws a ring outside the border box; it doesn't affect layout.
	OutlineWidth float32
	OutlineColor qui.Color
	HasOutline   bool
	// OutlineNone records an explicit `outline: none` (or a zero
	// outline-width): it also suppresses a text control's native focus
	// ring, the way browsers let a stylesheet drop the default ring.
	OutlineNone bool

	// Sizing.
	Width     float32
	HasWidth  bool
	Height    float32
	HasHeight bool
	// HeightDefinite: the element's height does not depend on its content
	// (CSS 2.1 § 10.5) — its children's percentage heights resolve against
	// it. Only tracked in web-compat mode.
	HeightDefinite bool
	// HeightIntrinsicAuto keeps flex-line-dependent percentages intrinsic
	// during measurement; placement resolves them against the stretched line.
	HeightIntrinsicAuto bool
	// WidthPct/HeightPct hold `width: 50%`-style declarations as 0–1
	// fractions; the root layout engines resolve them against the
	// containing block at layout time (qui.Style.WidthPct/HeightPct).
	// The resolved size is the border box (box-sizing is ignored for
	// percentage sizes). Mutually exclusive with Width/HasWidth.
	WidthPct  float32
	HeightPct float32
	// Margin*Auto record `auto` margins per side. Horizontal auto drives
	// `margin: 0 auto` centering in flow (see qui.FlowLayout); all four
	// sides feed flex auto-margin space absorption (qui.FlexLayout).
	MarginLeftAuto   bool
	MarginRightAuto  bool
	MarginTopAuto    bool
	MarginBottomAuto bool
	MinWidth         float32
	MinHeight        float32
	MaxWidth         float32
	MaxHeight        float32
	// Min*Pct/Max*Pct hold percentage min/max sizes as 0–1 fractions,
	// resolved against the containing block at layout time (CSS
	// `max-width: 100%`). Mutually exclusive with the absolute fields.
	MinWidthPct  float32
	MinHeightPct float32
	MaxWidthPct  float32
	MaxHeightPct float32
	// BoxSizing: "" / "content-box" (CSS default — width/height are the
	// CONTENT box, padding+border add outside) or "border-box" (width/height
	// are the border box). See boxSizingExtra.
	BoxSizing string

	// Flex (when Display == flex).
	FlexDirection qui.Direction
	Justify       qui.Justify
	AlignItems    qui.AlignCross
	RowGap        float32 // row-gap (gap's first value)
	ColumnGap     float32 // column-gap (gap's second value, else its first)
	Wrap          bool
	AlignContent  qui.AlignContent // wrap-line distribution; CSS initial = stretch
	FlexGrow      float32
	// Per-item flex properties (align-self / flex-shrink / flex-basis / order).
	AlignSelf     qui.AlignCross // AlignDefault = auto (inherit container)
	FlexShrink    float32
	HasFlexShrink bool
	FlexBasis     float32
	HasFlexBasis  bool
	// FlexBasisAuto: flex-basis is EXPLICITLY auto/content (`flex: auto`,
	// `flex-basis: auto`) — a growing item then starts from its content
	// size. Left unset, a bare flex-grow keeps the engine's basis-0
	// default: scroll containers measure at the available size, so a
	// content basis would claim all of it.
	FlexBasisAuto bool
	Order         int

	// Effects (Wave 1: box-shadow / opacity / transform / gradient).
	Shadow       qui.ShadowStyle
	HasShadow    bool
	ExtraShadows []qui.ShadowStyle // additional box-shadow layers
	Opacity      float32
	HasOpacity   bool
	Transform    *transformSpec
	Gradient     *gradientSpec
	Filter       qui.ImageFilter // CSS filter (filters.go)
	// BackdropFilter is CSS backdrop-filter (same functions as filter).
	BackdropFilter qui.ImageFilter
	// TextShadows is CSS text-shadow (inherited), first layer on top.
	TextShadows []qui.ShadowStyle
	// ClipPath is CSS clip-path; Mask the first mask-image layer with its
	// longhands (masking.go).
	ClipPath *clipPathSpec
	Mask     *maskSpec
	// ListItem: the element is display: list-item (Display is then
	// "block") and gets a marker. ListStyleType is the inherited
	// list-style-type ("" = disc); Counters are the element's own
	// counter-reset / counter-set / counter-increment (counters.go).
	ListItem      bool
	ListStyleType string
	Counters      []counterOp
	// BackgroundImageURL is the url() from background-image (resolved + loaded
	// in El.applyComputed, which has the engine's BaseDir). Gradient wins.
	BackgroundImageURL string

	// Positioning + overflow (Wave 2).
	Position  string // "", static, relative, absolute, fixed
	ZIndexSet bool
	// ZIndexItem: a flex or grid item, whose z-index applies even when it
	// is not positioned (CSS Flexbox § 4.3 / Grid § 9.5).
	ZIndexItem                           bool
	AspectRatio                          float32 // CSS aspect-ratio (width / height), 0 = auto
	WidthPctOffset, HeightPctOffset      float32 // px added to WidthPct / HeightPct (calc(% ± px))
	FlexBasisPct, FlexBasisPctOffset     float32 // a percentage flex-basis (and its calc() px)   // z-index is an integer, not auto
	InlineStatic                         bool    // an inline-level box blockified by absolute/fixed (its static position is on the line)
	Top, Right, Bottom, Left             float32
	HasTop, HasRight, HasBottom, HasLeft bool
	// InsetPct holds percentage insets (top, right, bottom, left) as
	// fractions of the containing block, resolved at layout time; the
	// matching Has* is set and the px field stays 0.
	InsetPct                     [4]float32
	ZIndex                       int  // paint order among siblings (0 = default)
	OverflowClip                 bool // overflow: hidden / clip
	OverflowClipX, OverflowClipY bool // per-axis hidden / clip
	OverflowScroll               bool // overflow: auto / scroll
	OverflowScrollX              bool // horizontal scroll axis
	OverflowScrollXY             bool // both axes scroll (overflow: auto / scroll on x and y)

	// AppRegion is CSS `app-region` (Electron's `-webkit-app-region`, also
	// accepted): "drag" makes pressing the element move the OS window,
	// "no-drag" carves a child back out of a dragging ancestor. Empty means
	// "not declared" — inherit the ancestor's answer, which is how a tab
	// strip drags as a whole. Only meaningful on a window whose title bar
	// the app draws itself (qui.TitlebarOverlay).
	AppRegion string

	// Cursor is CSS `cursor`, inherited: the pointer shape over this
	// element's box. HasCursor distinguishes "declared" from "auto / not
	// declared" — only a declaration overrides a widget's built-in shape
	// (the I-beam a text field or selectable label claims), which is what
	// makes `cursor: pointer` on a card win over the text inside it.
	// Keywords qui has no native shape for degrade to CursorDefault but
	// still count as declared. See qui/cursor.go for the resolution rule.
	Cursor    qui.CursorShape
	HasCursor bool

	// NoSelect is CSS `user-select: none` (also accepted spelled
	// `-webkit-user-select`), inherited: the element's text is excluded from
	// drag-selection and the clipboard. What UI chrome — button captions,
	// toolbar labels, list markers — wants, so a stray drag doesn't turn a
	// control into selectable prose. `text` / `auto` / `all` reset it.
	NoSelect bool

	// PointerNone is CSS `pointer-events: none`, inherited: the element is
	// not a hit-test target, so clicks/hover fall through to whatever is
	// behind it (a scrim, a decorative overlay). A descendant reopens itself
	// with `pointer-events: auto`. Inheriting here and flagging every element
	// in the subtree is what lets the engine keep the rule per-widget — see
	// qui/pointer_events.go.
	PointerNone bool

	// Hover / Focus / Active are the computed style variants while the
	// element is in the corresponding interactive state — populated only
	// when the sheet has rules gated on that pseudo-class. nil means no
	// state styling for that state.
	Hover  *ComputedStyle
	Focus  *ComputedStyle
	Active *ComputedStyle
	// FocusVisible is the variant while the element holds KEYBOARD focus
	// (:focus-visible rules on top of :focus ones); nil when the sheet has
	// no :focus-visible rule.
	FocusVisible *ComputedStyle
	// FocusOptIn reports that a rule other than :focus-within changes the
	// element's focused style. :focus-within styles a container whose
	// descendant holds focus; it must not make the container itself a
	// focus target (a Tab stop in front of the field it frames).
	FocusOptIn bool
	// AncestorHover / AncestorFocus / AncestorActive are the UNION variants
	// while an ANCESTOR (or preceding sibling) is hovered / focus-within /
	// pressed — the `.row:hover .del` case, with ALL such rules for this node
	// merged. Kept separate from Hover/Focus/Active (the element's OWN state)
	// because they are triggered by different elements. They are used for
	// sensitivity detection (does any ancestor-state rule change this node?)
	// and build decisions (hasStateBoxVariant / textStateDiffers); the styles
	// actually applied at draw time are the per-trigger-set variants resolved
	// by El.ancestorStateVariant, so hovering one named ancestor never applies
	// a rule that names a different one.
	AncestorHover  *ComputedStyle
	AncestorFocus  *ComputedStyle
	AncestorActive *ComputedStyle

	// raw is the merged declaration map (post-cascade, var()-resolved) for
	// this node, kept so build.go can read rarely-used properties directly.
	raw map[string]string

	// customProps is this node's CSS custom-property table (--x → value),
	// inherited from the parent and overridden by the node's own
	// declarations. Propagated to children so var() resolves down the tree.
	customProps map[string]string
}

// isVisualBox reports whether the element carries box decorations or
// sizing that a bare text Label can't represent (Label paints no border
// and ignores width/height). Such text elements must be built as a Box
// wrapping the text so the CSS box model applies.
func (cs *ComputedStyle) isVisualBox() bool {
	return cs.BorderWidth > 0 || cs.HasSideBorders || cs.Radius > 0 || !cs.Corners.IsZero() ||
		cs.HasWidth || cs.HasHeight ||
		cs.WidthPct > 0 || cs.HeightPct > 0 ||
		cs.MinWidth > 0 || cs.MinHeight > 0 ||
		cs.MaxWidth > 0 || cs.MaxHeight > 0 ||
		cs.MinWidthPct > 0 || cs.MinHeightPct > 0 ||
		cs.MaxWidthPct > 0 || cs.MaxHeightPct > 0 ||
		cs.HasShadow || cs.Gradient != nil || cs.HasOpacity || cs.Transform != nil ||
		cs.Filter != nil || cs.BackgroundImageURL != "" ||
		cs.ClipPath != nil || cs.Mask != nil || cs.BackdropFilter != nil ||
		cs.hasStateBoxVariant()
}

// hasStateBoxVariant reports whether any interactive-state variant changes
// a painted box decoration — a text element that would otherwise fold into
// a bare Label must become a Box so the hover/focus/active swap can paint.
func (cs *ComputedStyle) hasStateBoxVariant() bool {
	d := func(v *ComputedStyle) bool { return v != nil && boxDecorationsDiffer(cs, v) }
	return d(cs.Hover) || d(cs.Focus) || d(cs.Active) || d(cs.FocusVisible) ||
		d(cs.AncestorHover) || d(cs.AncestorFocus) || d(cs.AncestorActive)
}

// textStateDiffers reports whether any interactive-state variant changes
// the TEXT appearance (color or text-decoration) relative to the resting
// style. Unlike box decorations, text color lives on the element's child
// Label / marker / icon, so El.applyStateText swaps it at draw time based
// on the box's current hover/focus/active state.
func (cs *ComputedStyle) textStateDiffers() bool {
	d := func(v *ComputedStyle) bool {
		return v != nil && (v.Color != cs.Color ||
			v.Underline != cs.Underline || v.LineThrough != cs.LineThrough)
	}
	return d(cs.Hover) || d(cs.Focus) || d(cs.Active) || d(cs.FocusVisible) ||
		d(cs.AncestorHover) || d(cs.AncestorFocus) || d(cs.AncestorActive)
}

// resolveStyles computes styles for every element under root, top-down
// so inheritance sees resolved parent values.
func resolveStyles(root *Node, sheet *Stylesheet) map[*Node]*ComputedStyle {
	out := map[*Node]*ComputedStyle{}
	var walk func(n *Node, parent *ComputedStyle)
	walk = func(n *Node, parent *ComputedStyle) {
		if n.Type == ElementNode {
			cs := computeNode(n, sheet, parent)
			out[n] = cs
			for _, c := range n.Children {
				walk(c, cs)
			}
		} else {
			for _, c := range n.Children {
				walk(c, parent)
			}
		}
	}
	walk(root, nil)
	return out
}

// matchedDecl carries a declaration with its cascade sort key.
type matchedDecl struct {
	decl    Declaration
	a, b, c int // specificity
	order   int // source order
	tier    int // 0 UA, 1 author, 2 inline; !important bumps within tier
	layer   int // author cascade layer rank (Rule.Layer; 0 = unlayered)
}

func computeNode(n *Node, sheet *Stylesheet, parent *ComputedStyle) *ComputedStyle {
	rest := &restMatches{}
	base := interpret(n, mergedDeclsRest(n, sheet, selectorState{}, rest, true, parent), parent)
	// When the sheet has interactive-state rules, compute the matching
	// variant(s) too — the built widget swaps to them while the element is
	// hovered / focused / pressed. Each variant inherits from the parent's
	// resting style, matching how the base does.
	fwHover, fwFocus, fwFocusVisible, fwActive := frameworkStateWants(n)
	if !(sheet.HasHover || fwHover || sheet.HasFocus || fwFocus || sheet.HasFocusVisible ||
		fwFocusVisible || sheet.HasActive || fwActive || sheet.HasAncestorHover ||
		sheet.HasAncestorFocus || sheet.HasAncestorActive) {
		return base
	}
	// Which state pseudos n's candidate rules use at all. A variant whose
	// state none of them mention matches exactly the resting rules, so it
	// is a copy of base rather than another cascade + interpret — most
	// elements of a sheet with a few :hover rules are in that case.
	// Framework rules are not in the mask; their fw* wants force the
	// cascade instead.
	mask := sheet.stateMask(n)
	resting := *base
	// fw is whether a framework state rule concerns n in this state; a
	// variant without one whose author state rules all match exactly as at
	// rest (`.x:hover` on a node that is not .x, an ancestor-state union
	// with no ancestor-state rule matching) is a copy of base too.
	variant := func(need, fw bool, st selectorState) *ComputedStyle {
		if !need || !fw && !stateRulesDiffer(n, sheet, st, rest) {
			cp := resting
			return &cp
		}
		return interpret(n, mergedDeclsRest(n, sheet, st, rest, false, parent), parent)
	}
	hover, focus := mask&stateHover != 0, mask&stateFocus != 0
	if sheet.HasHover || fwHover {
		// The subject's hover propagates to ancestor compounds in matching
		// (hovering an element hovers its ancestors), so `.row:hover .del`
		// also lands in .del's own Hover variant — and `.a:hover .b:hover`
		// works when the cursor is on .b.
		base.Hover = variant(hover || fwHover, fwHover, selectorState{hover: true})
	}
	if sheet.HasFocus || fwFocus {
		base.Focus = variant(focus || fwFocus, fwFocus, selectorState{focus: true})
		base.FocusOptIn = true
		if sheet.HasFocusWithin {
			// Only when a :focus-within rule exists can it be the sole
			// reason for the focus variant. With no focus rule among n's
			// candidates both cascades are the resting one.
			base.FocusOptIn = (focus || fwFocus) &&
				(fwFocus || stateRulesDiffer(n, sheet, selectorState{focus: true, noFocusWithin: true}, rest)) && !maps.Equal(
				mergedDecls(n, sheet, selectorState{focus: true, noFocusWithin: true}),
				mergedDecls(n, sheet, selectorState{}))
		}
	}
	if sheet.HasFocusVisible || fwFocusVisible {
		base.FocusVisible = variant(focus || mask&stateFocusVisible != 0 || fwFocusVisible, fwFocusVisible || fwFocus,
			selectorState{focus: true, focusVisible: true})
	}
	if sheet.HasActive || fwActive {
		// A pressed element is conventionally also hovered; both states
		// propagate to ancestor compounds in matching, keeping `.btn:hover`
		// under an `.btn:active` press and `.row:hover .btn:active` while
		// the row is hovered.
		base.Active = variant(hover || mask&stateActive != 0 || fwActive, fwActive || fwHover,
			selectorState{hover: true, active: true})
	}
	// The ancestor-state UNION variants: only NON-subject compounds are
	// treated as hovered / focused / pressed, so `.row:hover .del` lands here
	// (fires from the row) while `.del:hover` does not. Equal to base when no
	// such rule matches this node, so their delta is a no-op — see
	// hasStateBoxVariant / textStateDiffers / El.linkAncestorState.
	if sheet.HasAncestorHover {
		base.AncestorHover = variant(hover || fwHover, fwHover, selectorState{ancestorHover: true})
	}
	if sheet.HasAncestorFocus {
		// On a non-subject compound :focus-visible acts as :focus.
		base.AncestorFocus = variant(focus || mask&stateFocusVisible != 0 || fwFocus || fwFocusVisible, fwFocus || fwFocusVisible,
			selectorState{ancestorFocus: true})
	}
	if sheet.HasAncestorActive {
		base.AncestorActive = variant(mask&stateActive != 0 || fwActive, fwActive, selectorState{ancestorActive: true})
	}
	return base
}

// pseudoStyle computes the style + text for a ::before/::after pseudo-element
// of n (which = "before"/"after"), inheriting from n's own computed style.
// Returns "" content when no matching rule carries a drawable `content`.
// pseudoDecls runs the cascade for n's `which` pseudo-element; nil when
// no rule matches.
func pseudoDecls(n *Node, sheet *Stylesheet, which string) map[string]string {
	var decls []matchedDecl
	order := 0
	for _, ri := range sheet.candidateRules(n) {
		rule := &sheet.Rules[ri]
		best := -1
		var ba, bb, bc int
		for _, sel := range rule.Selectors {
			if sel.pseudoElement() != which {
				continue
			}
			// Match the base element (ignoring the pseudo-element suffix): a
			// pseudoElement selector's non-pseudo parts still constrain which
			// elements get the generated content.
			if selectorMatchesIgnoringPseudoElem(sel, n) {
				a, b, c := sel.specificity()
				if score := a*10000 + b*100 + c; score > best {
					best, ba, bb, bc = score, a, b, c
				}
			}
		}
		if best >= 0 {
			for _, d := range rule.Declarations {
				decls = append(decls, matchedDecl{decl: d, a: ba, b: bb, c: bc, order: order, tier: 1, layer: rule.Layer})
				order++
			}
		}
	}
	if len(decls) == 0 {
		return nil
	}
	sort.SliceStable(decls, func(i, j int) bool { return cascadeKey(decls[i]) < cascadeKey(decls[j]) })
	// Fold like mergedDecls: a later shorthand evicts earlier longhands.
	merged := map[string]string{}
	for _, m := range decls {
		for _, lh := range shorthandLonghands[m.decl.Property] {
			delete(merged, lh)
		}
		merged[m.decl.Property] = m.decl.Value
	}
	return merged
}

func pseudoStyle(n *Node, sheet *Stylesheet, which string, parent *ComputedStyle) (*ComputedStyle, string) {
	merged := pseudoDecls(n, sheet, which)
	if merged == nil {
		return nil, ""
	}
	content, ok := merged["content"]
	if !ok {
		return nil, "" // no content → no generated box
	}
	return interpret(n, merged, parent), unquoteContent(content, n)
}

// pseudoStyleContent is pseudoStyle returning the raw `content` value
// (has reports whether one was declared) and the style even when no
// content is — ::marker styles the default marker without one. nil style
// when no rule matches at all.
func pseudoStyleContent(n *Node, sheet *Stylesheet, which string, parent *ComputedStyle) (cs *ComputedStyle, content string, has bool) {
	merged := pseudoDecls(n, sheet, which)
	if merged == nil {
		return nil, "", false
	}
	content, has = merged["content"]
	// A pseudo-element starts as an inline box, regardless of its
	// originating element's UA display (li must not generate another marker).
	if _, ok := merged["display"]; !ok {
		merged["display"] = "inline"
	}
	return interpret(n, merged, parent), content, has
}

// UnescapeCSS resolves CSS escapes in an identifier or string body: a
// backslash and 1–6 hex digits (plus one optional whitespace) is that code
// point, an escaped newline vanishes, and any other escaped character is
// itself.
func UnescapeCSS(s string) string {
	if !strings.Contains(s, "\\") {
		return s
	}
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c != '\\' || i+1 >= len(s) {
			b.WriteByte(c)
			continue
		}
		i++
		j := i
		for j < len(s) && j-i < 6 && isHexDigit(s[j]) {
			j++
		}
		if j > i {
			cp, _ := strconv.ParseUint(s[i:j], 16, 32)
			if cp == 0 || cp > unicode.MaxRune || (cp >= 0xD800 && cp <= 0xDFFF) {
				cp = 0xFFFD
			}
			b.WriteRune(rune(cp))
			if j < len(s) && (s[j] == ' ' || s[j] == '\t' || s[j] == '\n') {
				j++
			}
			i = j - 1
			continue
		}
		if s[i] == '\n' {
			continue
		}
		b.WriteByte(s[i])
	}
	return b.String()
}

func isHexDigit(c byte) bool {
	return c >= '0' && c <= '9' || c >= 'a' && c <= 'f' || c >= 'A' && c <= 'F'
}

// unquoteContent resolves a CSS `content` value to literal text. A value is a
// concatenation of components: quoted strings ("foo"/'foo') and attr(<name>)
// references, which resolve to the element's attribute value (empty when the
// attribute is absent). none/normal → empty (no generated box). counter() and
// other unsupported functions pass through verbatim.
func unquoteContent(v string, n *Node) string { return unquoteContentWith(v, n, nil) }

// unquoteContentWith is unquoteContent resolving counter() / counters()
// through counter (nil keeps them verbatim, the counter-less behavior).
func unquoteContentWith(v string, n *Node, counter func(fn cssFunction) string) string {
	v = strings.TrimSpace(v)
	if v == "" || strings.EqualFold(v, "none") || strings.EqualFold(v, "normal") {
		return ""
	}
	var sb strings.Builder
	matched := false
	i := 0
	for i < len(v) {
		c := v[i]
		switch {
		case c == ' ' || c == '\t' || c == '\n' || c == '\r':
			i++
		case c == '"' || c == '\'':
			// A string runs to its unescaped closing quote; its escapes
			// (\a0, \f002, \") resolve to characters.
			j := i + 1
			for j < len(v) && v[j] != c {
				if v[j] == '\\' {
					j++
				}
				j++
			}
			sb.WriteString(UnescapeCSS(v[i+1 : min(j, len(v))]))
			i = j + 1
			matched = true
		default:
			// attr(<name>) — resolve against the node's attributes.
			if rest := v[i:]; len(rest) >= 5 && strings.EqualFold(rest[:5], "attr(") {
				if close := strings.IndexByte(rest, ')'); close > 0 {
					name := strings.TrimSpace(rest[5:close])
					// attr() may carry a type/fallback (attr(x, "def")); use the
					// name up to the first comma.
					if comma := strings.IndexByte(name, ','); comma >= 0 {
						name = strings.TrimSpace(name[:comma])
					}
					if val, ok := n.Attr(name); ok {
						sb.WriteString(val)
					}
					i += close + 1
					matched = true
					continue
				}
			}
			// counter(name[, style]) / counters(name, sep[, style]).
			if rest := strings.ToLower(v[i:]); counter != nil && (strings.HasPrefix(rest, "counter(") || strings.HasPrefix(rest, "counters(")) {
				if end := closingParen(v, i+strings.IndexByte(rest, '(')); end > 0 {
					if fns := splitFunctionsQuoted(v[i : end+1]); len(fns) == 1 {
						sb.WriteString(counter(fns[0]))
					}
					i = end + 1
					matched = true
					continue
				}
			}
			if rest := strings.ToLower(v[i:]); counter != nil {
				kw := rest
				if sp := strings.IndexAny(kw, " \t\n"); sp >= 0 {
					kw = kw[:sp]
				}
				switch kw {
				case "open-quote", "close-quote", "no-open-quote", "no-close-quote":
					sb.WriteString(map[string]string{"open-quote": "\u201c", "close-quote": "\u201d"}[kw])
					i += len(kw)
					matched = true
					continue
				}
			}
			// Unrecognized token (e.g. counter(...)) — return verbatim, matching
			// the previous pass-through behavior for non-string content.
			if !matched {
				return v
			}
			i++
		}
	}
	return sb.String()
}

// mergedDecls runs the cascade for node n and returns the winning
// property→value map. st selects which interactive-state pseudos apply.
func mergedDecls(n *Node, sheet *Stylesheet, st selectorState) map[string]string {
	return mergedDeclsRest(n, sheet, st, nil, false)
}

// restMatches records, per candidate rule of a node, the best matching
// specificity at rest (-1 = no match), so the node's state variants can
// skip re-matching rules that use no state pseudo.
type restMatches struct{ spec []int32 }

// stateRulesDiffer reports whether any of n's candidate author rules that
// use a state pseudo matches differently under st than it did at rest
// (rest, recorded by the resting cascade). When none does, the author
// cascade under st is the resting one.
func stateRulesDiffer(n *Node, sheet *Stylesheet, st selectorState, rest *restMatches) bool {
	cands := sheet.candidateRules(n)
	if rest == nil || len(rest.spec) != len(cands) {
		return true
	}
	states := sheet.ruleIndex().states
	for k, ri := range cands {
		if states[ri] == 0 {
			continue
		}
		best := -1
		for _, sel := range sheet.Rules[ri].Selectors {
			if sel.matches(n, st) {
				a, b, c := sel.specificity()
				best = max(best, min(a, 99)*10000+min(b, 99)*100+min(c, 99))
			}
		}
		if int32(best) != rest.spec[k] {
			return true
		}
	}
	return false
}

// mergedDeclsRest is mergedDecls recording (record) or reusing rest.
func mergedDeclsRest(n *Node, sheet *Stylesheet, st selectorState, rest *restMatches, record bool, parents ...*ComputedStyle) map[string]string {
	// The matched declarations are scratch: reuse a buffer across calls
	// (growing it per element was a fifth of a heavy page's cascade).
	bufp := declBufPool.Get().(*[]matchedDecl)
	decls := (*bufp)[:0]
	defer func() {
		clear(decls) // drop the strings it references
		*bufp = decls[:0]
		declBufPool.Put(bufp)
	}()

	// Tier 0: user-agent defaults for this tag. Push-button input types
	// (submit/reset/button) borrow the <button> default chrome.
	uaTag := uaTagFor(n)
	// The global `hidden` attribute is a UA rule (`[hidden] { display: none }`),
	// not an intrinsic property — so author CSS can override it, exactly as in
	// a browser. Emitting it at tier 0 gets that for free.
	// So is `input[type=hidden i] { display: none !important }`.
	if t, ok := n.Attr("type"); ok && n.Tag == "input" && strings.EqualFold(strings.TrimSpace(t), "hidden") {
		decls = append(decls, matchedDecl{
			decl: Declaration{Property: "display", Value: "none", Important: true}, tier: 0,
		})
	}
	// `audio:not([controls]) { display: none !important }` (HTML § 15.4.1).
	if n.Tag == "audio" {
		if _, c := n.Attr("controls"); !c {
			decls = append(decls, matchedDecl{
				decl: Declaration{Property: "display", Value: "none", Important: true}, tier: 0,
			})
		}
	}
	if _, hidden := n.Attr("hidden"); hidden {
		decls = append(decls, matchedDecl{
			decl: Declaration{Property: "display", Value: "none"}, tier: 0,
		})
	}
	for _, d := range uaDeclarations(uaTag) {
		// The UA sheet keys off the tag alone, but a bare <a> with no href is
		// not a link: browsers scope both the link color and `cursor: pointer`
		// to :any-link. Drop those two for an anchor placeholder.
		if n.Tag == "a" && (d.Property == "cursor" || d.Property == "color") {
			if _, ok := n.Attr("href"); !ok {
				continue
			}
		}
		decls = append(decls, matchedDecl{decl: d, tier: 0})
	}
	// Still tier 0: framework component defaults (RegisterFrameworkCSS).
	decls = append(decls, frameworkDecls(n, st)...)
	// A closed <details> shows only its summary (HTML § 15.5.4).
	if hiddenByClosedDetails(n) {
		decls = append(decls, matchedDecl{decl: Declaration{Property: "display", Value: "none"}, tier: 0})
	}
	// Presentational hints (legacy HTML attributes) follow the UA sheet.
	for _, d := range presentationalHints(n) {
		decls = append(decls, matchedDecl{decl: d, tier: 0})
	}

	// Tier 1: author rules that match (only the candidates for n can).
	// A state variant re-matches only the rules that use a state pseudo:
	// any other rule matches exactly as it did at rest (rest, recorded by
	// the resting cascade of the same node).
	order := 0
	cands := sheet.candidateRules(n)
	var states []uint8
	reuse := rest != nil && !record && len(rest.spec) == len(cands)
	if reuse {
		states = sheet.ruleIndex().states
	}
	if record && rest != nil {
		rest.spec = make([]int32, len(cands))
	}
	for k, ri := range cands {
		rule := &sheet.Rules[ri]
		best := -1
		var ba, bb, bc int
		if reuse && states[ri] == 0 {
			if sp := rest.spec[k]; sp >= 0 {
				best = int(sp)
				ba, bb, bc = best/10000, best/100%100, best%100
			}
		} else {
			for _, sel := range rule.Selectors {
				if sel.matches(n, st) {
					a, b, c := sel.specificity()
					score := min(a, 99)*10000 + min(b, 99)*100 + min(c, 99)
					if score > best {
						best, ba, bb, bc = score, a, b, c
					}
				}
			}
			if record && rest != nil {
				rest.spec[k] = int32(best)
			}
		}
		if best >= 0 {
			for _, d := range rule.Declarations {
				decls = append(decls, matchedDecl{decl: d, a: ba, b: bb, c: bc, order: order, tier: 1, layer: rule.Layer})
				order++
			}
		}
	}

	// Tier 2: inline style attribute (highest).
	if inline, ok := n.Attr("style"); ok {
		for _, d := range parseDeclarations(inline) {
			decls = append(decls, matchedDecl{decl: d, tier: 2})
		}
	}

	// Cascade sort: !important tier first, then origin tier, then
	// specificity, then source order. Stable so equal keys keep order.
	slices.SortStableFunc(decls, func(a, b matchedDecl) int {
		return cmp.Compare(cascadeKey(a), cascadeKey(b))
	})

	// Fold in cascade order. A shorthand evicts every longhand (and
	// sub-shorthand) of its family that an EARLIER declaration set, so a
	// longhand surviving next to its shorthand is known to come later —
	// which is what lets the interpreter always prefer the longhand.
	direction := "ltr"
	if len(parents) > 0 && parents[0] != nil && parents[0].Direction != "" {
		direction = parents[0].Direction
	}
	if dir, ok := n.Attr("dir"); ok && (dir == "rtl" || dir == "ltr") {
		direction = dir
	}
	for _, d := range decls {
		if d.decl.Property == "direction" && (d.decl.Value == "rtl" || d.decl.Value == "ltr") {
			direction = d.decl.Value
		}
	}
	merged := map[string]string{}
	for _, m := range decls {
		for _, lh := range shorthandLonghands[m.decl.Property] {
			delete(merged, lh)
		}
		merged[m.decl.Property] = m.decl.Value
		mergeLogicalBox(merged, m.decl, direction == "rtl")
	}
	return merged
}

// declBufPool holds mergedDeclsRest's scratch declaration lists.
var declBufPool = sync.Pool{New: func() any {
	b := make([]matchedDecl, 0, 128)
	return &b
}}

func cascadeKey(m matchedDecl) int {
	tier := m.tier
	if m.decl.Important {
		tier += 10 // important beats any normal tier
	}
	// Cascade layers (CSS Cascade 5): unlayered author rules beat every
	// layer, a later layer an earlier one — before specificity. (For
	// !important the order flips; that refinement is not modeled.)
	layer := 1023
	if m.layer > 0 && m.layer < 1023 {
		layer = m.layer
	}
	spec := min(m.a, 99)*10000 + min(m.b, 99)*100 + min(m.c, 99)
	return tier*10_000_000_000 + layer*1_000_000 + spec // order handled by stable sort
}

// interpret turns the merged declaration map into a ComputedStyle,
// inheriting text/font properties from parent.
func interpret(n *Node, m map[string]string, parent *ComputedStyle) *ComputedStyle {
	rootFS := float32(16)
	if parent != nil && parent.RootFontSize > 0 {
		rootFS = parent.RootFontSize
	}
	remPx = rootFS
	// Resolve CSS custom properties + var() before reading any property:
	// build this node's inherited custom-prop table, then substitute var()
	// throughout the normal declarations.
	customProps := buildCustomProps(parent, m)
	m = resolveMapVars(m, customProps)
	inheritRegisteredProps(m, parent)
	colorInherited := m["color"] == "inherit" || m["color"] == "unset"
	lineHeightInherited := m["line-height"] == "inherit" || m["line-height"] == "unset" || m["font"] == "inherit"
	resolveCSSWideKeywords(m, parent)
	expandShorthands(m)
	cs := &ComputedStyle{raw: m, customProps: customProps, Direction: "ltr"}
	if parent != nil && parent.Direction != "" {
		cs.Direction = parent.Direction
	}
	if dir, ok := n.Attr("dir"); ok && (dir == "rtl" || dir == "ltr") {
		cs.Direction = dir
	}
	if dir := m["direction"]; dir == "rtl" || dir == "ltr" {
		cs.Direction = dir
	}

	// --- inheritable defaults ---
	if parent != nil {
		cs.Color = parent.Color
		cs.HasColor = parent.HasColor
		cs.FontSize = parent.FontSize
		cs.FontWeight = parent.FontWeight
		cs.Italic = parent.Italic
		cs.FontFamily = parent.FontFamily
		cs.FontSizeKeyword = parent.FontSizeKeyword
		cs.FontMonoOnly = parent.FontMonoOnly
		cs.LineHeight = parent.LineHeight
		cs.LineHeightPx = parent.LineHeightPx
		cs.TextAlign = parent.TextAlign
		cs.CenterBlocks = parent.CenterBlocks
		cs.LetterSpacing = parent.LetterSpacing
		cs.WordSpacing = parent.WordSpacing
		cs.TextIndent = parent.TextIndent
		cs.LigatureFeatures = parent.LigatureFeatures
		cs.FeatureSettings = parent.FeatureSettings
		cs.AccentColor = parent.AccentColor
		cs.HasAccentColor = parent.HasAccentColor
		cs.ScrollbarThumb = parent.ScrollbarThumb
		cs.ScrollbarTrack = parent.ScrollbarTrack
		cs.HasScrollbarColor = parent.HasScrollbarColor
		// white-space is inherited (CSS): a <b> inside a pre keeps
		// preserving spaces unless it declares its own value.
		cs.HyphensAuto = parent.HyphensAuto
		cs.Language = parent.Language
		cs.NoWrap = parent.NoWrap
		cs.PreserveWS = parent.PreserveWS
		cs.PreserveNL = parent.PreserveNL
		// overflow-wrap / word-break are inherited too.
		cs.BreakWord = parent.BreakWord
		// app-region inherits so a drag region covers its whole subtree.
		cs.AppRegion = parent.AppRegion
		// cursor inherits (CSS): a `pointer` card keeps the hand over every
		// label, icon and span inside it.
		cs.Cursor = parent.Cursor
		cs.HasCursor = parent.HasCursor
		// user-select inherits (CSS): marking a toolbar unselectable covers
		// every caption inside it.
		cs.NoSelect = parent.NoSelect
		// pointer-events inherits (CSS): a scrim's children are click-through
		// too, unless one says `auto`.
		cs.PointerNone = parent.PointerNone
		// text-shadow and list-style-type inherit (CSS).
		cs.TextShadows = parent.TextShadows
		cs.ListStyleType = parent.ListStyleType
	} else {
		cs.Color = qui.Color{A: 1} // CSS initial (canvastext): black
		cs.FontSize = 16
		cs.FontSizeKeyword = 4 // initial: medium
		cs.FontWeight = qui.FontWeightNormal
		cs.LineHeight = 0 // CSS initial `normal`: the face's own line height, as in a browser
		cs.TextAlign = qui.TextAlignStart
	}

	if v, ok := n.Attrs["lang"]; ok {
		cs.Language = v
	}
	if v, ok := m["hyphens"]; ok {
		cs.HyphensAuto = strings.TrimSpace(v) == "auto"
	} else if v, ok := m["-webkit-hyphens"]; ok {
		cs.HyphensAuto = strings.TrimSpace(v) == "auto"
	}
	// --- display ---
	cs.Display = strings.ToLower(strings.TrimSpace(m["display"]))
	if cs.Display == "" {
		cs.Display = defaultDisplay(n.Tag)
		// <li> is display: list-item in the UA sheet; the rest of the
		// engine lays it out as the block it is, ListItem adds the marker.
		cs.ListItem = n.Tag == "li"
	} else if isListItemDisplay(cs.Display) {
		cs.Display, cs.ListItem = "block", true
	}
	// float (CSS 2.1 § 9.7): an absolutely positioned box does not float;
	// a float's display is blockified.
	cs.Float, cs.Clear = "", ""
	if v := strings.ToLower(strings.TrimSpace(m["float"])); v == "left" || v == "right" ||
		v == "inline-start" || v == "inline-end" {
		pos := strings.ToLower(strings.TrimSpace(m["position"]))
		if pos != "absolute" && pos != "fixed" && cs.Display != "none" {
			cs.Float = map[string]string{"left": "left", "inline-start": "left", "right": "right", "inline-end": "right"}[v]
			cs.Display = blockify(cs.Display)
		}
	}
	if v := strings.ToLower(strings.TrimSpace(m["clear"])); v == "left" || v == "right" || v == "both" ||
		v == "inline-start" || v == "inline-end" {
		cs.Clear = map[string]string{"left": "left", "inline-start": "left", "right": "right", "inline-end": "right", "both": "both"}[v]
	}
	// app-region: drag / no-drag. Inherited (Electron's rule): declaring
	// drag on a title-bar strip makes everything in it move the window, and
	// interactive children opt back out with no-drag.
	if v, ok := m["app-region"]; ok {
		cs.AppRegion = normalizeAppRegion(v)
	} else if v, ok := m["-webkit-app-region"]; ok {
		cs.AppRegion = normalizeAppRegion(v)
	}
	// cursor: <keyword>. `auto` (and an unknown value) leaves the inherited
	// answer alone — for the root that means "not declared", so widgets keep
	// their built-in shapes.
	if v, ok := m["cursor"]; ok {
		if shape, declared, reset := parseCursor(v); reset {
			cs.Cursor, cs.HasCursor = qui.CursorDefault, false
		} else if declared {
			cs.Cursor, cs.HasCursor = shape, true
		}
	}
	// user-select: none excludes the element's text from drag-selection;
	// text / auto / all put it back (an inherited `none` is overridable).
	// `contain` and `all` have no distinct engine behavior — both mean
	// "selectable" here.
	// pointer-events: none / auto. The SVG-only values (visiblePainted,
	// stroke, fill, …) all mean "hittable" for our purposes.
	if v, ok := m["pointer-events"]; ok {
		switch strings.ToLower(strings.TrimSpace(v)) {
		case "none":
			cs.PointerNone = true
		case "":
			// empty declaration — ignore
		default:
			cs.PointerNone = false
		}
	}
	userSelect, ok := m["user-select"]
	if !ok {
		userSelect, ok = m["-webkit-user-select"]
	}
	if ok {
		switch strings.ToLower(strings.TrimSpace(userSelect)) {
		case "none":
			cs.NoSelect = true
		case "text", "auto", "all", "contain", "initial", "unset", "revert":
			cs.NoSelect = false
		}
	}
	// visibility: hidden / collapse hide paint while keeping layout space.
	if v, ok := m["visibility"]; ok {
		switch strings.ToLower(strings.TrimSpace(v)) {
		case "hidden", "collapse":
			cs.Hidden = true
		}
	}

	// --- font-size (before em-relative lengths) ---
	if v, ok := m["font-family"]; ok {
		cs.FontMonoOnly = strings.EqualFold(strings.Trim(strings.TrimSpace(v), `'"`), "monospace")
	}
	if v, ok := m["font-size"]; ok {
		kw := strings.ToLower(strings.TrimSpace(v))
		if k := fontSizeKeywordIndex(kw); k > 0 {
			cs.FontSizeKeyword = k
		} else if kw == "smaller" || kw == "larger" {
			f := float32(1.2)
			if kw == "smaller" {
				f = 1 / f
			}
			cs.FontSize *= f
			cs.FontSizeKeyword = 0
		} else if px, ok := parseLength(v, cs.FontSize, cs.FontSize); ok {
			cs.FontSize = px
			cs.FontSizeKeyword = 0
		}
	}
	if cs.FontSizeKeyword > 0 {
		cs.FontSize = fontSizeForKeyword(cs.FontSizeKeyword, webCompat && cs.FontMonoOnly)
	}
	if parent == nil && n.Tag == "html" {
		// The root's own font size (its rem is the initial 16px) is
		// every rem below it — and in its other properties.
		rootFS = cs.FontSize
		remPx = rootFS
	}
	cs.RootFontSize = rootFS
	fs := cs.FontSize

	// --- color ---
	cs.ColorOwn = colorInherited
	if v, ok := m["color"]; ok {
		if c, ok := parseColor(v); ok {
			cs.Color, cs.HasColor = c, true
			cs.ColorOwn = true
		}
	}
	// currentColor in any other property is the used `color` (CSS Color 4).
	m = resolveCurrentColor(m, cs.Color)
	cs.raw = m
	// --- accent-color (inherited; "auto" falls back to the native default) ---
	if v, ok := m["accent-color"]; ok {
		if strings.EqualFold(strings.TrimSpace(v), "auto") {
			cs.HasAccentColor = false
		} else if c, ok := parseColor(v); ok {
			cs.AccentColor, cs.HasAccentColor = c, true
		}
	}
	// --- scrollbar-width (not inherited): only `none` changes anything;
	// `thin` / `auto` keep the one bar size qui has ---
	if v, ok := m["scrollbar-width"]; ok {
		cs.ScrollbarNone = strings.EqualFold(strings.TrimSpace(v), "none")
	}
	// --- scrollbar-color (inherited): "<thumb> <track>" or auto ---
	if v, ok := m["scrollbar-color"]; ok {
		parts := splitColorList(v)
		switch {
		case strings.EqualFold(strings.TrimSpace(v), "auto"):
			cs.HasScrollbarColor = false
		case len(parts) == 2:
			thumb, ok1 := parseColor(parts[0])
			track, ok2 := parseColor(parts[1])
			if ok1 && ok2 {
				cs.ScrollbarThumb, cs.ScrollbarTrack, cs.HasScrollbarColor = thumb, track, true
			}
		}
	}
	// --- font-weight / style / family / line-height / text-align ---
	if v, ok := m["font-weight"]; ok {
		cs.FontWeight = parseFontWeight(v, cs.FontWeight)
	}
	if v, ok := m["font-style"]; ok {
		cs.Italic = strings.Contains(strings.ToLower(v), "italic")
	}
	if v, ok := m["font-family"]; ok {
		cs.FontFamily = firstFontFamily(v)
	}
	cs.LineHeightOwn = lineHeightInherited
	if cs.LineHeightPx > 0 && fs > 0 {
		// Inherited as a length: re-express it over this element's font.
		cs.LineHeight = cs.LineHeightPx / fs
	}
	if v, ok := m["line-height"]; ok {
		if strings.EqualFold(strings.TrimSpace(v), "normal") {
			cs.LineHeight = 0 // the face's own line height (text layouts read <=0 as normal)
			cs.LineHeightPx = 0
			cs.LineHeightOwn = true
		} else if lh, ok := parseFloat(strings.TrimSpace(v)); ok && !strings.ContainsAny(v, "px%rem") {
			cs.LineHeight = lh // unitless multiplier
			if lh == 0 {
				cs.LineHeight = -1
			}
			cs.LineHeightPx = 0
			cs.LineHeightOwn = true
		} else if px, ok := parseLength(v, fs, fs); ok && fs > 0 {
			cs.LineHeight = px / fs
			if px == 0 {
				cs.LineHeight = -1
			}
			cs.LineHeightPx = px
			cs.LineHeightOwn = true
		}
	}
	if v, ok := m["text-align"]; ok {
		cs.TextAlign = parseTextAlign(v)
		switch strings.ToLower(strings.TrimSpace(v)) {
		case "-webkit-center", "-moz-center", "-khtml-center":
			cs.CenterBlocks = true
		default:
			cs.CenterBlocks = false
		}
	}
	// letter-spacing / word-spacing: `normal` resets to 0, else a length.
	if v, ok := m["letter-spacing"]; ok {
		if strings.EqualFold(strings.TrimSpace(v), "normal") {
			cs.LetterSpacing = 0
		} else if px, ok := parseLength(v, fs, fs); ok {
			cs.LetterSpacing = px
		}
	}
	if v, ok := m["word-spacing"]; ok {
		if strings.EqualFold(strings.TrimSpace(v), "normal") {
			cs.WordSpacing = 0
		} else if px, ok := parseLength(v, fs, fs); ok {
			cs.WordSpacing = px
		}
	}
	if v, ok := m["font-variant-ligatures"]; ok {
		if f, ok := parseVariantLigatures(v); ok {
			cs.LigatureFeatures = f
		}
	}
	if v, ok := m["font-feature-settings"]; ok {
		if f, ok := parseFeatureSettings(v); ok {
			cs.FeatureSettings = f
		}
	}
	if v, ok := m["text-indent"]; ok {
		if px, ok := parseLength(v, fs, fs); ok {
			cs.TextIndent = px
		}
	}
	if v, ok := decorationValue(m); ok {
		lv := strings.ToLower(v)
		if strings.Contains(lv, "none") {
			cs.Underline, cs.LineThrough, cs.Overline = false, false, false
		} else {
			cs.Underline = strings.Contains(lv, "underline")
			cs.LineThrough = strings.Contains(lv, "line-through")
			cs.Overline = strings.Contains(lv, "overline")
		}
		// The `text-decoration` shorthand may also carry a style keyword, a
		// color, and a thickness — parse them out of whichever value we used.
		parseDecorationExtras(cs, v, fs)
	}
	// Longhands win over anything the shorthand set.
	if v, ok := m["text-decoration-style"]; ok {
		cs.DecorationStyle = parseDecorationStyle(strings.ToLower(strings.TrimSpace(v)))
	}
	if v, ok := m["text-decoration-color"]; ok {
		if c, ok := parseColor(strings.TrimSpace(v)); ok {
			cs.DecorationColor, cs.HasDecorationColor = c, true
		}
	}
	if v, ok := m["text-decoration-thickness"]; ok {
		lv := strings.ToLower(strings.TrimSpace(v))
		if lv != "auto" && lv != "from-font" {
			if px, ok := parseLength(v, fs, fs); ok {
				cs.DecorationThickness = px
			}
		}
	}
	if v, ok := m["white-space"]; ok {
		// An explicit value replaces the inherited one wholesale.
		cs.NoWrap, cs.PreserveWS, cs.PreserveNL = false, false, false
		switch strings.ToLower(strings.TrimSpace(v)) {
		case "nowrap":
			cs.NoWrap = true
		case "pre":
			cs.NoWrap, cs.PreserveWS = true, true
		case "pre-wrap":
			cs.PreserveWS = true // preserve whitespace but still soft-wrap
		case "pre-line":
			cs.PreserveNL = true // collapse spaces, keep explicit newlines
		}
	}
	// overflow-wrap (word-wrap is the legacy alias) and word-break both feed
	// the same engine switch; a later declaration wins, and `normal` resets.
	for _, prop := range []string{"word-wrap", "overflow-wrap", "word-break"} {
		v, ok := m[prop]
		if !ok {
			continue
		}
		switch strings.ToLower(strings.TrimSpace(v)) {
		case "break-word", "anywhere", "break-all":
			cs.BreakWord = true
		case "normal", "keep-all":
			cs.BreakWord = false
		}
	}
	if v, ok := m["text-overflow"]; ok {
		cs.Ellipsis = strings.Contains(strings.ToLower(v), "ellipsis")
	}
	if v, ok := m["text-transform"]; ok {
		cs.TextTransform = strings.ToLower(strings.TrimSpace(v))
	}

	// --- background ---
	// The shorthand first, then the longhand: when both survive the
	// cascade the longhand was declared later (mergedDecls), so it wins.
	if v, ok := m["background"]; ok {
		// Simplification: treat `background` as a color if it parses as one. Try the whole
		// value first (so functional colors like rgb()/hsl() with internal
		// spaces/commas survive), then fall back to the first field.
		if c, ok := parseColor(strings.TrimSpace(v)); ok {
			cs.Background, cs.HasBackground = c, true
		} else if fields := strings.Fields(v); len(fields) > 0 {
			if c, ok := parseColor(fields[0]); ok {
				cs.Background, cs.HasBackground = c, true
			}
		}
	}
	if v, ok := m["background-color"]; ok {
		if c, ok := parseColor(v); ok {
			cs.Background, cs.HasBackground = c, true
		}
	}
	// linear/radial-gradient or url() in background / background-image.
	if v, ok := m["background-image"]; ok {
		if g, ok := parseGradient(v); ok {
			cs.Gradient = g
		} else if u, ok := parseCSSURL(v); ok {
			cs.BackgroundImageURL = u
		}
	}
	if cs.Gradient == nil {
		if v, ok := m["background"]; ok && cs.BackgroundImageURL == "" {
			// The first background layer is painted on top. In particular,
			// a url() in the shorthand must not be lost to later layers.
			v = strings.TrimSpace(splitTopLevel(v, ',')[0])
			if g, ok := parseGradient(v); ok {
				cs.Gradient = g
			} else if u, ok := parseCSSURL(v); ok {
				cs.BackgroundImageURL = u
			}
		}
	}

	// --- effects: box-shadow / opacity / transform ---
	if v, ok := m["box-shadow"]; ok {
		if layers := parseBoxShadows(v, fs); len(layers) > 0 {
			cs.Shadow, cs.HasShadow = layers[0], true
			if len(layers) > 1 {
				cs.ExtraShadows = layers[1:]
			}
		}
	}
	if v, ok := m["opacity"]; ok {
		if o, ok := parseOpacity(v); ok {
			cs.Opacity, cs.HasOpacity = o, true
		}
	}
	if v, ok := m["transform"]; ok {
		if t, ok := parseTransform(v, fs); ok {
			cs.Transform = t
		}
	}
	if pre := individualTransformOps(m, fs); len(pre) > 0 {
		if cs.Transform == nil {
			cs.Transform = &transformSpec{sx: 1, sy: 1}
		}
		cs.Transform.ops = append(pre, cs.Transform.ops...)
	}
	if v, ok := m["filter"]; ok {
		if f, ok := parseFilter(v, fs); ok {
			cs.Filter = f
		}
	}
	if v, ok := m["backdrop-filter"]; ok {
		cs.BackdropFilter, _ = parseFilter(v, fs)
	} else if v, ok := m["-webkit-backdrop-filter"]; ok {
		cs.BackdropFilter, _ = parseFilter(v, fs)
	}
	if v, ok := m["text-shadow"]; ok {
		cs.TextShadows = parseTextShadows(v, fs, cs.Color)
	}
	if v, ok := m["clip-path"]; ok {
		cs.ClipPath, _ = parseClipPath(v, fs)
	} else if v, ok := m["-webkit-clip-path"]; ok {
		cs.ClipPath, _ = parseClipPath(v, fs)
	}
	cs.Mask, _ = parseMask(m, fs)
	parseListAndCounters(n, m, cs)
	if v, ok := m["transform-origin"]; ok && cs.Transform != nil {
		if ox, oy, ok := parseTransformOrigin(v); ok {
			cs.Transform.ox, cs.Transform.oy, cs.Transform.hasOrigin = ox, oy, true
		}
	}

	// --- positioning + overflow ---
	cs.Position = strings.ToLower(strings.TrimSpace(m["position"]))
	// The legacy `clip: rect()` (absolutely positioned boxes only) is a
	// clip-path inset; `clip: rect(0 0 0 0)` is how sites hide skip links.
	if cs.ClipPath == nil && (cs.Position == "absolute" || cs.Position == "fixed") {
		if v, ok := m["clip"]; ok {
			cs.ClipPath = parseClipRect(v, fs)
		}
	}
	if v, ok := m["z-index"]; ok {
		if n, err := strconv.Atoi(strings.TrimSpace(v)); err == nil {
			cs.ZIndex, cs.ZIndexSet = n, true
		}
	}
	cs.ZIndexItem = parent != nil && (isFlexDisplay(parent.Display) || isGridDisplay(parent.Display))
	if v, ok := m["top"]; ok {
		if pct, ok := parsePercentFraction(v); ok {
			cs.InsetPct[0], cs.HasTop = pct, true
		} else if px, ok := parseLength(v, fs, 0); ok {
			cs.Top, cs.HasTop = px, true
		}
	}
	if v, ok := m["right"]; ok {
		if pct, ok := parsePercentFraction(v); ok {
			cs.InsetPct[1], cs.HasRight = pct, true
		} else if px, ok := parseLength(v, fs, 0); ok {
			cs.Right, cs.HasRight = px, true
		}
	}
	if v, ok := m["bottom"]; ok {
		if pct, ok := parsePercentFraction(v); ok {
			cs.InsetPct[2], cs.HasBottom = pct, true
		} else if px, ok := parseLength(v, fs, 0); ok {
			cs.Bottom, cs.HasBottom = px, true
		}
	}
	if v, ok := m["left"]; ok {
		if pct, ok := parsePercentFraction(v); ok {
			cs.InsetPct[3], cs.HasLeft = pct, true
		} else if px, ok := parseLength(v, fs, 0); ok {
			cs.Left, cs.HasLeft = px, true
		}
	}
	parseOverflow(m, cs)
	// CSS blockifies out-of-flow (absolute/fixed) elements — an inline <span>
	// with position:absolute lays out as a block. This also keeps it out of
	// inline runs (buildFlow), so it stays a standalone positioned widget.
	if (cs.Position == "absolute" || cs.Position == "fixed") && strings.HasPrefix(cs.Display, "inline") {
		cs.Display = "block"
		cs.InlineStatic = true
	}

	// --- border ---
	parseBorder(m, cs, fs)
	parseBorderRadius(m, cs, fs)

	// --- outline (ring outside the border box; no layout effect) ---
	parseOutline(m, cs, fs)

	// --- box spacing ---
	cs.Padding = parseBox(m, "padding", fs)
	cs.Margin = parseBox(m, "margin", fs)
	cs.MarginTopAuto, cs.MarginRightAuto, cs.MarginBottomAuto, cs.MarginLeftAuto = marginAutoFlags(m)
	// Blink's StyleAdjuster: a table never keeps an inherited -webkit-*
	// alignment — it resets to start (its own margins still follow the
	// parent's -webkit-center below).
	if n.Tag == "table" && cs.CenterBlocks {
		if _, own := m["text-align"]; !own {
			cs.CenterBlocks = false
			cs.TextAlign = qui.TextAlignStart
		}
	}
	// -webkit-center on the parent centers a block child whose side
	// margins the author left alone.
	if parent != nil && parent.CenterBlocks && !isInlineLevelDisplay(cs.Display) {
		if _, l := m["margin-left"]; !l {
			if _, r := m["margin-right"]; !r {
				if _, sh := m["margin"]; !sh {
					cs.MarginLeftAuto, cs.MarginRightAuto = true, true
				}
			}
		}
	}

	// --- sizing ---
	cs.BoxSizing = strings.ToLower(strings.TrimSpace(m["box-sizing"]))
	if v, ok := m["width"]; ok {
		if strings.Contains(v, "%") && (strings.Contains(v, "min(") || strings.Contains(v, "max(") || strings.Contains(v, "clamp(")) {
			cs.widthExpr = v
		} else if pct, ok := parsePercentFraction(v); ok {
			cs.WidthPct = pct
		} else if px, frac, hasPct, ok := parseLengthPct(v, fs); ok && hasPct && frac > 0 {
			cs.WidthPct, cs.WidthPctOffset = frac, px // calc(100% - 2rem)
		} else if px, ok := parseLength(v, fs, 0); ok {
			cs.Width, cs.HasWidth = px, true
		}
	}
	if v, ok := m["height"]; ok {
		if strings.Contains(v, "%") && (strings.Contains(v, "min(") || strings.Contains(v, "max(") || strings.Contains(v, "clamp(")) {
			cs.heightExpr = v
		} else if pct, ok := parsePercentFraction(v); ok {
			cs.HeightPct = pct
		} else if px, frac, hasPct, ok := parseLengthPct(v, fs); ok && hasPct && frac > 0 {
			cs.HeightPct, cs.HeightPctOffset = frac, px
		} else if px, ok := parseLength(v, fs, 0); ok {
			cs.Height, cs.HasHeight = px, true
		}
	}
	if v, ok := m["min-width"]; ok {
		if pct, ok := parsePercentFraction(v); ok {
			cs.MinWidthPct = pct
		} else if px, ok := parseLength(v, fs, 0); ok {
			cs.MinWidth = px
		}
	}
	if v, ok := m["min-height"]; ok {
		if pct, ok := parsePercentFraction(v); ok {
			cs.MinHeightPct = pct
		} else if px, ok := parseLength(v, fs, 0); ok {
			cs.MinHeight = px
		}
	}
	if v, ok := m["max-width"]; ok {
		if pct, ok := parsePercentFraction(v); ok {
			cs.MaxWidthPct = pct
		} else if px, ok := parseLength(v, fs, 0); ok {
			cs.MaxWidth = px
		}
	}
	if v, ok := m["max-height"]; ok {
		if pct, ok := parsePercentFraction(v); ok {
			cs.MaxHeightPct = pct
		} else if px, ok := parseLength(v, fs, 0); ok {
			cs.MaxHeight = px
		}
	}

	cs.AspectRatio = 0
	if v, ok := m["aspect-ratio"]; ok {
		cs.AspectRatio = parseAspectRatio(v)
	}

	// --- flex ---
	cs.FlexDirection = qui.Horizontal
	cs.AlignItems = qui.AlignStretch
	if v, ok := m["flex-direction"]; ok && strings.Contains(v, "column") {
		cs.FlexDirection = qui.Vertical
	}
	if v, ok := m["justify-content"]; ok {
		cs.Justify = parseJustify(v)
	}
	if v, ok := m["align-items"]; ok {
		cs.AlignItems = parseAlign(v)
	}
	cs.RowGap, cs.ColumnGap = gridGaps(cs)
	if v, ok := m["flex-wrap"]; ok && strings.Contains(v, "wrap") {
		cs.Wrap = true
	}
	cs.AlignContent = qui.AlignContentStretch // CSS initial (normal → stretch)
	if v, ok := m["align-content"]; ok {
		cs.AlignContent = parseAlignContent(v)
	}
	if v, ok := m["flex-grow"]; ok {
		if g, ok := parseFloat(v); ok {
			cs.FlexGrow = g
		}
	}
	if v, ok := m["align-self"]; ok {
		if s := strings.ToLower(strings.TrimSpace(v)); s != "" && s != "auto" {
			cs.AlignSelf = parseAlign(s)
		}
	}
	if v, ok := m["flex-shrink"]; ok {
		if s, ok := parseFloat(v); ok {
			cs.FlexShrink, cs.HasFlexShrink = s, true
		}
	}
	cs.FlexBasisAuto = webCompat // CSS initial flex-basis is auto, even with flex-grow.
	if v, ok := m["flex-basis"]; ok {
		if s := strings.ToLower(strings.TrimSpace(v)); s == "auto" || s == "content" {
			cs.FlexBasisAuto = true
		} else if s != "" {
			cs.FlexBasisAuto = false
			if px, frac, hasPct, ok := parseLengthPct(s, fs); ok && hasPct && frac > 0 {
				cs.FlexBasisPct, cs.FlexBasisPctOffset = frac, px
			} else if px, ok := parseLength(s, fs, 0); ok {
				cs.FlexBasis, cs.HasFlexBasis = px, true
			}
		}
	}
	if v, ok := m["order"]; ok {
		if n, err := strconv.Atoi(strings.TrimSpace(v)); err == nil {
			cs.Order = n
		}
	}

	if webCompat {
		resolveHeightDefiniteness(n, cs, parent)
	}
	return cs
}

// resolveHeightDefiniteness decides whether cs's height is definite and
// drops a percentage height that has nothing definite to resolve against:
// CSS computes it to `auto` then (CSS 2.1 § 10.5) — otherwise a
// `height: 100%` inside an auto-height box would grow with whatever the
// layout last gave it.
func resolveHeightDefiniteness(n *Node, cs, parent *ComputedStyle) {
	root := parent == nil || n.Tag == "html"
	outOfFlow := cs.Position == "absolute" || cs.Position == "fixed"
	parentDefinite := root || (parent != nil && parent.HeightDefinite)
	// A stretched item of a row flex container takes the container's
	// resolved cross size, which is definite for its descendants even
	// when the flex line was sized from its content.
	stretched := parent != nil && isFlexDisplay(parent.Display) && parent.FlexDirection == qui.Horizontal &&
		!cs.HasHeight && cs.HeightPct == 0 &&
		(cs.AlignSelf == qui.AlignDefault && (parent.AlignItems == qui.AlignStretch || parent.AlignItems == qui.AlignDefault) ||
			cs.AlignSelf == qui.AlignStretch)
	if cs.HeightPct > 0 && !parentDefinite && !outOfFlow {
		cs.HeightPct = 0
	}
	if cs.MinHeightPct > 0 && !parentDefinite && !outOfFlow {
		cs.MinHeightPct = 0
	}
	if cs.MaxHeightPct > 0 && !parentDefinite && !outOfFlow {
		cs.MaxHeightPct = 0
	}
	// The root's percentages resolve against the viewport (the initial
	// containing block); its own height is definite only when set.
	cs.HeightIntrinsicAuto = !cs.HasHeight && parent != nil && (parent.HeightIntrinsicAuto || stretched && !parent.HeightDefinite)
	cs.HeightDefinite = cs.HasHeight || (cs.HeightPct > 0 && (parentDefinite || outOfFlow)) ||
		stretched || (outOfFlow && cs.HasTop && cs.HasBottom)
}

// parseOutline resolves the outline shorthand and longhands. The initial
// style is none: a color or width alone does not enable an outline.
func parseOutline(m map[string]string, cs *ComputedStyle, fs float32) {
	width := float32(3) // medium
	color := cs.Color
	style := "none"
	if v, ok := m["outline"]; ok {
		for _, f := range fieldsTopLevel(v) {
			if _, ok := borderStyleKeyword(f); ok || f == "auto" {
				style = f
			} else if c, ok := parseColor(f); ok {
				color = c
			} else if px, ok := parseLength(f, fs, 0); ok {
				width = px
			}
		}
	}
	if v, ok := m["outline-style"]; ok {
		style = strings.TrimSpace(v)
	}
	if v, ok := m["outline-width"]; ok {
		if px, ok := parseLength(v, fs, 0); ok {
			width = px
		}
	}
	if v, ok := m["outline-color"]; ok {
		if c, ok := parseColor(v); ok {
			color = c
		}
	}
	if style == "none" || style == "hidden" || width <= 0 {
		// Color-only rules must not suppress a native control's focus ring.
		_, shorthand := m["outline"]
		_, longhand := m["outline-style"]
		cs.OutlineNone = shorthand || longhand || width <= 0
		return
	}
	cs.OutlineWidth, cs.OutlineColor, cs.HasOutline = width, color, true
}

// boxSizingExtra returns how much padding + border to ADD to the declared
// width / height so the widget's outer (border) box carries the CSS-correct
// size. For the default content-box, the declared size is the CONTENT box,
// so padding + border widen the outer box; for border-box the declared size
// already IS the outer box (qui's native Style.Width semantics), so nothing
// is added. Returned as (horizontal, vertical) extents.
func (cs *ComputedStyle) boxSizingExtra() (h, v float32) {
	if cs.BoxSizing == "border-box" {
		return 0, 0
	}
	// content-box (default): add padding + border on each axis.
	h = cs.Padding.Horizontal() + cs.borderExtentH()
	v = cs.Padding.Vertical() + cs.borderExtentV()
	return h, v
}

// borderExtentH / borderExtentV return the total horizontal / vertical border
// thickness, honoring per-side widths when set.
func (cs *ComputedStyle) borderExtentH() float32 {
	if cs.HasSideBorders {
		return cs.SideWidths.Left + cs.SideWidths.Right
	}
	return cs.BorderWidth * 2
}

func (cs *ComputedStyle) borderExtentV() float32 {
	if cs.HasSideBorders {
		return cs.SideWidths.Top + cs.SideWidths.Bottom
	}
	return cs.BorderWidth * 2
}

// decorationValue returns the text-decoration line value, preferring the
// text-decoration-line longhand over the text-decoration shorthand.
func decorationValue(m map[string]string) (string, bool) {
	if v, ok := m["text-decoration-line"]; ok {
		return v, true
	}
	if v, ok := m["text-decoration"]; ok {
		return v, true
	}
	return "", false
}

// parseDecorationStyle maps a text-decoration-style keyword to the engine
// enum (default solid).
func parseDecorationStyle(kw string) qui.DecorationLineStyle {
	switch kw {
	case "double":
		return qui.DecorationDouble
	case "dotted":
		return qui.DecorationDotted
	case "dashed":
		return qui.DecorationDashed
	case "wavy":
		return qui.DecorationWavy
	default:
		return qui.DecorationSolid
	}
}

// parseDecorationExtras pulls a style keyword, color, and thickness out of a
// `text-decoration` shorthand value (line keywords already consumed). Tokens
// are order-independent: a recognized style keyword sets the style, a length
// sets the thickness, and anything else that parses as a color sets the color.
func parseDecorationExtras(cs *ComputedStyle, v string, fs float32) {
	for _, tok := range fieldsTopLevel(v) {
		low := strings.ToLower(tok)
		switch low {
		case "underline", "line-through", "overline", "none", "solid":
			continue // line keyword or default style
		case "double", "dotted", "dashed", "wavy":
			cs.DecorationStyle = parseDecorationStyle(low)
			continue
		}
		if strings.HasSuffix(low, "px") || strings.HasSuffix(low, "em") ||
			strings.HasSuffix(low, "rem") || strings.HasSuffix(low, "%") {
			if px, ok := parseLength(tok, fs, fs); ok {
				cs.DecorationThickness = px
			}
			continue
		}
		if c, ok := parseColor(tok); ok {
			cs.DecorationColor, cs.HasDecorationColor = c, true
		}
	}
}

// decorationPaint bundles the computed text-decoration color/style/thickness
// for the renderer (zero value = text color, solid, auto thickness).
func (cs *ComputedStyle) decorationPaint() qui.DecorationPaint {
	return qui.DecorationPaint{
		Color:     cs.DecorationColor,
		HasColor:  cs.HasDecorationColor,
		Style:     cs.DecorationStyle,
		Thickness: cs.DecorationThickness,
	}
}

// decoration folds the computed underline / line-through flags into a
// qui.TextDecoration bitmask for the text renderer.
func (cs *ComputedStyle) decoration() qui.TextDecoration {
	var d qui.TextDecoration
	if cs.Underline {
		d |= qui.DecorationUnderline
	}
	if cs.LineThrough {
		d |= qui.DecorationLineThrough
	}
	if cs.Overline {
		d |= qui.DecorationOverline
	}
	return d
}

// parseOverflow reads overflow / overflow-x / overflow-y into the clip /
// scroll flags. A scroll/auto on either axis wins over hidden; the x-axis
// scroll is tracked separately so the builder can pick the ScrollView
// orientation.
func parseOverflow(m map[string]string, cs *ComputedStyle) {
	classify := func(v string) (clip, scroll bool) {
		switch strings.ToLower(strings.TrimSpace(v)) {
		case "hidden", "clip":
			return true, false
		case "scroll", "auto":
			return false, true
		}
		return false, false
	}
	var clipX, clipY, scrollX, scrollY bool
	// `overflow` shorthand: one value applies to both axes; two values are
	// x then y.
	if v, ok := m["overflow"]; ok {
		words := strings.Fields(v)
		if len(words) == 1 {
			c, s := classify(words[0])
			clipX, clipY, scrollX, scrollY = c, c, s, s
		} else if len(words) >= 2 {
			clipX, scrollX = classify(words[0])
			clipY, scrollY = classify(words[1])
		}
	}
	if v, ok := m["overflow-x"]; ok {
		clipX, scrollX = classify(v)
	}
	if v, ok := m["overflow-y"]; ok {
		clipY, scrollY = classify(v)
	}
	// CSS Overflow 3: `visible` beside a scrolling axis computes to `auto`,
	// so `overflow-y: auto` alone also scrolls content that overflows to
	// the right (Chrome does). Only an explicit hidden / clip keeps it out.
	if scrollY && !clipX {
		scrollX = true
	}
	cs.OverflowClip = clipX || clipY
	cs.OverflowClipX, cs.OverflowClipY = clipX, clipY
	cs.OverflowScroll = scrollX || scrollY
	// Pick the horizontal ScrollView orientation only when the x-axis
	// scrolls and the y-axis does not (a block's default scroll is vertical).
	cs.OverflowScrollX = scrollX && !scrollY
	cs.OverflowScrollXY = scrollX && scrollY
}

// firstField returns the first whitespace-separated token of v (or "").
func firstField(v string) string {
	f := strings.Fields(v)
	if len(f) == 0 {
		return ""
	}
	return f[0]
}

func parseBorder(m map[string]string, cs *ComputedStyle, fs float32) {
	if webCompat {
		parseWebBorder(m, cs, fs)
		return
	}
	cs.BorderStyle = qui.BorderSolid

	// HasBorder records that the author DECLARED the border (even as
	// `border: none`), so applyCommon can clear a control's native border
	// instead of leaving it untouched when the resolved width is 0.
	_, hasB := m["border"]
	_, hasBW := m["border-width"]
	_, hasBS := m["border-style"]
	cs.HasBorder = hasB || hasBW || hasBS

	// Uniform shorthand: `border: 1px solid #ccc`.
	if v, ok := m["border"]; ok {
		w, hasW, c, hasC, s, hasS := parseBorderShorthand(v, fs)
		if hasW {
			cs.BorderWidth = w
		} else {
			cs.BorderWidth = 1
		}
		if hasC {
			cs.BorderColor = c
		}
		if hasS {
			cs.BorderStyle = s
		}
	}
	if v, ok := m["border-width"]; ok {
		if px, ok := parseLength(v, fs, 0); ok {
			cs.BorderWidth = px
		}
	}
	if v, ok := m["border-color"]; ok {
		if c, ok := parseColor(v); ok {
			cs.BorderColor = c
		}
	}
	if v, ok := m["border-style"]; ok {
		cs.BorderStyle = parseBorderStyle(v)
	}
	if cs.BorderStyle == qui.BorderNone {
		cs.BorderWidth = 0
	}

	// Per-side. Seed from the uniform values, then apply overrides so a
	// mix (`border: 1px …; border-bottom: 2px …`) keeps the uniform base.
	cs.SideWidths = qui.Insets{Top: cs.BorderWidth, Right: cs.BorderWidth, Bottom: cs.BorderWidth, Left: cs.BorderWidth}
	cs.SideColors = qui.SideColors{Top: cs.BorderColor, Right: cs.BorderColor, Bottom: cs.BorderColor, Left: cs.BorderColor}
	sides := []struct {
		name string
		w    *float32
		c    *qui.Color
	}{
		{"top", &cs.SideWidths.Top, &cs.SideColors.Top},
		{"right", &cs.SideWidths.Right, &cs.SideColors.Right},
		{"bottom", &cs.SideWidths.Bottom, &cs.SideColors.Bottom},
		{"left", &cs.SideWidths.Left, &cs.SideColors.Left},
	}
	for _, s := range sides {
		if v, ok := m["border-"+s.name]; ok { // e.g. border-left: 4px solid #ddd
			w, hasW, c, hasC, _, _ := parseBorderShorthand(v, fs)
			if hasW {
				*s.w = w
				cs.HasSideBorders = true
			}
			if hasC {
				*s.c = c
			}
		}
		if v, ok := m["border-"+s.name+"-width"]; ok {
			if px, ok := parseLength(v, fs, 0); ok {
				*s.w = px
				cs.HasSideBorders = true
			}
		}
		if v, ok := m["border-"+s.name+"-color"]; ok {
			if c, ok := parseColor(v); ok {
				*s.c = c
			}
		}
	}
}

// CSS borders have an initial style of none. Width/color declarations do not
// make a border visible; each side computes its used width from its own style.
func parseWebBorder(m map[string]string, cs *ComputedStyle, fs float32) {
	width := float32(3) // medium
	color := cs.Color
	style := qui.BorderNone
	_, b := m["border"]
	_, bw := m["border-width"]
	_, bs := m["border-style"]
	cs.HasBorder = b || bw || bs
	if v, ok := m["border"]; ok {
		w, hw, c, hc, st, hs := parseBorderShorthand(v, fs)
		if hw {
			width = w
		}
		if hc {
			color = c
		}
		if hs {
			style = st
		}
	}
	widths := [4]float32{width, width, width, width}
	colors := [4]qui.Color{color, color, color, color}
	styles := [4]qui.BorderStyle{style, style, style, style}
	for part, apply := range map[string]func(int, string){
		"width": func(i int, v string) {
			if w, ok := parseLength(v, fs, 0); ok {
				widths[i] = w
			}
		},
		"color": func(i int, v string) {
			if c, ok := parseColor(v); ok {
				colors[i] = c
			}
		},
		"style": func(i int, v string) { styles[i] = parseBorderStyle(v) },
	} {
		if v, ok := m["border-"+part]; ok {
			f := fieldsTopLevel(v)
			if len(f) > 0 && len(f) <= 4 {
				idx := [4]int{0, 0, 0, 0}
				if len(f) >= 2 {
					idx[1] = 1
					idx[3] = 1
				}
				if len(f) >= 3 {
					idx[2] = 2
				}
				if len(f) == 4 {
					idx[3] = 3
				}
				for i, j := range idx {
					apply(i, f[j])
				}
			}
		}
	}
	for i, side := range []string{"top", "right", "bottom", "left"} {
		if v, ok := m["border-"+side]; ok {
			cs.HasBorder = true
			w, hw, c, hc, st, hs := parseBorderShorthand(v, fs)
			widths[i] = 3
			colors[i] = cs.Color
			styles[i] = qui.BorderNone
			if hw {
				widths[i] = w
			}
			if hc {
				colors[i] = c
			}
			if hs {
				styles[i] = st
			}
		}
		if v, ok := m["border-"+side+"-width"]; ok {
			cs.HasBorder = true
			if w, ok := parseLength(v, fs, 0); ok {
				widths[i] = w
			}
		}
		if v, ok := m["border-"+side+"-color"]; ok {
			if c, ok := parseColor(v); ok {
				colors[i] = c
			}
		}
		if v, ok := m["border-"+side+"-style"]; ok {
			cs.HasBorder = true
			styles[i] = parseBorderStyle(v)
		}
		if styles[i] == qui.BorderNone {
			widths[i] = 0
		}
	}
	cs.BorderStyle = qui.BorderSolid
	for i, st := range styles {
		if widths[i] > 0 {
			cs.BorderStyle = st
			break
		}
	}
	cs.BorderColor = colors[0]
	cs.SideWidths = qui.Insets{Top: widths[0], Right: widths[1], Bottom: widths[2], Left: widths[3]}
	cs.SideColors = qui.SideColors{Top: colors[0], Right: colors[1], Bottom: colors[2], Left: colors[3]}
	cs.HasSideBorders = widths[0] != widths[1] || widths[0] != widths[2] || widths[0] != widths[3] || ((widths[0] > 0 || widths[1] > 0 || widths[2] > 0 || widths[3] > 0) && (colors[0] != colors[1] || colors[0] != colors[2] || colors[0] != colors[3]))
	if !cs.HasSideBorders {
		cs.BorderWidth = widths[0]
	}
}

// parseBorderRadius resolves the uniform Radius plus per-corner Corners from
// `border-radius` (1–4 values, clockwise from top-left; an elliptical `a / b`
// keeps only the horizontal radii) and the four `border-<corner>-radius`
// longhands. When every corner ends up equal it collapses back to the uniform
// Radius fast path; otherwise Corners is set and box painting goes per-corner.
func parseBorderRadius(m map[string]string, cs *ComputedStyle, fs float32) {
	cs.borderRadii = parseRelativeBorderRadii(m, fs)
	var uniform float32
	var c qui.CornerRadii
	hasCorners := false
	if v, ok := m["border-radius"]; ok {
		if i := strings.IndexByte(v, '/'); i >= 0 { // elliptical: keep horizontal
			v = v[:i]
		}
		f := fieldsTopLevel(v)
		px := func(i int) float32 {
			p, _ := parseLength(f[i], fs, 0)
			return p
		}
		switch len(f) {
		case 1:
			uniform = px(0)
		case 2:
			a, b := px(0), px(1)
			c = qui.CornerRadii{TL: a, TR: b, BR: a, BL: b}
			hasCorners = true
		case 3:
			a, b, d := px(0), px(1), px(2)
			c = qui.CornerRadii{TL: a, TR: b, BR: d, BL: b}
			hasCorners = true
		default: // 4+
			c = qui.CornerRadii{TL: px(0), TR: px(1), BR: px(2), BL: px(3)}
			hasCorners = true
		}
	}
	longhands := []struct {
		name string
		p    *float32
	}{
		{"border-top-left-radius", &c.TL},
		{"border-top-right-radius", &c.TR},
		{"border-bottom-right-radius", &c.BR},
		{"border-bottom-left-radius", &c.BL},
	}
	anyLonghand := false
	for _, lh := range longhands {
		if _, ok := m[lh.name]; ok {
			anyLonghand = true
			break
		}
	}
	if anyLonghand && !hasCorners {
		c = qui.CornerRadii{TL: uniform, TR: uniform, BR: uniform, BL: uniform}
		hasCorners = true
	}
	for _, lh := range longhands {
		if v, ok := m[lh.name]; ok {
			if i := strings.IndexByte(v, '/'); i >= 0 {
				v = v[:i]
			}
			if p, ok := parseLength(firstField(v), fs, 0); ok {
				*lh.p = p
			}
		}
	}
	if hasCorners && c.TL == c.TR && c.TR == c.BR && c.BR == c.BL {
		uniform = c.TL // all equal → uniform fast path
		hasCorners = false
	}
	cs.Radius = uniform
	if hasCorners {
		cs.Corners = c
	}
}

// parseBorderShorthand splits a `border` / `border-<side>` value into its
// width / color / style tokens (any order, any subset).
func parseBorderShorthand(v string, fs float32) (w float32, hasW bool, c qui.Color, hasC bool, s qui.BorderStyle, hasS bool) {
	for _, tok := range fieldsTopLevel(v) {
		if bs, ok := borderStyleKeyword(tok); ok {
			s, hasS = bs, true
		} else if tok == "thin" {
			w, hasW = 1, true
		} else if tok == "medium" {
			w, hasW = 3, true
		} else if tok == "thick" {
			w, hasW = 5, true
		} else if px, ok := parseLength(tok, fs, 0); ok {
			w, hasW = px, true
		} else if col, ok := parseColor(tok); ok {
			c, hasC = col, true
		}
	}
	return
}

func parseBorderStyle(v string) qui.BorderStyle {
	if s, ok := borderStyleKeyword(strings.TrimSpace(v)); ok {
		return s
	}
	return qui.BorderSolid
}

// borderStyleKeyword maps a CSS border-style keyword to a qui.BorderStyle.
// Unsupported line styles (double/groove/ridge/inset/outset) degrade to
// solid; ok=false when the token isn't a border-style keyword at all.
func borderStyleKeyword(tok string) (qui.BorderStyle, bool) {
	switch strings.ToLower(tok) {
	case "solid", "double", "groove", "ridge", "inset", "outset":
		return qui.BorderSolid, true
	case "dashed":
		return qui.BorderDashed, true
	case "dotted":
		return qui.BorderDotted, true
	case "none", "hidden":
		return qui.BorderNone, true
	}
	return qui.BorderSolid, false
}

// parseBox reads a `padding`/`margin` shorthand (1–4 values) plus the
// per-side longhands into an Insets.
// parsePercentFraction parses a bare top-level percentage ("50%") into a
// fraction (0.5). Unlike parsePercentage it does not clamp, and it rejects
// anything that isn't exactly `<number>%` (calc/min/max keep their own path).
func parsePercentFraction(v string) (float32, bool) {
	v = strings.TrimSpace(v)
	if !strings.HasSuffix(v, "%") {
		return 0, false
	}
	f, ok := parseFloat(strings.TrimSuffix(v, "%"))
	if !ok || f <= 0 {
		return 0, false
	}
	return f / 100, true
}

// marginAutoFlags reports which sides carry CSS `margin: auto`, honoring
// the 1–4 value shorthand (top/right/bottom/left) plus longhand overrides.
func marginAutoFlags(m map[string]string) (top, right, bottom, left bool) {
	if v, ok := m["margin"]; ok {
		f := fieldsTopLevel(strings.ToLower(v))
		isAuto := func(i int) bool { return i < len(f) && f[i] == "auto" }
		switch len(f) {
		case 1:
			top, right, bottom, left = isAuto(0), isAuto(0), isAuto(0), isAuto(0)
		case 2:
			top, bottom = isAuto(0), isAuto(0)
			left, right = isAuto(1), isAuto(1)
		case 3:
			top = isAuto(0)
			left, right = isAuto(1), isAuto(1)
			bottom = isAuto(2)
		case 4:
			top, right, bottom, left = isAuto(0), isAuto(1), isAuto(2), isAuto(3)
		}
	}
	if v, ok := m["margin-top"]; ok {
		top = strings.EqualFold(strings.TrimSpace(v), "auto")
	}
	if v, ok := m["margin-right"]; ok {
		right = strings.EqualFold(strings.TrimSpace(v), "auto")
	}
	if v, ok := m["margin-bottom"]; ok {
		bottom = strings.EqualFold(strings.TrimSpace(v), "auto")
	}
	if v, ok := m["margin-left"]; ok {
		left = strings.EqualFold(strings.TrimSpace(v), "auto")
	}
	return top, right, bottom, left
}

func parseBox(m map[string]string, prop string, fs float32) qui.Insets {
	return parseBoxRelative(m, prop, fs, 0)
}

func parseBoxRelative(m map[string]string, prop string, fs, ref float32) qui.Insets {
	var ins qui.Insets
	if v, ok := m[prop]; ok {
		// Top-level split: `calc(1rem + 4px) 8px` is two values, not four.
		fields := fieldsTopLevel(v)
		vals := make([]float32, 0, 4)
		for _, f := range fields {
			px, _ := parseLength(f, fs, ref)
			vals = append(vals, px)
		}
		switch len(vals) {
		case 1:
			ins = qui.Insets{Top: vals[0], Right: vals[0], Bottom: vals[0], Left: vals[0]}
		case 2:
			ins = qui.Insets{Top: vals[0], Bottom: vals[0], Right: vals[1], Left: vals[1]}
		case 3:
			ins = qui.Insets{Top: vals[0], Right: vals[1], Left: vals[1], Bottom: vals[2]}
		case 4:
			ins = qui.Insets{Top: vals[0], Right: vals[1], Bottom: vals[2], Left: vals[3]}
		}
	}
	if v, ok := m[prop+"-top"]; ok {
		if px, ok := parseLength(v, fs, ref); ok {
			ins.Top = px
		}
	}
	if v, ok := m[prop+"-right"]; ok {
		if px, ok := parseLength(v, fs, ref); ok {
			ins.Right = px
		}
	}
	if v, ok := m[prop+"-bottom"]; ok {
		if px, ok := parseLength(v, fs, ref); ok {
			ins.Bottom = px
		}
	}
	if v, ok := m[prop+"-left"]; ok {
		if px, ok := parseLength(v, fs, ref); ok {
			ins.Left = px
		}
	}
	return ins
}

// parseCursor maps a CSS `cursor` keyword onto a qui.CursorShape.
//
// Three outcomes, because "declared" and "which shape" are separate
// questions (see ComputedStyle.Cursor):
//
//   - reset:    `auto` / CSS-wide `initial` — drop any inherited declaration
//     and hand the decision back to the widget's built-in shape.
//   - declared: a recognized keyword. Keywords qui has no native shape for
//     (move / grab / not-allowed / wait / help / zoom-* /
//     none) still count as declared and degrade to CursorDefault: the author
//     said "not text here", and suppressing a nested I-beam is the part of
//     that intent qui can honor. GLFW 3.3 only offers six standard shapes,
//     so widening the set means adding CursorShape values plus cocoa/glfw
//     mappings in the platform seam — a separate change.
//   - neither:  an unrecognized value, an invalid declaration per CSS, so
//     the inherited answer stands.
//
// `url(...)` custom cursors and multi-value fallback lists are not
// supported; the first bare keyword in the list decides.
func parseCursor(v string) (shape qui.CursorShape, declared, reset bool) {
	for _, tok := range strings.Split(v, ",") {
		tok = strings.ToLower(strings.TrimSpace(tok))
		if tok == "" || strings.HasPrefix(tok, "url(") {
			continue // custom bitmap cursor — skip to the fallback keyword
		}
		switch tok {
		case "auto", "initial", "unset", "revert":
			return qui.CursorDefault, false, true
		case "pointer":
			return qui.CursorHand, true, false
		case "text", "vertical-text":
			return qui.CursorText, true, false
		case "crosshair", "cell":
			return qui.CursorCrosshair, true, false
		case "ew-resize", "col-resize", "e-resize", "w-resize":
			return qui.CursorResizeEW, true, false
		case "ns-resize", "row-resize", "n-resize", "s-resize":
			return qui.CursorResizeNS, true, false
		case "nwse-resize", "nw-resize", "se-resize":
			return qui.CursorResizeNWSE, true, false
		case "nesw-resize", "ne-resize", "sw-resize":
			return qui.CursorResizeNESW, true, false
		case "default", "none", "context-menu", "help", "progress", "wait",
			"move", "grab", "grabbing", "not-allowed", "no-drop", "alias",
			"copy", "all-scroll", "zoom-in", "zoom-out":
			// Recognized, but no native shape → declared + degraded.
			return qui.CursorDefault, true, false
		}
	}
	return qui.CursorDefault, false, false
}

// normalizeAppRegion maps an app-region declaration onto the two values El
// acts on. Anything else (including CSS-wide `none`) reads as "not a drag
// region", which is also what an unstyled element means.
func normalizeAppRegion(v string) string {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "drag":
		return "drag"
	case "no-drag":
		return "no-drag"
	}
	return ""
}

func parseFontWeight(v string, cur qui.FontWeight) qui.FontWeight {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "bold", "bolder":
		return qui.FontWeightBold
	case "normal":
		return qui.FontWeightNormal
	case "lighter":
		return qui.FontWeightLight
	}
	if n, ok := parseFloat(v); ok {
		return qui.FontWeight(int(n))
	}
	return cur
}

// firstFontFamily resolves a CSS font-family list the way a browser
// does: the first family that is actually installed (registered with
// qui via SetDefaultFont / LoadFontVariant*) wins, so
// `"Google Sans", Roboto, Arial, sans-serif` lands on Roboto when only
// Roboto is registered. A generic family keyword (sans-serif, serif,
// system-ui, ...) that is not itself registered ends the search — it
// means "the default face", which is what the engine falls back to
// anyway. When nothing in the list is installed the first entry is
// returned unchanged, so the computed value still names what the author
// asked for (and renders with the default face).
// fontSizeKeywordIndex maps an absolute-size keyword to 1…8 (0: none).
func fontSizeKeywordIndex(v string) int8 {
	switch v {
	case "xx-small":
		return 1
	case "x-small":
		return 2
	case "small":
		return 3
	case "medium":
		return 4
	case "large":
		return 5
	case "x-large":
		return 6
	case "xx-large":
		return 7
	case "xxx-large":
		return 8
	}
	return 0
}

// fontSizeForKeyword is Chrome's absolute-size table for a 16px default
// font (13px for a monospace-only font-family).
func fontSizeForKeyword(k int8, mono bool) float32 {
	sans := [...]float32{9, 10, 13, 16, 18, 24, 32, 48}
	monoT := [...]float32{9, 9, 10, 13, 16, 20, 26, 39}
	if k < 1 || k > 8 {
		return 16
	}
	if mono {
		return monoT[k-1]
	}
	return sans[k-1]
}

func firstFontFamily(v string) string {
	parts := strings.Split(v, ",")
	first := ""
	for i, p := range parts {
		name := UnescapeCSS(strings.Trim(strings.TrimSpace(p), "'\""))
		if i == 0 {
			first = name
		}
		if name == "" {
			continue
		}
		if qui.FontFamilyRegistered(name) {
			return name
		}
		if isGenericFontFamily(name) {
			if mapped := genericFontFamilies[strings.ToLower(name)]; mapped != "" && qui.FontFamilyRegistered(mapped) {
				return mapped
			}
			break
		}
	}
	return first
}

// hiddenByClosedDetails reports whether n is content of a closed
// <details> other than its summary.
func hiddenByClosedDetails(n *Node) bool {
	p := siblingParent(n)
	if p == nil || p.Tag != "details" || n.Type != ElementNode {
		return false
	}
	if _, open := p.Attr("open"); open {
		return false
	}
	return firstSummary(p) != n
}

// firstSummary is a <details>' first <summary> child.
func firstSummary(details *Node) *Node {
	for _, c := range details.Children {
		if c.Type == ElementNode && c.Tag == "summary" {
			return c
		}
	}
	return nil
}

// isFlexDisplay / isGridDisplay report a flex (grid) container, block- or
// inline-level: inline-flex lays its children out as flex items exactly
// like flex, only the box itself sits on a line.
func isFlexDisplay(d string) bool { return d == "flex" || d == "inline-flex" }
func isGridDisplay(d string) bool { return d == "grid" || d == "inline-grid" }

// blockify maps a display value to its block-level equivalent (CSS
// Display 3 § 2.7): what floats, absolutely positioned boxes and flex / grid
// items get.
func blockify(d string) string {
	switch d {
	case "inline", "inline-block", "contents", "":
		return "block"
	case "inline-flex":
		return "flex"
	case "inline-grid":
		return "grid"
	case "inline-table":
		return "table"
	}
	return d
}

// newFormattingContext reports whether a box with this style establishes
// a new block formatting context (CSS 2.1 § 9.4.1, CSS Display 3): such a
// box is placed beside floats instead of under them, and contains its own.
func (cs *ComputedStyle) newFormattingContext() bool {
	if cs == nil {
		return false
	}
	// Flex and grid items establish independent formatting contexts, even
	// when their own computed display is block. Their auto height contains
	// descendant floats.
	if cs.ZIndexItem {
		return true
	}
	if cs.Float != "" || cs.OverflowScroll || cs.OverflowClip {
		return true
	}
	switch cs.Position {
	case "absolute", "fixed":
		return true
	}
	switch cs.Display {
	case "flow-root", "flex", "inline-flex", "grid", "inline-grid", "table", "inline-table",
		"inline-block", "table-cell", "table-caption", "list-item-flow-root":
		return true
	}
	if v := strings.TrimSpace(cs.raw["contain"]); v == "layout" || v == "content" || v == "strict" || v == "paint" {
		return true
	}
	return false
}

// pseudoAfterClears reports a clearfix: an ::after rule that clears floats
// (`.clearfix::after { content: ""; display: table; clear: both }`). The
// generated box ends the element below its floats, so the element contains
// them — which is what htmlcss models it as.
func pseudoAfterClears(n *Node, sheet *Stylesheet) bool {
	for _, ri := range sheet.candidateRules(n) {
		rule := &sheet.Rules[ri]
		clears := false
		for _, d := range rule.Declarations {
			if d.Property == "clear" {
				v := strings.ToLower(strings.TrimSpace(d.Value))
				clears = v == "both" || v == "left" || v == "right"
			}
		}
		if !clears {
			continue
		}
		for _, sel := range rule.Selectors {
			if sel.pseudoElement() == "after" && selectorMatchesIgnoringPseudoElem(sel, n) {
				return true
			}
		}
	}
	return false
}

// isInlineLevelDisplay reports display values that are inline-level
// (-webkit-center leaves those to text alignment).
func isInlineLevelDisplay(d string) bool {
	switch d {
	case "inline", "inline-block", "inline-flex", "inline-grid", "inline-table", "contents", "none":
		return true
	}
	return false
}

// genericFontFamilies maps CSS generic families to concrete ones (see
// SetGenericFontFamilies). Empty by default: a generic means the default
// face.
var genericFontFamilies = map[string]string{}

// SetGenericFontFamilies maps CSS generic family keywords (serif,
// sans-serif, monospace, system-ui, cursive, fantasy, …) to installed or
// registered families — a browser's font settings ("Standard font:
// Times"). A generic left unmapped renders with the default face.
func SetGenericFontFamilies(m map[string]string) {
	genericFontFamilies = map[string]string{}
	for k, v := range m {
		genericFontFamilies[strings.ToLower(k)] = v
	}
}

// isGenericFontFamily reports CSS generic family keywords.
func isGenericFontFamily(name string) bool {
	switch strings.ToLower(name) {
	case "serif", "sans-serif", "monospace", "cursive", "fantasy",
		"system-ui", "ui-sans-serif", "ui-serif", "ui-monospace", "ui-rounded",
		"-apple-system", "blinkmacsystemfont", "math", "emoji", "fangsong":
		return true
	}
	return false
}

func parseTextAlign(v string) qui.TextAlign {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "center", "-webkit-center", "-moz-center", "-khtml-center":
		return qui.TextAlignCenter
	case "right", "end", "-webkit-right", "-moz-right":
		return qui.TextAlignEnd
	case "justify":
		return qui.TextAlignJustify
	default:
		return qui.TextAlignStart
	}
}

func parseJustify(v string) qui.Justify {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "center":
		return qui.JustifyCenter
	case "flex-end", "end", "right":
		return qui.JustifyEnd
	case "space-between":
		return qui.JustifySpaceBetween
	case "space-around":
		return qui.JustifySpaceAround
	case "space-evenly":
		return qui.JustifySpaceEvenly
	default:
		return qui.JustifyStart
	}
}

// parseAlignContent maps CSS align-content keywords. The CSS initial value
// (`normal`, and anything unrecognized) is stretch for flex containers.
func parseAlignContent(v string) qui.AlignContent {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "flex-start", "start":
		return qui.AlignContentStart
	case "center":
		return qui.AlignContentCenter
	case "flex-end", "end":
		return qui.AlignContentEnd
	case "space-between":
		return qui.AlignContentSpaceBetween
	case "space-around":
		return qui.AlignContentSpaceAround
	case "space-evenly":
		return qui.AlignContentSpaceEvenly
	default:
		return qui.AlignContentStretch
	}
}

func parseAlign(v string) qui.AlignCross {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "center":
		return qui.AlignCenter
	case "flex-start", "start":
		return qui.AlignStart
	case "flex-end", "end":
		return qui.AlignEnd
	case "stretch":
		return qui.AlignStretch
	default:
		return qui.AlignStretch
	}
}

// FontFeatures is the element's OpenType feature list for qui.Font:
// font-variant-ligatures, then font-feature-settings (later wins).
func (cs *ComputedStyle) FontFeatures() []string {
	if len(cs.FeatureSettings) == 0 {
		return cs.LigatureFeatures
	}
	if len(cs.LigatureFeatures) == 0 {
		return cs.FeatureSettings
	}
	return append(append([]string(nil), cs.LigatureFeatures...), cs.FeatureSettings...)
}

// ligatureKeywords maps font-variant-ligatures keywords to the features
// they switch (CSS Fonts 4 §6.4).
var ligatureKeywords = map[string][]string{
	"common-ligatures":           {"liga", "clig"},
	"no-common-ligatures":        {"-liga", "-clig"},
	"discretionary-ligatures":    {"dlig"},
	"no-discretionary-ligatures": {"-dlig"},
	"historical-ligatures":       {"hlig"},
	"no-historical-ligatures":    {"-hlig"},
	"contextual":                 {"calt"},
	"no-contextual":              {"-calt"},
}

// parseVariantLigatures parses font-variant-ligatures: normal, none (every
// ligature and contextual alternate off — what code fonts need so "!="
// stays two glyphs), or a list of the keywords above. A value with an
// unknown or repeated-group keyword is invalid.
func parseVariantLigatures(v string) ([]string, bool) {
	switch kw := strings.ToLower(strings.TrimSpace(v)); kw {
	case "normal":
		return nil, true
	case "none":
		return []string{"-liga", "-clig", "-dlig", "-hlig", "-calt"}, true
	}
	var out []string
	groups := map[string]bool{}
	for _, tok := range strings.Fields(strings.ToLower(v)) {
		f, ok := ligatureKeywords[tok]
		if !ok {
			return nil, false
		}
		group := strings.TrimPrefix(tok, "no-")
		if groups[group] {
			return nil, false
		}
		groups[group] = true
		out = append(out, f...)
	}
	return out, len(out) > 0
}

// parseFeatureSettings parses font-feature-settings: normal or a comma
// list of `"tag" [on | off | <integer>]`. A malformed entry invalidates
// the whole declaration, as in browsers.
func parseFeatureSettings(v string) ([]string, bool) {
	v = strings.TrimSpace(v)
	if strings.EqualFold(v, "normal") {
		return nil, true
	}
	var out []string
	for _, item := range strings.Split(v, ",") {
		item = strings.TrimSpace(item)
		if len(item) < 6 || (item[0] != '"' && item[0] != '\'') || item[5] != item[0] {
			return nil, false
		}
		tag := item[1:5]
		for i := 0; i < 4; i++ {
			if tag[i] < 0x20 || tag[i] > 0x7e {
				return nil, false
			}
		}
		val := 1
		switch rest := strings.ToLower(strings.TrimSpace(item[6:])); rest {
		case "", "on":
		case "off":
			val = 0
		default:
			n, err := strconv.Atoi(rest)
			if err != nil || n < 0 {
				return nil, false
			}
			val = n
		}
		out = append(out, tag+"="+strconv.Itoa(val))
	}
	return out, true
}

// parseAspectRatio reads `16 / 9`, `1.5` or `auto 4/3` as width / height
// (0 for auto or invalid).
func parseAspectRatio(v string) float32 {
	v = strings.TrimSpace(strings.ToLower(v))
	v = strings.TrimSpace(strings.TrimPrefix(v, "auto"))
	if v == "" {
		return 0
	}
	a, b, slash := strings.Cut(v, "/")
	w, ok := parseFloat(strings.TrimSpace(a))
	if !ok || w <= 0 {
		return 0
	}
	if !slash {
		return w
	}
	h, ok := parseFloat(strings.TrimSpace(b))
	if !ok || h <= 0 {
		return 0
	}
	return w / h
}
