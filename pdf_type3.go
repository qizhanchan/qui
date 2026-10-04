package qui

import (
	"bytes"
	"fmt"
	"unicode/utf16"

	"golang.org/x/image/font"
	"golang.org/x/image/font/opentype"
	"golang.org/x/image/font/sfnt"
	"golang.org/x/image/math/fixed"
)

// Type3 font embedding: draws each used glyph as a vector outline (via
// sfnt.LoadGlyph) inside a PDF Type3 CharProc. Works for any font flavor —
// notably CFF/OpenType, which cannot be embedded as a CIDFontType2 — while
// staying selectable/searchable through a /ToUnicode CMap. Single-byte
// Type3 codes cap each sub-font at 256 glyphs, so large glyph sets spill
// into additional sub-fonts (chunks).

type type3Font struct {
	ef     *embeddedFont
	chunks []*type3Chunk
	codeOf map[sfnt.GlyphIndex]t3ref
}

type t3ref struct{ chunk, code int }

type type3Chunk struct {
	gids  []sfnt.GlyphIndex
	texts []string
	advs  []int // design units
}

func newType3Font(ef *embeddedFont) *type3Font {
	return &type3Font{ef: ef, codeOf: map[sfnt.GlyphIndex]t3ref{}}
}

// assign returns the (chunk, code) for gid, allocating a new code the first
// time a glyph is seen. r/advUnits are recorded on first assignment; later
// calls (from encodeGlyph) pass zero and are ignored.
func (t *type3Font) assign(gid sfnt.GlyphIndex, text string, advUnits int) t3ref {
	if ref, ok := t.codeOf[gid]; ok {
		return ref
	}
	if len(t.chunks) == 0 || len(t.chunks[len(t.chunks)-1].gids) >= 256 {
		t.chunks = append(t.chunks, &type3Chunk{})
	}
	ci := len(t.chunks) - 1
	c := t.chunks[ci]
	code := len(c.gids)
	c.gids = append(c.gids, gid)
	c.texts = append(c.texts, text)
	c.advs = append(c.advs, advUnits)
	ref := t3ref{chunk: ci, code: code}
	t.codeOf[gid] = ref
	return ref
}

func (t *type3Font) chunkName(chunk int) string {
	return fmt.Sprintf("%sc%d", t.ef.name, chunk)
}

// buildObjects serializes every chunk as a Type3 font.
func (t *type3Font) buildObjects(nextID *int) ([]pdfObject, string, error) {
	var objs []pdfObject
	var res bytes.Buffer
	f := t.ef.font
	upem := t.ef.unitsPerEm
	bbox := fontBBoxUnits(f, upem)
	fm := pdfNum(1.0 / float64(upem))

	for ci, c := range t.chunks {
		var procIDs []int
		for i := range c.gids {
			procID := *nextID
			*nextID++
			procIDs = append(procIDs, procID)
			stream := glyphProcStream(f, c.gids[i], upem, c.advs[i], bbox)
			var b bytes.Buffer
			fmt.Fprintf(&b, "<< /Length %d >>\nstream\n", len(stream))
			b.Write(stream)
			b.WriteString("\nendstream")
			objs = append(objs, pdfObject{procID, b.Bytes()})
		}

		toUniID := *nextID
		*nextID++
		toUni := type3ToUnicode(c)
		var tu bytes.Buffer
		fmt.Fprintf(&tu, "<< /Length %d >>\nstream\n", len(toUni))
		tu.Write(toUni)
		tu.WriteString("\nendstream")
		objs = append(objs, pdfObject{toUniID, tu.Bytes()})

		fontID := *nextID
		*nextID++

		var charProcs bytes.Buffer
		charProcs.WriteString("<< ")
		for i, id := range procIDs {
			fmt.Fprintf(&charProcs, "/g%d %d 0 R ", i, id)
		}
		charProcs.WriteString(">>")

		var diffs bytes.Buffer
		diffs.WriteString("[ 0")
		for i := range c.gids {
			fmt.Fprintf(&diffs, " /g%d", i)
		}
		diffs.WriteString(" ]")

		var widths bytes.Buffer
		widths.WriteString("[ ")
		for _, a := range c.advs {
			fmt.Fprintf(&widths, "%d ", a)
		}
		widths.WriteString("]")

		fontBody := fmt.Sprintf(
			"<< /Type /Font /Subtype /Type3 /FontBBox [%d %d %d %d] /FontMatrix [%s 0 0 %s 0 0] /CharProcs %s /Encoding << /Type /Encoding /Differences %s >> /FirstChar 0 /LastChar %d /Widths %s /ToUnicode %d 0 R /Resources << >> >>",
			bbox[0], bbox[1], bbox[2], bbox[3], fm, fm, charProcs.String(), diffs.String(), len(c.gids)-1, widths.String(), toUniID)
		objs = append(objs, pdfObject{fontID, []byte(fontBody)})

		fmt.Fprintf(&res, "/%s %d 0 R ", t.chunkName(ci), fontID)
	}
	return objs, res.String(), nil
}

