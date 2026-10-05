package htmlcss

import (
	"sort"
	"strconv"
	"strings"

	"github.com/qizhanchan/qui"
)

// ComputedStyle is the resolved, ready-to-render style for one element.
// It is what build.go compiles into qui.Style + a layout engine.
type ComputedStyle struct {
	Display string // block | inline | inline-block | flex | none
	Hidden  bool   // visibility: hidden / collapse (paints nothing, keeps box)

	// Text / font (inheritable).
	Color qui.Color
	// HasColor records whether `color` was explicitly set anywhere up the
	// inheritance chain. Form controls only override their native text
	// color when it was authored — otherwise they keep the UA black.
	HasColor bool
	// AccentColor is CSS `accent-color` (inherited): the fill for form
	// controls' active parts — checkbox tick box, radio dot, switch track,
	// slider active track. Lets a dark stylesheet retint controls whose
	// chrome otherwise comes from the light native defaults.
	AccentColor    qui.Color
	HasAccentColor bool
	// ScrollbarThumb / ScrollbarTrack are CSS `scrollbar-color` (inherited).
	ScrollbarThumb, ScrollbarTrack qui.Color
	HasScrollbarColor              bool
	FontSize       float32
	FontWeight     qui.FontWeight
	Italic         bool
	FontFamily     string
	LineHeight     float32 // multiplier over font size
	TextAlign      qui.TextAlign
	// LetterSpacing / WordSpacing (px) are CSS tracking, inherited; they flow
	// into qui.Font so the root text engine measures + paints them.
	LetterSpacing float32
	WordSpacing   float32
	// TextIndent (px) indents the first line of a block's text (inherited).
	TextIndent  float32
	Underline   bool
	LineThrough bool
	Overline    bool
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
	HasSideBorders bool
	SideWidths     qui.Insets
	SideColors     qui.SideColors
	Radius         float32
	// Corners holds per-corner radii (border-*-radius or a multi-value
	// border-radius). Zero means "uniform Radius applies to all corners".
	Corners qui.CornerRadii
	Padding qui.Insets
	Margin  qui.Insets
	// Outline draws a ring outside the border box; it doesn't affect layout.
	OutlineWidth float32
	OutlineColor qui.Color
	HasOutline   bool

	// Sizing.
	Width     float32
	HasWidth  bool
	Height    float32
	HasHeight bool
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
	Gap           float32
	Wrap          bool
	AlignContent  qui.AlignContent // wrap-line distribution; CSS initial = stretch
	FlexGrow      float32
	// Per-item flex properties (align-self / flex-shrink / flex-basis / order).
	AlignSelf     qui.AlignCross // AlignDefault = auto (inherit container)
	FlexShrink    float32
	HasFlexShrink bool
	FlexBasis     float32
	HasFlexBasis  bool
	Order         int

	// Effects (Wave 1: box-shadow / opacity / transform / gradient).
	Shadow       qui.ShadowStyle
	HasShadow    bool
	ExtraShadows []qui.ShadowStyle // additional box-shadow layers
	Opacity      float32
	HasOpacity   bool
	Transform    *transformSpec
	Gradient     *gradientSpec
	Filter       qui.ImageFilter // CSS filter: blur()/drop-shadow()
	// BackgroundImageURL is the url() from background-image (resolved + loaded
	// in El.applyComputed, which has the engine's BaseDir). Gradient wins.
	BackgroundImageURL string

	// Positioning + overflow (Wave 2).
	Position                             string // "", static, relative, absolute, fixed
	Top, Right, Bottom, Left             float32
	HasTop, HasRight, HasBottom, HasLeft bool
	ZIndex                               int  // paint order among siblings (0 = default)
	OverflowClip                         bool // overflow: hidden / clip
	OverflowScroll                       bool // overflow: auto / scroll
	OverflowScrollX                      bool // horizontal scroll axis

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
}

