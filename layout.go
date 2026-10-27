package qui

// Layout positions child widgets inside a container.
type Layout interface {
	Apply(children []Widget, bounds Rect)
}

// LayoutMeasurer is the optional half of a Layout engine: it reports the
// intrinsic content size (excluding the container's own padding) that
// children laid out by the engine need within avail. A non-positive avail
// axis means "unconstrained". Container.Measure uses it, so a custom engine
// that implements it sizes correctly inside a ScrollView, Popup or any
// content-sized parent; one that doesn't measures as "all available space".
// FlexLayout, GridLayout and FlowLayout implement it (by value, so pointers
// to them do too).
type LayoutMeasurer interface {
	Measure(children []Widget, avail Size) Size
}

// Direction indicates layout flow.
type Direction int

const (
	Horizontal Direction = iota
	Vertical
)

// Justify controls how the main axis distributes free space among
// flex items (CSS justify-content semantics).
type Justify int

const (
	JustifyStart        Justify = iota // default — items at start, Gap between
	JustifyCenter                      // whole group centered
	JustifyEnd                         // pushed to end
	JustifySpaceBetween                // first at start, last at end, equal gaps between (overrides Gap)
	JustifySpaceAround                 // half-gap at each end, equal gaps between (overrides Gap)
	JustifySpaceEvenly                 // equal gaps everywhere including ends (overrides Gap)
)

// AlignContent controls how a wrapping FlexLayout distributes its LINES
// along the cross axis when the container has more cross space than the
// lines occupy (CSS align-content). Zero value packs lines at the start —
// today's behavior — so existing wrap layouts are unaffected. CSS's
// initial value for flex containers is stretch; callers wanting spec
// behavior opt in with AlignContentStretch (htmlcss does).
type AlignContent int

const (
	AlignContentStart        AlignContent = iota // lines packed at cross start (default)
	AlignContentCenter                           // line block centered
	AlignContentEnd                              // lines packed at cross end
	AlignContentSpaceBetween                     // first/last lines at edges, equal gaps between
	AlignContentSpaceAround                      // half-gap at each end (adds to CrossGap)
	AlignContentSpaceEvenly                      // equal gaps everywhere including ends
	AlignContentStretch                          // free space grows every line equally
)

// AlignCross controls how items align on the cross axis (CSS align-items
// for container / align-self for per-item override via FlexItem.Align).
type AlignCross int

const (
	AlignDefault AlignCross = iota // per-item only: "inherit container AlignItems"
	AlignStart
	AlignCenter
	AlignEnd
	AlignStretch // fill the cross axis (default for items without explicit Align)
)

type minSized interface {
	MinSize() Size
}

type maxSized interface {
	MaxSize() Size
}

type preferredSized interface {
	PreferredSize() Size
}

// Style-vs-interface precedence for size constraints:
//   The interface method (BaseWidget.MinSize/MaxSize/PreferredSize,
//   populated by SetMinSize/SetMaxSize/SetPreferredSize) is primary.
//   Style.MinWidth/MinHeight/MaxWidth/MaxHeight/Width/Height fills in
//   PER AXIS when the interface returned 0 on that axis, so a caller
//   can declare "min height in Style" without disturbing an
//   interface-supplied MinWidth.

func widgetMinSize(w Widget) Size {
	var min Size
	if ms, ok := w.(minSized); ok {
		min = ms.MinSize()
	}
	if min.W < 0 {
		min.W = 0
	}
	if min.H < 0 {
		min.H = 0
	}
	if s := w.Style(); s != nil {
		if min.W == 0 && s.MinWidth > 0 {
			min.W = s.MinWidth
		}
		if min.H == 0 && s.MinHeight > 0 {
			min.H = s.MinHeight
		}
	}
	return min
}

