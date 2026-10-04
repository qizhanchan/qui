package qui

// TextSpan is an inline rich-text segment.
// When Font/Color are nil, base style is used.
type TextSpan struct {
	Text  string
	Font  *Font
	Color *Color
	// Decoration draws underline / line-through over this span (CSS
	// text-decoration). Zero value draws nothing.
	Decoration TextDecoration
	// DecorationPaint carries the span's text-decoration-color/-style/
	// -thickness overrides. Zero value = text color, solid, auto thickness.
	DecorationPaint DecorationPaint
	// Href, when non-empty, marks this span as a hyperlink. It is purely
	// semantic metadata — the CPU text rasterizer ignores it (link styling
	// is the caller's job via Color/Font) — but it is carried into the
	// HTML clipboard flavor as an <a href> so a cross-widget copy pastes
	// as a real link into Word / browsers.
	Href string
	// Image, when non-empty, marks this span as an inline image (its src /
	// URL). Like Href it's clipboard-only metadata — the rasterizer ignores
	// it — surfacing as an <img src> in the HTML flavor so copying a
	// selection that spans an inline <img> pastes the picture too.
	Image string
}

// RichTextRun is one drawable run inside a visual line.
type RichTextRun struct {
	Text            string
	Font            Font
	Color           Color
	Width           float32
	Decoration      TextDecoration
	DecorationPaint DecorationPaint
	// Href carries the source span's hyperlink target (empty for normal
	// text). It survives layout so callers can hit-test clicks against
	// links via RichTextLayout.LinkAt.
	Href   string
	shaped *shapedLine
}

// RichTextLine stores one laid out line of rich text.
type RichTextLine struct {
	Runs       []RichTextRun
	Width      float32
	Offset     float32
	LineHeight float32
	direction  TextDirection
}

// RichTextLayout is the immutable output of BuildRichTextLayout.
type RichTextLayout struct {
	Lines     []RichTextLine
	Width     float32
	Height    float32
	Truncated bool
}

type styledRune struct {
	r         rune
	font      Font
	color     Color
	deco      TextDecoration
	decoPaint DecorationPaint
	href      string
}

// BuildRichTextLayout lays out styled spans with wrapping/alignment.
func BuildRichTextLayout(spans []TextSpan, baseFont Font, baseColor Color, opts TextLayoutOptions) RichTextLayout {
	stream := flattenSpans(spans, baseFont, baseColor)
	lines, shapedOK := layoutStyledRunesShaped(stream, baseFont, opts)
	if !shapedOK {
		lines = layoutStyledRunes(stream, baseFont, opts)
	}
	if len(lines) == 0 {
		lines = append(lines, RichTextLine{
			Runs:       nil,
			Width:      0,
			Offset:     0,
			LineHeight: scaledLineHeight(baseFont, opts.LineHeightScale),
		})
	}

	truncated := false
	if opts.MaxLines > 0 && len(lines) > opts.MaxLines {
		lines = lines[:opts.MaxLines]
		truncated = true
		if opts.Ellipsis {
			ellipsizeRichTextLine(&lines[len(lines)-1], opts.MaxWidth)
		}
	}

	var maxW float32
	for _, line := range lines {
		if line.Width > maxW {
			maxW = line.Width
		}
	}
	alignW := maxW
	if opts.MaxWidth > 0 {
		alignW = opts.MaxWidth
	}
	for i := range lines {
		rtl := lines[i].direction == TextDirectionRTL
		switch opts.Align {
		case TextAlignCenter:
			lines[i].Offset = (alignW - lines[i].Width) * 0.5
		case TextAlignEnd:
			if !rtl {
				lines[i].Offset = alignW - lines[i].Width
			}
		default:
			if rtl {
				lines[i].Offset = alignW - lines[i].Width
			}
		}
		if lines[i].Offset < 0 {
			lines[i].Offset = 0
		}
	}

	var h float32
	for _, line := range lines {
		h += line.LineHeight
	}
	return RichTextLayout{
		Lines:     lines,
		Width:     maxW,
		Height:    h,
		Truncated: truncated,
	}
}

// DrawRichTextLayout draws a precomputed rich text layout.
func DrawRichTextLayout(canvas Canvas, layout RichTextLayout, origin Rect) {
	y := origin.Y
	for _, line := range layout.Lines {
		x := origin.X + line.Offset
		for _, run := range line.Runs {
			if run.Text != "" {
				runRect := Rect{
					X: x,
					Y: y,
					W: run.Width,
					H: line.LineHeight,
				}
				if shapedCanvas, ok := canvas.(shapedLineCanvas); ok && run.shaped != nil {
					shapedCanvas.drawShapedLine(run.shaped, runRect, run.Color, run.Font)
				} else {
					canvas.DrawText(run.Text, runRect, run.Color, run.Font)
				}
				if run.Decoration != 0 {
					drawDecorationRun(canvas, run.Decoration, x, run.Width, y, line.LineHeight, run.Color, run.Font, run.DecorationPaint)
				}
			}
			x += run.Width
		}
		y += line.LineHeight
	}
}