func computeNode(n *Node, sheet *Stylesheet, parent *ComputedStyle) *ComputedStyle {
	base := interpret(n, mergedDecls(n, sheet, selectorState{}), parent)
	// When the sheet has interactive-state rules, compute the matching
	// variant(s) too — the built widget swaps to them while the element is
	// hovered / focused / pressed. Each variant inherits from the parent's
	// resting style, matching how the base does.
	if sheet.HasHover {
		// The subject's hover propagates to ancestor compounds in matching
		// (hovering an element hovers its ancestors), so `.row:hover .del`
		// also lands in .del's own Hover variant — and `.a:hover .b:hover`
		// works when the cursor is on .b.
		base.Hover = interpret(n, mergedDecls(n, sheet, selectorState{hover: true}), parent)
	}
	if sheet.HasFocus {
		base.Focus = interpret(n, mergedDecls(n, sheet, selectorState{focus: true}), parent)
	}
	if sheet.HasFocusVisible {
		base.FocusVisible = interpret(n, mergedDecls(n, sheet, selectorState{focus: true, focusVisible: true}), parent)
	}
	if sheet.HasActive {
		// A pressed element is conventionally also hovered; both states
		// propagate to ancestor compounds in matching, keeping `.btn:hover`
		// under an `.btn:active` press and `.row:hover .btn:active` while
		// the row is hovered.
		base.Active = interpret(n, mergedDecls(n, sheet, selectorState{hover: true, active: true}), parent)
	}
	// The ancestor-state UNION variants: only NON-subject compounds are
	// treated as hovered / focused / pressed, so `.row:hover .del` lands here
	// (fires from the row) while `.del:hover` does not. Equal to base when no
	// such rule matches this node, so their delta is a no-op — see
	// hasStateBoxVariant / textStateDiffers / El.linkAncestorState.
	if sheet.HasAncestorHover {
		base.AncestorHover = interpret(n, mergedDecls(n, sheet, selectorState{ancestorHover: true}), parent)
	}
	if sheet.HasAncestorFocus {
		base.AncestorFocus = interpret(n, mergedDecls(n, sheet, selectorState{ancestorFocus: true}), parent)
	}
	if sheet.HasAncestorActive {
		base.AncestorActive = interpret(n, mergedDecls(n, sheet, selectorState{ancestorActive: true}), parent)
	}
	return base
}

// pseudoStyle computes the style + text for a ::before/::after pseudo-element
// of n (which = "before"/"after"), inheriting from n's own computed style.
// Returns "" content when no matching rule carries a drawable `content`.
func pseudoStyle(n *Node, sheet *Stylesheet, which string, parent *ComputedStyle) (*ComputedStyle, string) {
	var decls []matchedDecl
	order := 0
	for _, rule := range sheet.Rules {
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
				decls = append(decls, matchedDecl{decl: d, a: ba, b: bb, c: bc, order: order, tier: 1})
				order++
			}
		}
	}
	if len(decls) == 0 {
		return nil, ""
	}
	sort.SliceStable(decls, func(i, j int) bool { return cascadeKey(decls[i]) < cascadeKey(decls[j]) })
	merged := map[string]string{}
	for _, m := range decls {
		merged[m.decl.Property] = m.decl.Value
	}
	content, ok := merged["content"]
	if !ok {
		return nil, "" // no content → no generated box
	}
	return interpret(n, merged, parent), unquoteContent(content, n)
}