// widgetMaxSize returns the widget's max-size cap. Zero on an axis
// means "unconstrained" — callers should skip the cap on that axis.
func widgetMaxSize(w Widget) Size {
	var max Size
	if ms, ok := w.(maxSized); ok {
		max = ms.MaxSize()
	}
	if max.W < 0 {
		max.W = 0
	}
	if max.H < 0 {
		max.H = 0
	}
	if s := w.Style(); s != nil {
		if max.W == 0 && s.MaxWidth > 0 {
			max.W = s.MaxWidth
		}
		if max.H == 0 && s.MaxHeight > 0 {
			max.H = s.MaxHeight
		}
	}
	return max
}

// widgetPreferredSize returns the widget's preferred-size override.
// Zero on an axis means "no override" — callers should fall back to
// Measure() on that axis. Style.Width/Height serve as the declarative
// equivalent to SetPreferredSize.
func widgetPreferredSize(w Widget) Size {
	var p Size
	if ps, ok := w.(preferredSized); ok {
		p = ps.PreferredSize()
	}
	if p.W < 0 {
		p.W = 0
	}
	if p.H < 0 {
		p.H = 0
	}
	if s := w.Style(); s != nil {
		if p.W == 0 && s.Width > 0 {
			p.W = s.Width
		}
		if p.H == 0 && s.Height > 0 {
			p.H = s.Height
		}
	}
	return p
}

// applyMinSize floors a size by the per-axis minimums.
func applyMinSize(size, min Size) Size {
	if size.W < min.W {
		size.W = min.W
	}
	if size.H < min.H {
		size.H = min.H
	}
	return size
}

// applyMaxSize caps a size by the per-axis maximums. A zero on an axis
// means "unconstrained" — only positive maxes clamp.
func applyMaxSize(size, max Size) Size {
	if max.W > 0 && size.W > max.W {
		size.W = max.W
	}
	if max.H > 0 && size.H > max.H {
		size.H = max.H
	}
	return size
}

// applyConstraints floors by min then caps by max in one call. Used
// every place a layout engine takes a raw Measure result and turns it
// into the size passed to child.Layout.
func applyConstraints(size, min, max Size) Size {
	return applyMaxSize(applyMinSize(size, min), max)
}

// resolvedMinSize / resolvedMaxSize are the containing-block-aware forms
// of widgetMinSize / widgetMaxSize: they additionally resolve the CSS
// percentage min/max fields (Style.Min*Pct / Max*Pct, e.g. `max-width:
// 100%`) against cb, the containing block's content box on each axis.
// The absolute Min*/Max* fields still win when set; percentages only
// fill an axis the absolute size left at zero. Only positive cb axes
// resolve (a zero-height containing block leaves height-% unresolved).
// pctWidth / pctHeight resolve a percentage size against the containing
// block on that axis, adding the content-box extra (Style.PctExtra).
func (s *Style) pctWidth(frac, cbW float32) float32  { return cbW*frac + s.PctExtra.W }
func (s *Style) pctHeight(frac, cbH float32) float32 { return cbH*frac + s.PctExtra.H }

func resolvedMinSize(w Widget, cb Size) Size {
	min := widgetMinSize(w)
	if s := w.Style(); s != nil {
		if min.W == 0 && s.MinWidthPct > 0 && cb.W > 0 {
			min.W = s.pctWidth(s.MinWidthPct, cb.W)
		}
		if min.H == 0 && s.MinHeightPct > 0 && cb.H > 0 {
			min.H = s.pctHeight(s.MinHeightPct, cb.H)
		}
	}
	return min
}

func resolvedMaxSize(w Widget, cb Size) Size {
	max := widgetMaxSize(w)
	if s := w.Style(); s != nil {
		if max.W == 0 && s.MaxWidthPct > 0 && cb.W > 0 {
			max.W = s.pctWidth(s.MaxWidthPct, cb.W)
		}
		if max.H == 0 && s.MaxHeightPct > 0 && cb.H > 0 {
			max.H = s.pctHeight(s.MaxHeightPct, cb.H)
		}
	}
	return max
}

