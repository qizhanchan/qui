package svg

import (
	"math"

	"github.com/qizhanchan/qui"
)

// Length is a float32 with an explicit "set" flag so a zero value can be
// distinguished from "use the parent / SVG default". The whole reason
// this type exists is the SVG cascade: a Rect with no stroke-width
// declared should inherit from its <g> ancestor, but a Rect explicitly
// set to 0 should not. Plain float32 plus a sentinel like NaN works but
// is error-prone — a single forgotten math.IsNaN check silently inherits.
type Length struct {
	V   float32
	Set bool
}

// SetLength returns a Length explicitly carrying v.
func SetLength(v float32) Length { return Length{V: v, Set: true} }

// PaintKind tags whether a Paint inherits, paints nothing, or paints a
// solid color. v1 has no gradient/pattern variants — when those land,
// add new Kinds and a payload union.
type PaintKind uint8

const (
	PaintInherit PaintKind = iota // zero value — pull from parent / default
	PaintNone                     // SVG fill="none" / stroke="none"
	PaintSolid                    // Color is a solid RGBA paint
)

// Paint describes how to fill or stroke a region. v1 only supports the
// "solid color" variant beyond inherit/none; future gradient + pattern
// types will live alongside PaintKind values without changing this
// struct's name.
type Paint struct {
	Kind  PaintKind
	Color qui.Color
}

// SolidPaint is a convenience constructor for the common case.
func SolidPaint(c qui.Color) Paint { return Paint{Kind: PaintSolid, Color: c} }

// NonePaint is sugar for fill="none" / stroke="none".
func NonePaint() Paint { return Paint{Kind: PaintNone} }

// LineCap mirrors SVG's stroke-linecap values. The zero value is
// LineCapInherit so a Style with no stroke-linecap declared follows
// its ancestor.
type LineCap uint8

const (
	LineCapInherit LineCap = iota
	LineCapButt
	LineCapRound
	LineCapSquare
)

// LineJoin mirrors SVG's stroke-linejoin values.
type LineJoin uint8

const (
	LineJoinInherit LineJoin = iota
	LineJoinMiter
	LineJoinRound
	LineJoinBevel
)

// FillRule mirrors SVG's fill-rule values.
type FillRule uint8

const (
	FillRuleInherit FillRule = iota
	FillRuleNonZero
	FillRuleEvenOdd
)

// Style holds the painter state for a single element. Every field uses
// "zero = inherit" semantics so unset fields cascade naturally from the
// enclosing Group. The cascade is resolved at render time by
// resolveStyle; element constructors and parsers only ever write fields
// that were explicitly set.
//
// Dash is the one field that needs a companion HasDash flag because nil
// and len==0 carry different meanings ("inherit" vs "explicitly no
// dash"); use SetNoDash / SetDash helpers to avoid getting that wrong.
type Style struct {
	Fill          Paint
	Stroke        Paint
	StrokeWidth   Length
	Opacity       Length
	FillOpacity   Length
	StrokeOpacity Length
	LineCap       LineCap
	LineJoin      LineJoin
	MiterLimit    Length
	Dash          []float32
	HasDash       bool
	DashOffset    Length
	FillRule      FillRule
}

// SetDash records dash as the explicit dash array. Empty slice with
// HasDash=true means "no dash" (overrides parent dash); call SetNoDash
// for that explicit case.
func (s *Style) SetDash(dash []float32) {
	s.Dash = dash
	s.HasDash = true
}

// SetNoDash explicitly clears the dash, even if a parent declared one.
func (s *Style) SetNoDash() {
	s.Dash = nil
	s.HasDash = true
}

// resolvedStyle is the post-cascade view: every field has a real value,
// inheritance has been collapsed, and defaults have been applied. Only
// the renderer touches it.
type resolvedStyle struct {
	Fill          Paint
	Stroke        Paint
	StrokeWidth   float32
	Opacity       float32
	FillOpacity   float32
	StrokeOpacity float32
	LineCap       LineCap
	LineJoin      LineJoin
	MiterLimit    float32
	Dash          []float32
	DashOffset    float32
	FillRule      FillRule
}

// defaultResolvedStyle is the SVG 1.1 initial value set, applied at the
// root of the cascade. Per spec: fill=black, stroke=none, opacity=1,
// stroke-width=1, linecap=butt, linejoin=miter, miterlimit=4,
// fill-rule=nonzero. Dash is empty.
func defaultResolvedStyle() resolvedStyle {
	return resolvedStyle{
		Fill:          Paint{Kind: PaintSolid, Color: qui.Color{R: 0, G: 0, B: 0, A: 1}},
		Stroke:        Paint{Kind: PaintNone},
		StrokeWidth:   1,
		Opacity:       1,
		FillOpacity:   1,
		StrokeOpacity: 1,
		LineCap:       LineCapButt,
		LineJoin:      LineJoinMiter,
		MiterLimit:    4,
		FillRule:      FillRuleNonZero,
	}
}

// resolveStyle merges child onto parent. Anything explicitly set on
// child wins; everything else carries the parent value. Opacity is the
// one field that *multiplies* — SVG's compositing rule — even when both
// are set, the child still inherits the parent's group opacity.
func resolveStyle(parent resolvedStyle, child Style) resolvedStyle {
	r := parent
	if child.Fill.Kind != PaintInherit {
		r.Fill = child.Fill
	}
	if child.Stroke.Kind != PaintInherit {
		r.Stroke = child.Stroke
	}
	if child.StrokeWidth.Set {
		r.StrokeWidth = child.StrokeWidth.V
	}
	if child.Opacity.Set {
		// Multiplicative opacity: a child opacity of 0.5 inside a parent
		// at 0.8 paints at 0.4 total. Per SVG 1.1 §14.5.
		r.Opacity = parent.Opacity * child.Opacity.V
	}
	if child.FillOpacity.Set {
		r.FillOpacity = child.FillOpacity.V
	}
	if child.StrokeOpacity.Set {
		r.StrokeOpacity = child.StrokeOpacity.V
	}
	if child.LineCap != LineCapInherit {
		r.LineCap = child.LineCap
	}
	if child.LineJoin != LineJoinInherit {
		r.LineJoin = child.LineJoin
	}
	if child.MiterLimit.Set {
		r.MiterLimit = child.MiterLimit.V
	}
	if child.HasDash {
		r.Dash = child.Dash
	}
	if child.DashOffset.Set {
		r.DashOffset = child.DashOffset.V
	}
	if child.FillRule != FillRuleInherit {
		r.FillRule = child.FillRule
	}
	return r
}

// clampUnit clamps a value into the [0, 1] range. Used for opacity
// products where bad input shouldn't crash the renderer.
func clampUnit(v float32) float32 {
	if math.IsNaN(float64(v)) || v < 0 {
		return 0
	}
	if v > 1 {
		return 1
	}
	return v
}