// glyphProcStream renders one glyph as a Type3 CharProc: a d1 width/bbox
// declaration followed by the filled outline (Y-flipped into PDF glyph
// space, which is Y-up).
func glyphProcStream(f *opentype.Font, gid sfnt.GlyphIndex, upem, advUnits int, bbox [4]int) []byte {
	var b bytes.Buffer
	fmt.Fprintf(&b, "%d 0 %d %d %d %d d1\n", advUnits, bbox[0], bbox[1], bbox[2], bbox[3])

	var buf sfnt.Buffer
	segs, err := f.LoadGlyph(&buf, gid, fixed.I(upem), nil)
	if err != nil || len(segs) == 0 {
		return b.Bytes() // blank glyph (e.g. space) — width only
	}
	u := func(v fixed.Int26_6) float64 { return float64(v) / 64.0 }
	// PDF glyph space is Y-up; sfnt outline Y increases down — negate Y.
	var cx, cy float64 // current point
	for _, s := range segs {
		switch s.Op {
		case sfnt.SegmentOpMoveTo:
			cx, cy = u(s.Args[0].X), -u(s.Args[0].Y)
			fmt.Fprintf(&b, "%s %s m\n", pdfNum(cx), pdfNum(cy))
		case sfnt.SegmentOpLineTo:
			cx, cy = u(s.Args[0].X), -u(s.Args[0].Y)
			fmt.Fprintf(&b, "%s %s l\n", pdfNum(cx), pdfNum(cy))
		case sfnt.SegmentOpQuadTo:
			qx, qy := u(s.Args[0].X), -u(s.Args[0].Y)
			ex, ey := u(s.Args[1].X), -u(s.Args[1].Y)
			// Quadratic -> cubic.
			c1x := cx + 2.0/3.0*(qx-cx)
			c1y := cy + 2.0/3.0*(qy-cy)
			c2x := ex + 2.0/3.0*(qx-ex)
			c2y := ey + 2.0/3.0*(qy-ey)
			fmt.Fprintf(&b, "%s %s %s %s %s %s c\n", pdfNum(c1x), pdfNum(c1y), pdfNum(c2x), pdfNum(c2y), pdfNum(ex), pdfNum(ey))
			cx, cy = ex, ey
		case sfnt.SegmentOpCubeTo:
			c1x, c1y := u(s.Args[0].X), -u(s.Args[0].Y)
			c2x, c2y := u(s.Args[1].X), -u(s.Args[1].Y)
			ex, ey := u(s.Args[2].X), -u(s.Args[2].Y)
			fmt.Fprintf(&b, "%s %s %s %s %s %s c\n", pdfNum(c1x), pdfNum(c1y), pdfNum(c2x), pdfNum(c2y), pdfNum(ex), pdfNum(ey))
			cx, cy = ex, ey
		}
	}
	b.WriteString("f\n")
	return b.Bytes()
}

func type3ToUnicode(c *type3Chunk) []byte {
	var b bytes.Buffer
	b.WriteString("/CIDInit /ProcSet findresource begin\n12 dict begin\nbegincmap\n")
	b.WriteString("/CIDSystemInfo << /Registry (Adobe) /Ordering (UCS) /Supplement 0 >> def\n")
	b.WriteString("/CMapName /Adobe-Identity-UCS def\n/CMapType 2 def\n")
	b.WriteString("1 begincodespacerange\n<00> <FF>\nendcodespacerange\n")
	for start := 0; start < len(c.texts); start += 100 {
		end := start + 100
		if end > len(c.texts) {
			end = len(c.texts)
		}
		fmt.Fprintf(&b, "%d beginbfchar\n", end-start)
		for i := start; i < end; i++ {
			fmt.Fprintf(&b, "<%02X> <", i)
			text := c.texts[i]
			if text == "" {
				text = "\uFFFD"
			}
			for _, u := range utf16.Encode([]rune(text)) {
				fmt.Fprintf(&b, "%04X", u)
			}
			b.WriteString(">\n")
		}
		b.WriteString("endbfchar\n")
	}
	b.WriteString("endcmap\nCMapName currentdict /CMap defineresource pop\nend\nend")
	return b.Bytes()
}

// fontBBoxUnits returns the font bounding box in design units.
func fontBBoxUnits(f *opentype.Font, upem int) [4]int {
	var buf sfnt.Buffer
	if b, err := f.Bounds(&buf, fixed.I(upem), font.HintingNone); err == nil {
		return [4]int{
			int(b.Min.X) / 64, -int(b.Max.Y) / 64,
			int(b.Max.X) / 64, -int(b.Min.Y) / 64,
		}
	}
	return [4]int{0, -upem / 5, upem, upem * 4 / 5}
}