// measureWithConstraints is the canonical pipeline for "what size
// does this child want here?". It calls Measure with the available
// budget, overlays any PreferredSize axes, then applies min/max. Use
// this anywhere a layout pass currently calls Measure + applyMinSize.
func measureWithConstraints(w Widget, avail Size) Size {
	p := widgetPreferredSize(w)
	min, max := widgetMinSize(w), widgetMaxSize(w)
	m := MeasureChild(w, ownWidthAvail(avail, p.W, min.W, max.W))
	if p.W > 0 {
		m.W = p.W
	}
	if p.H > 0 {
		m.H = p.H
	}
	return applyConstraints(m, min, max)
}

// ownWidthAvail narrows avail to the width a widget will be laid out at when
// its own constraints fix that width — an explicit width (clamped to
// min/max), or a max-width below the space offered. Its content must be
// measured at that width: text wrapped into a 440px dialog is taller than
// the same text measured across the whole window, and keeping the wide
// measurement's height would clip it (CSS sizes a box's content at the box's
// own used width).
func ownWidthAvail(avail Size, prefW, minW, maxW float32) Size {
	w := float32(0)
	switch {
	case prefW > 0:
		w = prefW
		if maxW > 0 && w > maxW {
			w = maxW
		}
		if w < minW {
			w = minW
		}
	case maxW > 0 && (avail.W <= 0 || maxW < avail.W):
		w = maxW
	}
	if w > 0 && w != avail.W {
		avail.W = w
	}
	return avail
}

// MeasureConstrained answers "what size does this widget want inside a box
// of avail?" the way the layout engines do: Measure, then any explicit
// PreferredSize / Style Width-Height (percentages resolved against avail),
// then min/max. Code that positions a widget ITSELF instead of going
// through a layout engine — overlay hosts (portals, popovers) above all —
// must measure through this, or an explicitly sized child (a CSS-sized
// dialog, say) silently keeps whatever its own Measure returned.
func MeasureConstrained(w Widget, avail Size) Size {
	p := widgetPreferredSize(w)
	if s := w.Style(); s != nil {
		if p.W == 0 && s.WidthPct > 0 && avail.W > 0 {
			p.W = s.pctWidth(s.WidthPct, avail.W)
		}
		if p.H == 0 && s.HeightPct > 0 && avail.H > 0 {
			p.H = s.pctHeight(s.HeightPct, avail.H)
		}
	}
	min, max := resolvedMinSize(w, avail), resolvedMaxSize(w, avail)
	m := MeasureChild(w, ownWidthAvail(avail, p.W, min.W, max.W))
	if p.W > 0 {
		m.W = p.W
	}
	if p.H > 0 {
		m.H = p.H
	}
	return applyConstraints(m, min, max)
}

// ----------------------------------------------------------------------
// Grid — CSS-grid-like track sizing
//
// Columns and Rows are now described by per-track specs rather than
// simple counts. Each track can be:
//
//   - GridFixed:    exact pixel width  (Size field)
//   - GridFraction: share of remaining space, weight = Size  (the "fr" unit)
//   - GridAuto:     max-content — Measure children in this track, take max
//
// Children may either accept auto row-major placement or opt into a
// specific cell via GridItem { Col, Row, ColSpan, RowSpan }.

// ----------------------------------------------------------------------
// AbsoluteLayout — anchor-based positioning.
//
// Each child specifies which parent edges to pin to via Anchor (a
// bitmask), distances from those edges, and optional Width / Height
// when not fully constrained by opposing anchors.
//
// Typical patterns:
//
//   - Pinned top-right corner (tooltip / badge): Anchor = Top|Right
//   - Full-height sidebar on left: Anchor = Left|Top|Bottom, Width = 240
//   - Stretch to fill parent: Anchor = Left|Right|Top|Bottom
//   - Centered dialog: skip this layout — use Dialog's built-in centering