// LinkAt returns the Href of the hyperlink run under pt, given the same origin
// the layout was drawn at (the rect passed to DrawRichTextLayout /
// DrawTextSpans). It mirrors DrawRichTextLayout's line/run geometry so hit
// results line up pixel-for-pixel with what was painted. ok is false when pt
// lands on non-link text or outside the laid-out glyphs.
func (l RichTextLayout) LinkAt(origin Rect, pt Point) (href string, ok bool) {
	y := origin.Y
	for _, line := range l.Lines {
		if pt.Y >= y && pt.Y < y+line.LineHeight {
			x := origin.X + line.Offset
			for _, run := range line.Runs {
				if pt.X >= x && pt.X < x+run.Width {
					if run.Href != "" {
						return run.Href, true
					}
					return "", false
				}
				x += run.Width
			}
			return "", false
		}
		y += line.LineHeight
	}
	return "", false
}

// DrawTextSpans builds and draws rich text in one call.
func DrawTextSpans(canvas Canvas, spans []TextSpan, rect Rect, baseFont Font, baseColor Color, opts TextLayoutOptions) RichTextLayout {
	layout := BuildRichTextLayout(spans, baseFont, baseColor, opts)
	DrawRichTextLayout(canvas, layout, rect)
	return layout
}

func flattenSpans(spans []TextSpan, baseFont Font, baseColor Color) []styledRune {
	if len(spans) == 0 {
		return nil
	}
	stream := make([]styledRune, 0, 128)
	for _, span := range spans {
		font := baseFont
		if span.Font != nil {
			font = *span.Font
		}
		color := baseColor
		if span.Color != nil {
			color = *span.Color
		}
		for _, r := range span.Text {
			stream = append(stream, styledRune{r: r, font: font, color: color, deco: span.Decoration, decoPaint: span.DecorationPaint, href: span.Href})
		}
	}
	return stream
}

func layoutStyledRunes(stream []styledRune, baseFont Font, opts TextLayoutOptions) []RichTextLine {
	if len(stream) == 0 {
		return nil
	}
	items := make([]InlineItem, 0, 8)
	for _, styled := range stream {
		if len(items) > 0 {
			last := &items[len(items)-1]
			if fontStyleEqual(last.Font, styled.font) && last.Color == styled.color &&
				last.Decoration == styled.deco && last.DecorationPaint == styled.decoPaint && last.Href == styled.href {
				last.Text += string(styled.r)
				continue
			}
		}
		items = append(items, InlineItem{
			Text:            string(styled.r),
			Font:            styled.font,
			Color:           styled.color,
			Decoration:      styled.deco,
			DecorationPaint: styled.decoPaint,
			Href:            styled.href,
		})
	}
	inline := BuildInlineLayout(items, InlineLayoutOptions{
		MaxWidth:        opts.MaxWidth,
		Wrap:            opts.Wrap,
		Direction:       opts.Direction,
		LineHeightScale: opts.LineHeightScale,
		BaseFont:        baseFont,
		FirstIndent:     opts.FirstIndent,
		BreakLongWords:  opts.BreakLongWords,
	})
	lines := make([]RichTextLine, 0, len(inline.Lines))
	for _, inlineLine := range inline.Lines {
		line := RichTextLine{LineHeight: inlineLine.Height}
		for _, frag := range inlineLine.Frags {
			if frag.Box || frag.Tab || frag.Text == "" {
				continue
			}
			line.Runs = append(line.Runs, RichTextRun{
				Text:            frag.Text,
				Font:            frag.Font,
				Color:           frag.Color,
				Width:           frag.Rect.W,
				Decoration:      frag.Decoration,
				DecorationPaint: frag.DecorationPaint,
				Href:            frag.Href,
			})
			line.Width += frag.Rect.W
		}
		if line.LineHeight <= 0 {
			line.LineHeight = scaledLineHeight(baseFont, opts.LineHeightScale)
		}
		lines = append(lines, line)
	}
	return lines
}

// scaledLineHeight resolves one run's line-box height.
//
// scale is a CSS `line-height`: a multiple of the FONT SIZE, not of the
// face's own line height. Every caller means it that way — htmlcss hands
// over the computed `line-height` property (a length is stored as px ÷
// font-size), the widgets copy that through, and q-word derives it from
// Docs, whose "1.15 spacing" is `line-height:1.38` in its own clipboard CSS
// against `font-size:10pt`.
//
// Multiplying the FACE's natural height instead — which is what this did —
// inflated every line box by that height, ≈1.16 em for Arial and Times. It
// was invisible in isolation (text just looked airy) and obvious in
// aggregate: q-word fitted 38 lines on a page where Docs fits 46, and every
// htmlcss page rendered `line-height: 1.5` as 1.74.
//
// scale <= 0 is CSS `line-height: normal` — the face's own height.
func scaledLineHeight(fontSpec Font, scale float32) float32 {
	return lineBox(fontSpec, scale, 0)
}

