package qui

import (
	"strings"
	"testing"
)

// layoutFingerprint drops the shaped-run pointer so two layouts can be
// compared field by field.
func layoutFingerprint(l TextLayout) TextLayout {
	out := l
	out.Lines = make([]TextLayoutLine, len(l.Lines))
	for i, line := range l.Lines {
		line.shaped = nil
		out.Lines[i] = line
	}
	return out
}

func layoutsEqual(a, b TextLayout) bool {
	fa, fb := layoutFingerprint(a), layoutFingerprint(b)
	if fa.Width != fb.Width || fa.Height != fb.Height || fa.LineHeight != fb.LineHeight ||
		fa.Truncated != fb.Truncated || len(fa.Lines) != len(fb.Lines) {
		return false
	}
	for i := range fa.Lines {
		if fa.Lines[i] != fb.Lines[i] {
			return false
		}
	}
	return true
}

// A layout assembled from memoized paragraphs must equal one built cold,
// including the passes that look across paragraphs (alignment against the
// widest line, justify's last-line rule, first-line indent).
func TestSegmentCachedLayoutMatchesColdBuild(t *testing.T) {
	doc := "short\nThe quick brown fox jumps over the lazy dog again\n\nשלום עולם mixed\nend line\r\nlast"
	font := Font{Size: 14}
	cases := map[string]TextLayoutOptions{
		"nowrap-start":  {},
		"nowrap-center": {Align: TextAlignCenter},
		"nowrap-end":    {Align: TextAlignEnd},
		"wrap-justify":  {Wrap: true, MaxWidth: 120, Align: TextAlignJustify},
		"wrap-indent":   {Wrap: true, MaxWidth: 90, FirstIndent: 12, BreakLongWords: true},
		"maxlines":      {Wrap: true, MaxWidth: 90, MaxLines: 3, Ellipsis: true},
	}
	for name, opts := range cases {
		invalidateTextLayoutCache()
		cold := BuildTextLayout(doc, font, opts)
		// Warm the paragraph memo through a different document that shares
		// most paragraphs, then build the original again from the memo.
		invalidateTextLayoutCache()
		BuildTextLayout(strings.Replace(doc, "short", "SHORT!", 1), font, opts)
		warm := BuildTextLayout(doc, font, opts)
		if !layoutsEqual(cold, warm) {
			t.Errorf("%s: memoized layout differs from a cold build\ncold %+v\nwarm %+v", name, layoutFingerprint(cold), layoutFingerprint(warm))
		}
	}
	// Every option set shares one memo: a key that ignored an input would
	// serve one set's paragraphs to another.
	cold := map[string]TextLayout{}
	for name, opts := range cases {
		invalidateTextLayoutCache()
		cold[name] = BuildTextLayout(doc, font, opts)
	}
	invalidateTextLayoutCache()
	for _, opts := range cases {
		BuildTextLayout(doc+"\nx", font, opts) // fills the paragraph memo
	}
	for name, opts := range cases {
		if got := BuildTextLayout(doc, font, opts); !layoutsEqual(cold[name], got) {
			t.Errorf("%s: paragraph memo leaked across option sets", name)
		}
	}
}

// Editing one paragraph of a long document re-shapes that paragraph only.
func TestSegmentCacheReshapesOnlyEditedParagraph(t *testing.T) {
	invalidateTextLayoutCache()
	var b strings.Builder
	for i := 0; i < 200; i++ {
		b.WriteString("paragraph number ")
		b.WriteByte(byte('a' + i%26))
		b.WriteString(strings.Repeat("x", i%7))
		b.WriteByte('\n')
	}
	doc := b.String()
	font := Font{Size: 14}
	opts := TextLayoutOptions{Wrap: true, MaxWidth: 200}
	BuildTextLayout(doc, font, opts)
	textLayoutCacheMu.Lock()
	before := len(textSegmentCache)
	textLayoutCacheMu.Unlock()

	edited := strings.Replace(doc, "paragraph number c", "paragraph number cZ", 1)
	BuildTextLayout(edited, font, opts)
	textLayoutCacheMu.Lock()
	added := len(textSegmentCache) - before
	textLayoutCacheMu.Unlock()
	if added != 1 {
		t.Fatalf("editing one paragraph shaped %d new paragraphs, want 1", added)
	}
}
