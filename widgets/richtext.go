package widgets

import . "github.com/qizhanchan/qui"

// RichText is a selectable, link-aware rich-text widget: a span-first
// wrapper over Label. It renders a sequence of styled TextSpans with
// paragraph layout (wrap / align / line-height), participates in the
// window-level cross-widget text selection (drag to select, Cmd/Ctrl+C to
// copy), and opens hyperlink spans (TextSpan.Href) in the browser on click.
//
// Selection, highlight, copy, and link handling all come from Label — the
// app never writes any of that logic:
//
//	root.AddChild(widgets.NewRichText(
//		qui.TextSpan{Text: "See the "},
//		qui.TextSpan{Text: "docs", Color: &blue, Href: "https://example.com"},
//	).Wrap(true))
type RichText struct {
	Label
}

// NewRichText builds a wrapping, start-aligned, selectable RichText from
// the given spans.
func NewRichText(spans ...TextSpan) *RichText {
	rt := &RichText{Label: *NewLabel("")}
	rt.SetSelf(rt)
	// LineHeightScale 0 = CSS `line-height: normal` (the face's own
	// height), which is what this widget has always rendered.
	rt.Paragraph = ParagraphStyle{Wrap: true, Align: TextAlignStart, BreakLongWords: true}
	rt.Selectable = true
	rt.SetSpans(spans)
	return rt
}

// HitTest returns the RichText itself (not the embedded Label) so event
// dispatch and the parent chain see the outer pointer registered via
// SetSelf.
func (rt *RichText) HitTest(p Point) Widget {
	if rt.Bounds().Contains(p) {
		return rt
	}
	return nil
}

// Wrap toggles word wrapping and returns the widget for chaining.
func (rt *RichText) Wrap(on bool) *RichText {
	rt.Paragraph.Wrap = on
	rt.InvalidateLayout()
	return rt
}

// Align sets the paragraph alignment and returns the widget for chaining.
func (rt *RichText) Align(a TextAlign) *RichText {
	rt.Paragraph.Align = a
	rt.InvalidateLayout()
	return rt
}

// LineHeight scales the line height (1 == font default) and returns the
// widget for chaining.
func (rt *RichText) LineHeight(scale float32) *RichText {
	rt.Paragraph.LineHeightScale = scale
	rt.InvalidateLayout()
	return rt
}

// MaxLines caps the rendered line count; with ellipsis the last line is
// truncated with "…". Returns the widget for chaining.
func (rt *RichText) MaxLines(n int, ellipsis bool) *RichText {
	rt.Paragraph.MaxLines = n
	rt.Paragraph.Ellipsis = ellipsis
	rt.InvalidateLayout()
	return rt
}