// lineBox resolves a run's line-box height from the TWO different units
// callers legitimately express it in. They are not interchangeable, and
// treating them as one number is what this function exists to stop.
//
//   - cssScale is a CSS `line-height`: a multiple of the FONT SIZE.
//     htmlcss hands over the computed property verbatim (a length is stored
//     as px ÷ font-size), and the widgets copy that through.
//
//   - lines is a WORD PROCESSOR's line spacing: a multiple of the font's own
//     natural line height, i.e. of "one line". That is what DOCX's
//     `w:line` with `lineRule="auto"` counts in 240ths of, and what Word's
//     and Docs' spacing dropdowns mean. Measured in Google Docs
//     (2026-07-28): Single at 10pt Arial puts 60 lines in an A4 text band
//     of 931px — 15.5px a line, which is Arial's natural height, NOT the
//     16px that `1.2 × font-size` would give.
//
//     Docs' own clipboard CSS says `line-height:1.2` for Single, because
//     exporting to CSS forces it to guess an em value for "one line". That
//     export is lossy and does not match what Docs renders — a trap worth
//     naming, since it is where the mistaken 1.2 constant came from.
//
// lines wins when set; otherwise cssScale; otherwise CSS `line-height:
// normal`, the face's own height.
func lineBox(fontSpec Font, cssScale, lines float32) float32 {
	// An unset Size means 14 px everywhere else in the font stack
	// (GetFontFacesFor) — apply the same default here so the font-size
	// branch agrees, instead of silently ignoring cssScale (0 × scale = 0).
	// BuildTextLayout's memo key normalizes Size the same way; the two must
	// stay in lockstep or Size 0 and Size 14 share a cache entry while
	// producing different layouts.
	if fontSpec.Size <= 0 {
		fontSpec.Size = 14
	}
	if lines > 0 {
		if h := NaturalLineHeight(fontSpec) * lines; h > 0 {
			return h
		}
	}
	if cssScale > 0 {
		if h := fontSpec.Size * cssScale; h > 0 {
			return h
		}
	}
	return NaturalLineHeight(fontSpec)
}

// NaturalLineHeight is the face's own line height (CSS `line-height:
// normal`): ascent + descent + line gap, as the font declares them. It is
// the unit a word processor's line spacing counts in — "one line".
//
// It is deliberately NOT rounded to whole pixels. Rounding up cost Arial at
// 10pt half a pixel a line, which is two lines a page — enough to disagree
// with Docs on where a page breaks.
func NaturalLineHeight(fontSpec Font) float32 {
	return float32(GetFontFaceFor(fontSpec).Metrics().Height) / 64
}

func ellipsizeRichTextLine(line *RichTextLine, maxWidth float32) {
	if line == nil {
		return
	}
	if len(line.Runs) == 0 {
		return
	}
	last := line.Runs[len(line.Runs)-1]
	ellipsisWidth, _ := TextMetrics("…", last.Font)
	if maxWidth > 0 && ellipsisWidth >= maxWidth {
		line.Runs = []RichTextRun{{Text: "…", Font: last.Font, Color: last.Color, Width: ellipsisWidth}}
		line.Width = ellipsisWidth
		return
	}

	kept := make([]RichTextRun, 0, len(line.Runs))
	var w float32
	for _, run := range line.Runs {
		runes := []rune(run.Text)
		boundaries := GraphemeBoundaries(runes)
		keepN := 0
		for i := 1; i < len(boundaries); i++ {
			candidateWidth, _ := TextMetrics(string(runes[:boundaries[i]]), run.Font)
			if maxWidth > 0 && w+candidateWidth+ellipsisWidth > maxWidth {
				break
			}
			keepN = boundaries[i]
		}
		if keepN > 0 {
			width, _ := TextMetrics(string(runes[:keepN]), run.Font)
			shaped, _ := shapeSingleLine(runes[:keepN], run.Font, TextDirectionAuto)
			kept = append(kept, RichTextRun{
				Text:   string(runes[:keepN]),
				Font:   run.Font,
				Color:  run.Color,
				Width:  width,
				shaped: shaped,
			})
			w += width
		}
		if keepN < len(runes) {
			break
		}
	}
	if len(kept) == 0 {
		kept = append(kept, RichTextRun{Text: "…", Font: last.Font, Color: last.Color, Width: ellipsisWidth})
		line.Runs = kept
		line.Width = ellipsisWidth
		return
	}
	kept[len(kept)-1].Text += "…"
	kept[len(kept)-1].Width += ellipsisWidth
	kept[len(kept)-1].shaped, _ = shapeSingleLine([]rune(kept[len(kept)-1].Text), kept[len(kept)-1].Font, TextDirectionAuto)
	line.Runs = kept
	line.Width = w + ellipsisWidth
}