// unquoteContent resolves a CSS `content` value to literal text. A value is a
// concatenation of components: quoted strings ("foo"/'foo') and attr(<name>)
// references, which resolve to the element's attribute value (empty when the
// attribute is absent). none/normal → empty (no generated box). counter() and
// other unsupported functions pass through verbatim.
func unquoteContent(v string, n *Node) string {
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
			end := strings.IndexByte(v[i+1:], c)
			if end < 0 {
				sb.WriteString(v[i+1:]) // unterminated: take the rest
				i = len(v)
			} else {
				sb.WriteString(v[i+1 : i+1+end])
				i += end + 2
			}
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
	var decls []matchedDecl

	// Tier 0: user-agent defaults for this tag. Push-button input types
	// (submit/reset/button) borrow the <button> default chrome.
	uaTag := n.Tag
	if n.Tag == "input" {
		switch n.AttrOr("type", "") {
		case "submit", "reset", "button", "file", "color":
			uaTag = "button"
		}
	}
	// The global `hidden` attribute is a UA rule (`[hidden] { display: none }`),
	// not an intrinsic property — so author CSS can override it, exactly as in
	// a browser. Emitting it at tier 0 gets that for free.
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

	// Tier 1: author rules that match.
	order := 0
	for _, rule := range sheet.Rules {
		best := -1
		var ba, bb, bc int
		for _, sel := range rule.Selectors {
			if sel.matches(n, st) {
				a, b, c := sel.specificity()
				score := a*10000 + b*100 + c
				if score > best {
					best, ba, bb, bc = score, a, b, c
				}
			}
		}
		if best >= 0 {
			for _, d := range rule.Declarations {
				decls = append(decls, matchedDecl{decl: d, a: ba, b: bb, c: bc, order: order, tier: 1})
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
	sort.SliceStable(decls, func(i, j int) bool {
		return cascadeKey(decls[i]) < cascadeKey(decls[j])
	})

	merged := map[string]string{}
	for _, m := range decls {
		merged[m.decl.Property] = m.decl.Value
	}
	return merged
}

func cascadeKey(m matchedDecl) int {
	tier := m.tier
	if m.decl.Important {
		tier += 10 // important beats any normal tier
	}
	spec := m.a*10000 + m.b*100 + m.c
	return tier*1_000_000 + spec*10 // order handled by stable sort
}

// interpret turns the merged declaration map into a ComputedStyle,
// inheriting text/font properties from parent.
func interpret(n *Node, m map[string]string, parent *ComputedStyle) *ComputedStyle {
	// Resolve CSS custom properties + var() before reading any property:
	// build this node's inherited custom-prop table, then substitute var()
	// throughout the normal declarations.
	customProps := buildCustomProps(parent, m)
	m = resolveMapVars(m, customProps)
	inheritRegisteredProps(m, parent)
	expandShorthands(m)
	cs := &ComputedStyle{raw: m, customProps: customProps}

	// --- inheritable defaults ---
	if parent != nil {
		cs.Color = parent.Color
		cs.HasColor = parent.HasColor
		cs.FontSize = parent.FontSize
		cs.FontWeight = parent.FontWeight
		cs.Italic = parent.Italic
		cs.FontFamily = parent.FontFamily
		cs.LineHeight = parent.LineHeight
		cs.TextAlign = parent.TextAlign
		cs.LetterSpacing = parent.LetterSpacing
		cs.WordSpacing = parent.WordSpacing
		cs.TextIndent = parent.TextIndent
		cs.AccentColor = parent.AccentColor
		cs.HasAccentColor = parent.HasAccentColor
		cs.ScrollbarThumb = parent.ScrollbarThumb
		cs.ScrollbarTrack = parent.ScrollbarTrack
		cs.HasScrollbarColor = parent.HasScrollbarColor
		// white-space is inherited (CSS): a <b> inside a pre keeps
		// preserving spaces unless it declares its own value.
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
	} else {
		cs.Color = qui.Color{R: 0.1, G: 0.1, B: 0.1, A: 1}
		cs.FontSize = 16
		cs.FontWeight = qui.FontWeightNormal
		cs.LineHeight = 1.4
		cs.TextAlign = qui.TextAlignStart
	}

	// --- display ---
	cs.Display = strings.ToLower(strings.TrimSpace(m["display"]))
	if cs.Display == "" {
		cs.Display = defaultDisplay(n.Tag)
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
	if v, ok := m["font-size"]; ok {
		if px, ok := parseLength(v, cs.FontSize, cs.FontSize); ok {
			cs.FontSize = px
		}
	}
	fs := cs.FontSize

	// --- color ---
	if v, ok := m["color"]; ok {
		if c, ok := parseColor(v); ok {
			cs.Color, cs.HasColor = c, true
		}
	}
	// --- accent-color (inherited; "auto" falls back to the native default) ---
	if v, ok := m["accent-color"]; ok {
		if strings.EqualFold(strings.TrimSpace(v), "auto") {
			cs.HasAccentColor = false
		} else if c, ok := parseColor(v); ok {
			cs.AccentColor, cs.HasAccentColor = c, true
		}
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
	if v, ok := m["line-height"]; ok {
		if lh, ok := parseFloat(strings.TrimSpace(v)); ok && !strings.ContainsAny(v, "px%rem") {
			cs.LineHeight = lh // unitless multiplier
		} else if px, ok := parseLength(v, fs, fs); ok && fs > 0 {
			cs.LineHeight = px / fs
		}
	}
	if v, ok := m["text-align"]; ok {
		cs.TextAlign = parseTextAlign(v)
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
	if v, ok := m["background-color"]; ok {
		if c, ok := parseColor(v); ok {
			cs.Background, cs.HasBackground = c, true
		}
	}
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
	// linear/radial-gradient or url() in background / background-image.
	if v, ok := m["background-image"]; ok {
		if g, ok := parseGradient(v); ok {
			cs.Gradient = g
		} else if u, ok := parseCSSURL(v); ok {
			cs.BackgroundImageURL = u
		}
	}
	if cs.Gradient == nil {
		if v, ok := m["background"]; ok {
			if g, ok := parseGradient(v); ok {
				cs.Gradient = g
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
	if v, ok := m["filter"]; ok {
		if f, ok := parseFilter(v, fs); ok {
			cs.Filter = f
		}
	}
	if v, ok := m["transform-origin"]; ok && cs.Transform != nil {
		if ox, oy, ok := parseTransformOrigin(v); ok {
			cs.Transform.ox, cs.Transform.oy, cs.Transform.hasOrigin = ox, oy, true
		}
	}

	// --- positioning + overflow ---
	cs.Position = strings.ToLower(strings.TrimSpace(m["position"]))
	if v, ok := m["z-index"]; ok {
		if n, err := strconv.Atoi(strings.TrimSpace(v)); err == nil {
			cs.ZIndex = n
		}
	}
	if v, ok := m["top"]; ok {
		if px, ok := parseLength(v, fs, 0); ok {
			cs.Top, cs.HasTop = px, true
		}
	}
	if v, ok := m["right"]; ok {
		if px, ok := parseLength(v, fs, 0); ok {
			cs.Right, cs.HasRight = px, true
		}
	}
	if v, ok := m["bottom"]; ok {
		if px, ok := parseLength(v, fs, 0); ok {
			cs.Bottom, cs.HasBottom = px, true
		}
	}
	if v, ok := m["left"]; ok {
		if px, ok := parseLength(v, fs, 0); ok {
			cs.Left, cs.HasLeft = px, true
		}
	}
	parseOverflow(m, cs)
	// CSS blockifies out-of-flow (absolute/fixed) elements — an inline <span>
	// with position:absolute lays out as a block. This also keeps it out of
	// inline runs (buildFlow), so it stays a standalone positioned widget.
	if (cs.Position == "absolute" || cs.Position == "fixed") && strings.HasPrefix(cs.Display, "inline") {
		cs.Display = "block"
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

	// --- sizing ---
	cs.BoxSizing = strings.ToLower(strings.TrimSpace(m["box-sizing"]))
	if v, ok := m["width"]; ok {
		if pct, ok := parsePercentFraction(v); ok {
			cs.WidthPct = pct
		} else if px, ok := parseLength(v, fs, 0); ok {
			cs.Width, cs.HasWidth = px, true
		}
	}
	if v, ok := m["height"]; ok {
		if pct, ok := parsePercentFraction(v); ok {
			cs.HeightPct = pct
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
	if v, ok := m["gap"]; ok {
		if px, ok := parseLength(v, fs, 0); ok {
			cs.Gap = px
		}
	}
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
	if v, ok := m["flex-basis"]; ok {
		if s := strings.ToLower(strings.TrimSpace(v)); s != "" && s != "auto" {
			if px, ok := parseLength(s, fs, 0); ok {
				cs.FlexBasis, cs.HasFlexBasis = px, true
			}
		}
	}
	if v, ok := m["order"]; ok {
		if n, err := strconv.Atoi(strings.TrimSpace(v)); err == nil {
			cs.Order = n
		}
	}

	return cs
}

// parseOutline resolves the `outline` shorthand plus outline-width/-color.
// Width defaults to 1px when a color is present without an explicit width.
func parseOutline(m map[string]string, cs *ComputedStyle, fs float32) {
	var width float32
	var color qui.Color
	haveW, haveC := false, false
	if v, ok := m["outline"]; ok && strings.TrimSpace(v) != "" {
		if strings.EqualFold(strings.TrimSpace(v), "none") {
			return
		}
		for _, f := range strings.Fields(v) {
			if c, ok := parseColor(f); ok {
				color, haveC = c, true
			} else if px, ok := parseLength(f, fs, 0); ok {
				width, haveW = px, true
			}
		}
	}
	if v, ok := m["outline-width"]; ok {
		if px, ok := parseLength(v, fs, 0); ok {
			width, haveW = px, true
		}
	}
	if v, ok := m["outline-color"]; ok {
		if c, ok := parseColor(v); ok {
			color, haveC = c, true
		}
	}
	if !haveW && !haveC {
		return
	}
	if !haveW {
		width = 1
	}
	if !haveC {
		color = qui.Color{R: 0, G: 0, B: 0, A: 1}
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
	cs.OverflowClip = clipX || clipY
	cs.OverflowScroll = scrollX || scrollY
	// Pick the horizontal ScrollView orientation only when the x-axis
	// scrolls and the y-axis does not (a block's default scroll is vertical).
	cs.OverflowScrollX = scrollX && !scrollY
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

// parseBorderRadius resolves the uniform Radius plus per-corner Corners from
// `border-radius` (1–4 values, clockwise from top-left; an elliptical `a / b`
// keeps only the horizontal radii) and the four `border-<corner>-radius`
// longhands. When every corner ends up equal it collapses back to the uniform
// Radius fast path; otherwise Corners is set and box painting goes per-corner.
func parseBorderRadius(m map[string]string, cs *ComputedStyle, fs float32) {
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
	for _, tok := range strings.Fields(v) {
		if bs, ok := borderStyleKeyword(tok); ok {
			s, hasS = bs, true
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
		f := strings.Fields(strings.ToLower(v))
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
	var ins qui.Insets
	if v, ok := m[prop]; ok {
		fields := strings.Fields(v)
		vals := make([]float32, 0, 4)
		for _, f := range fields {
			px, _ := parseLength(f, fs, 0)
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
		if px, ok := parseLength(v, fs, 0); ok {
			ins.Top = px
		}
	}
	if v, ok := m[prop+"-right"]; ok {
		if px, ok := parseLength(v, fs, 0); ok {
			ins.Right = px
		}
	}
	if v, ok := m[prop+"-bottom"]; ok {
		if px, ok := parseLength(v, fs, 0); ok {
			ins.Bottom = px
		}
	}
	if v, ok := m[prop+"-left"]; ok {
		if px, ok := parseLength(v, fs, 0); ok {
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
//     (move / grab / not-allowed / wait / help / zoom-* / diagonal resizes /
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
		case "default", "none", "context-menu", "help", "progress", "wait",
			"move", "grab", "grabbing", "not-allowed", "no-drop", "alias",
			"copy", "all-scroll", "zoom-in", "zoom-out",
			"ne-resize", "nw-resize", "se-resize", "sw-resize",
			"nesw-resize", "nwse-resize":
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

func firstFontFamily(v string) string {
	parts := strings.Split(v, ",")
	if len(parts) == 0 {
		return ""
	}
	return strings.Trim(strings.TrimSpace(parts[0]), "'\"")
}

func parseTextAlign(v string) qui.TextAlign {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "center":
		return qui.TextAlignCenter
	case "right", "end":
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
