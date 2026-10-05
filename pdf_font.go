package qui

import (
	"bytes"
	"fmt"
	"sort"
	"strings"
	"unicode/utf16"

	"github.com/go-text/typesetting/di"
	gtfont "github.com/go-text/typesetting/font"
	"github.com/go-text/typesetting/shaping"
	"golang.org/x/image/font"
	"golang.org/x/image/font/opentype"
	"golang.org/x/image/font/sfnt"
	"golang.org/x/image/math/fixed"
)

// PDF font embedding. Each distinct *opentype.Font used by DrawText is
// embedded once as a composite Type0 / CIDFontType2 (Identity-H) font whose
// program is a subset of the original containing only the glyphs actually
// drawn, plus a /ToUnicode CMap so copy/paste and search recover the right
// Unicode — including CJK.
//
// Run-splitting mirrors the on-screen path (renderer_gl.go faceForRune /
// drawTextLineRaw): a single DrawText string is partitioned into runs of
// consecutive runes covered by the same font in the resolved fallback
// chain, and each run is shown with that font's embedded resource.

// pdfFontSet accumulates every embedded font across all pages of a doc.
type pdfFontSet struct {
	order  []*embeddedFont
	byFont map[*opentype.Font]*embeddedFont
}

func newPDFFontSet() *pdfFontSet {
	return &pdfFontSet{byFont: map[*opentype.Font]*embeddedFont{}}
}

// glyphKind selects the PDF font-embedding strategy for a font.
type glyphKind int

const (
	// kindType0 embeds a glyf-based TrueType font as a subset Type0 /
	// CIDFontType2 (Identity-H): compact, crisp, 2-byte GIDs, no glyph
	// count limit. Used for Latin fonts and glyf-based CJK fonts.
	kindType0 glyphKind = iota
	// kindType3 embeds any font (notably CFF/OpenType, which can't be a
	// CIDFontType2) as a Type3 font whose glyphs are drawn as vector
	// outlines via sfnt.LoadGlyph. Universal + selectable (ToUnicode), but
	// single-byte codes cap each sub-font at 256 glyphs, so large glyph
	// sets split across several Type3 sub-fonts.
	kindType3
)

type embeddedFont struct {
	font       *opentype.Font
	name       string // base resource name, e.g. "F0"
	unitsPerEm int
	raw        []byte // original program bytes (for Type0 subsetting)
	kind       glyphKind

	// Type0 state.
	used map[sfnt.GlyphIndex]string // GID -> source cluster (ToUnicode)
	adv  map[sfnt.GlyphIndex]int    // GID -> advance in design units (/W)

	// Type3 state.
	t3 *type3Font
}

func (s *pdfFontSet) getOrAdd(f *opentype.Font) *embeddedFont {
	if ef, ok := s.byFont[f]; ok {
		return ef
	}
	ef := &embeddedFont{
		font:       f,
		name:       fmt.Sprintf("F%d", len(s.order)),
		unitsPerEm: int(f.UnitsPerEm()),
		used:       map[sfnt.GlyphIndex]string{},
		adv:        map[sfnt.GlyphIndex]int{},
	}
	if ef.unitsPerEm <= 0 {
		ef.unitsPerEm = 1000
	}
	ef.raw = rawFontBytesFor(f)
	ef.kind = kindType0
	if ef.raw != nil {
		if _, tables, err := parseSFNTTables(ef.raw); err == nil {
			if _, hasGlyf := tables["glyf"]; !hasGlyf {
				ef.kind = kindType3
			}
		}
	} else {
		// No recoverable bytes to subset — render outlines instead.
		ef.kind = kindType3
	}
	if ef.kind == kindType3 {
		ef.t3 = newType3Font(ef)
	}
	s.byFont[f] = ef
	s.order = append(s.order, ef)
	return ef
}

func (ef *embeddedFont) register(gid sfnt.GlyphIndex, text string, advUnits int) {
	if ef.kind == kindType3 {
		ef.t3.assign(gid, text, advUnits)
		return
	}
	if _, ok := ef.used[gid]; !ok {
		ef.used[gid] = text
		ef.adv[gid] = advUnits
	}
}

// encodeGlyph returns the PDF resource name and the hex-encoded code bytes
// for showing gid: 2-byte GID for Type0, 1-byte code for Type3.
func (ef *embeddedFont) encodeGlyph(gid sfnt.GlyphIndex) (resName, hexCode string) {
	if ef.kind == kindType3 {
		ref := ef.t3.assign(gid, "", 0) // already registered; returns existing
		return ef.t3.chunkName(ref.chunk), fmt.Sprintf("%02X", ref.code)
	}
	return ef.name, fmt.Sprintf("%04X", uint16(gid))
}

// rawFontBytesFor returns the retained bytes for f, or falls back to
// sfnt.WriteSourceTo (single-font sources), or nil.
func rawFontBytesFor(f *opentype.Font) []byte {
	if b := lookupRawFontBytes(f); b != nil {
		return b
	}
	if b, err := rawFontBytes(f); err == nil {
		return b
	}
	return nil
}

// fontChainForSpec returns the parallel *opentype.Font chain (primary +
// user fallbacks + auto fallbacks) matching GetFontFacesFor's face chain.
// This is the authoritative rune->font mapping the PDF text path uses.
func fontChainForSpec(spec Font) []*opentype.Font {
	// Ensure the font system is initialized and auto-fallbacks populated.
	GetFontFacesFor(spec)

	fontCacheMu.Lock()
	defer fontCacheMu.Unlock()
	primary := selectPrimaryFontUnlocked(spec)
	var chain []*opentype.Font
	seen := map[*opentype.Font]bool{}
	add := func(f *opentype.Font) {
		if f == nil || seen[f] {
			return
		}
		seen[f] = true
		chain = append(chain, f)
	}
	add(primary)
	for _, f := range fontFallbacks {
		add(f)
	}
	for _, f := range autoFallbacks {
		add(f)
	}
	return chain
}

// resolveRune picks the first font in the chain that has a glyph for r,
// returning that font, its GID, and the advance in font design units. If no
// font covers r, it returns the primary font with GID 0 (notdef).
func resolveRune(chain []*opentype.Font, r rune, buf *sfnt.Buffer) (*opentype.Font, sfnt.GlyphIndex, int) {
	for _, f := range chain {
		gid, err := f.GlyphIndex(buf, r)
		if err != nil || gid == 0 {
			continue
		}
		return f, gid, glyphAdvanceUnits(f, gid, buf)
	}
	if len(chain) > 0 {
		f := chain[0]
		return f, 0, glyphAdvanceUnits(f, 0, buf)
	}
	return nil, 0, 0
}

// glyphAdvanceUnits returns a glyph's advance in font design units. Using
// ppem == unitsPerEm makes the returned pixel advance equal the design-unit
// advance.
func glyphAdvanceUnits(f *opentype.Font, gid sfnt.GlyphIndex, buf *sfnt.Buffer) int {
	upem := int(f.UnitsPerEm())
	if upem <= 0 {
		upem = 1000
	}
	adv, err := f.GlyphAdvance(buf, gid, fixed.I(upem), font.HintingNone)
	if err != nil {
		return 0
	}
	return adv.Round()
}

// drawTextRuns emits one or more text-showing objects for text, matching the
// on-screen baseline placement (backend_cpu.go drawTextIntoRaw): single-line
// text is cap-height centered in rect; multi-line centers the ink block.
func (c *pdfCanvas) drawTextRuns(text string, rect Rect, color Color, fontSpec Font) {
	if text == "" || color.A <= 0 {
		return
	}
	chain := fontChainForSpec(fontSpec)
	if len(chain) == 0 {
		return
	}
	size := fontSpec.Size
	if size <= 0 {
		size = 14
	}
	face := GetFontFaceFor(fontSpec)
	m := face.Metrics()
	ascent := m.Ascent.Ceil()
	descent := m.Descent.Ceil()
	lineH := m.Height.Ceil()
	capH := m.CapHeight.Ceil()
	if capH <= 0 {
		capH = ascent - descent
	}

	c.setFill(color)

	lines := strings.Split(text, "\n")
	if len(lines) == 1 {
		baseline := rect.Y + (rect.H+float32(capH))/2
		c.emitTextLine(strings.TrimRight(lines[0], "\r"), rect.X, baseline, size, fontSpec, chain)
		return
	}
	totalInk := float32((len(lines)-1)*lineH + ascent + descent)
	extra := rect.H - totalInk
	if extra < 0 {
		extra = 0
	} else {
		extra /= 2
	}
	y := rect.Y + float32(ascent) + extra
	for _, ln := range lines {
		c.emitTextLine(strings.TrimRight(ln, "\r"), rect.X, y, size, fontSpec, chain)
		y += float32(lineH)
	}
}

// emitTextLine draws one line at the given baseline. Each rune is resolved
// to a font + glyph, registered for embedding, and encoded to (resource
// name, code bytes); consecutive runes sharing a resource are shown in one
// text object. Grouping by resource (not just font) transparently handles
// Type3 sub-font chunking.
func (c *pdfCanvas) emitTextLine(line string, x, baseline, size float32, fontSpec Font, chain []*opentype.Font) {
	if line == "" {
		return
	}
	if shaped, ok := shapeSingleLine([]rune(line), fontSpec, TextDirectionAuto); ok {
		c.emitShapedTextLine(shaped, x, baseline, fontSpec)
		return
	}
	c.emitLegacyTextLine(line, x, baseline, size, fontSpec, chain)
}

func (c *pdfCanvas) emitLegacyTextLine(line string, x, baseline, size float32, fontSpec Font, chain []*opentype.Font) {
	var buf sfnt.Buffer
	runes := []rune(line)
	letterSpacing := fontSpec.LetterSpacing

	type tok struct {
		resName string
		hex     string
		advPx   float32
	}
	toks := make([]tok, 0, len(runes))
	for _, r := range runes {
		f, gid, advUnits := resolveRune(chain, r, &buf)
		if f == nil {
			continue
		}
		ef := c.doc.fonts.getOrAdd(f)
		ef.register(gid, string(r), advUnits)
		resName, hex := ef.encodeGlyph(gid)
		advPx := float32(advUnits)*size/float32(ef.unitsPerEm) + letterSpacing
		toks = append(toks, tok{resName, hex, advPx})
	}

	i := 0
	for i < len(toks) {
		j := i + 1
		for j < len(toks) && toks[j].resName == toks[i].resName {
			j++
		}
		c.emit("BT\n/%s %s Tf\n", toks[i].resName, pdfNum(float64(size)))
		if letterSpacing != 0 {
			c.emit("%s Tc\n", pdfNum(float64(letterSpacing)))
		}
		c.emit("1 0 0 -1 %s %s Tm\n<", pdfNum(float64(x)), pdfNum(float64(baseline)))
		for _, t := range toks[i:j] {
			c.emit("%s", t.hex)
		}
		c.emit("> Tj\nET\n")
		for _, t := range toks[i:j] {
			x += t.advPx
		}
		i = j
	}
}

// pdfEmojiOversample is how many raster pixels a PDF emoji gets per
// point. PDF is resolution-independent but emoji are bitmaps: embedding
// one at 1px/pt looks soft on any retina screen or printer, so rasterize
// at 4× and let the viewer scale it down into the same box.
const pdfEmojiOversample = 4

func (c *pdfCanvas) emitShapedTextLine(line *shapedLine, x, baseline float32, fontSpec Font) {
	if line == nil {
		return
	}
	metrics := GetFontFaceFor(fontSpec).Metrics()
	capHeight := float32(metrics.CapHeight.Ceil())
	if capHeight <= 0 {
		capHeight = float32(metrics.Ascent.Ceil() - metrics.Descent.Ceil())
	}
	penX := x
	for _, runIndex := range visualRunOrder(line.runs) {
		run := &line.runs[runIndex]
		source := sourceFontForShapingFace(run.Face)
		for _, glyph := range run.Glyphs {
			if glyph.GlyphID == gtfont.EmptyGlyph {
				start := glyph.ClusterIndex
				end := start + glyph.RuneCount
				if start >= 0 && end <= len(line.text) && start < end {
					if img := lookupEmojiSequence(string(line.text[start:end]), fontSpec.Size*pdfEmojiOversample); img != nil {
						bounds := img.Bounds()
						w := float32(bounds.Dx()) / pdfEmojiOversample
						h := float32(bounds.Dy()) / pdfEmojiOversample
						adv := fixedToPx(glyph.Advance)
						c.DrawImage(img, Rect{X: penX + (adv-w)/2, Y: baseline - capHeight/2 - h/2, W: w, H: h})
					}
				}
				penX += fixedToPx(glyph.Advance)
				continue
			}
			if source == nil {
				penX += fixedToPx(glyph.Advance)
				continue
			}
			gid := sfnt.GlyphIndex(glyph.GlyphID)
			var buf sfnt.Buffer
			advUnits := glyphAdvanceUnits(source, gid, &buf)
			cluster := glyphClusterText(line.glyphText(), glyph, run.Direction)
			ef := c.doc.fonts.getOrAdd(source)
			ef.register(gid, cluster, advUnits)
			resName, hex := ef.encodeGlyph(gid)
			fontSize := fixedToPx(run.Size)
			gx := penX + fixedToPx(glyph.XOffset)
			gy := baseline - fixedToPx(glyph.YOffset)
			c.emit("BT\n/%s %s Tf\n1 0 0 -1 %s %s Tm\n<%s> Tj\nET\n",
				resName, pdfNum(float64(fontSize)), pdfNum(float64(gx)), pdfNum(float64(gy)), hex)
			penX += fixedToPx(glyph.Advance)
		}
	}
}

func glyphClusterText(text []rune, glyph shaping.Glyph, direction di.Direction) string {
	start := glyph.ClusterIndex
	end := start + glyph.RuneCount
	if start < 0 || end > len(text) || start >= end {
		return "\uFFFD"
	}
	cluster := append([]rune(nil), text[start:end]...)
	if direction == di.DirectionRTL {
		for i, j := 0, len(cluster)-1; i < j; i, j = i+1, j-1 {
			cluster[i], cluster[j] = cluster[j], cluster[i]
		}
	}
	return string(cluster)
}

// buildObjects serializes every embedded font, advancing *nextID. It returns
// the font objects plus the /Font resource-dict body (e.g. "/F0 12 0 R ").
func (s *pdfFontSet) buildObjects(nextID *int) ([]pdfObject, string, error) {
	var objs []pdfObject
	var res bytes.Buffer
	for _, ef := range s.order {
		if ef.kind == kindType3 {
			t3objs, t3res, err := ef.t3.buildObjects(nextID)
			if err != nil {
				return nil, "", err
			}
			objs = append(objs, t3objs...)
			res.WriteString(t3res)
			continue
		}
		raw := ef.raw
		if raw == nil {
			return nil, "", fmt.Errorf("qui/pdf: cannot embed font %s: no font bytes", ef.name)
		}
		program, err := subsetFontProgram(ef.font, raw, ef.used)
		if err != nil {
			return nil, "", fmt.Errorf("qui/pdf: subset font %s: %w", ef.name, err)
		}

		fontFileID := *nextID
		*nextID++
		descID := *nextID
		*nextID++
		cidID := *nextID
		*nextID++
		toUniID := *nextID
		*nextID++
		type0ID := *nextID
		*nextID++

		// FontFile2 stream.
		var ff bytes.Buffer
		fmt.Fprintf(&ff, "<< /Length %d /Length1 %d >>\nstream\n", len(program), len(program))
		ff.Write(program)
		ff.WriteString("\nendstream")
		objs = append(objs, pdfObject{fontFileID, ff.Bytes()})

		// FontDescriptor.
		asc, desc, bbox, capHeight := fontDescriptorMetrics(ef)
		descBody := fmt.Sprintf(
			"<< /Type /FontDescriptor /FontName /%s /Flags 4 /FontBBox [%d %d %d %d] /ItalicAngle 0 /Ascent %d /Descent %d /CapHeight %d /StemV 80 /FontFile2 %d 0 R >>",
			subsetTag(ef), bbox[0], bbox[1], bbox[2], bbox[3], asc, desc, capHeight, fontFileID)
		objs = append(objs, pdfObject{descID, []byte(descBody)})

		// CIDFontType2 with /W widths.
		wArr := buildWArray(ef)
		cidBody := fmt.Sprintf(
			"<< /Type /Font /Subtype /CIDFontType2 /BaseFont /%s /CIDSystemInfo << /Registry (Adobe) /Ordering (Identity) /Supplement 0 >> /FontDescriptor %d 0 R /CIDToGIDMap /Identity /W %s >>",
			subsetTag(ef), descID, wArr)
		objs = append(objs, pdfObject{cidID, []byte(cidBody)})

		// ToUnicode CMap.
		toUni := buildToUnicode(ef)
		var tu bytes.Buffer
		fmt.Fprintf(&tu, "<< /Length %d >>\nstream\n", len(toUni))
		tu.Write(toUni)
		tu.WriteString("\nendstream")
		objs = append(objs, pdfObject{toUniID, tu.Bytes()})

		// Type0 root font.
		type0Body := fmt.Sprintf(
			"<< /Type /Font /Subtype /Type0 /BaseFont /%s /Encoding /Identity-H /DescendantFonts [%d 0 R] /ToUnicode %d 0 R >>",
			subsetTag(ef), cidID, toUniID)
		objs = append(objs, pdfObject{type0ID, []byte(type0Body)})

		fmt.Fprintf(&res, "/%s %d 0 R ", ef.name, type0ID)
	}
	return objs, res.String(), nil
}

// subsetTag returns a BaseFont name with a 6-uppercase-letter subset prefix.
func subsetTag(ef *embeddedFont) string {
	// Deterministic tag derived from the font pointer identity via its used
	// glyph count + name index — good enough to be unique within a doc.
	base := "QUIFON" // fixed prefix; uniqueness across fonts comes from the name
	return base + "+" + "Font" + strings.TrimPrefix(ef.name, "F")
}

func buildWArray(ef *embeddedFont) string {
	gids := make([]int, 0, len(ef.adv))
	for g := range ef.adv {
		gids = append(gids, int(g))
	}
	sort.Ints(gids)
	var b bytes.Buffer
	b.WriteString("[ ")
	upem := ef.unitsPerEm
	for _, g := range gids {
		w := ef.adv[sfnt.GlyphIndex(g)] * 1000 / upem
		fmt.Fprintf(&b, "%d [%d] ", g, w)
	}
	b.WriteString("]")
	return b.String()
}

func buildToUnicode(ef *embeddedFont) []byte {
	type pair struct {
		gid  sfnt.GlyphIndex
		text string
	}
	pairs := make([]pair, 0, len(ef.used))
	for g, text := range ef.used {
		pairs = append(pairs, pair{g, text})
	}
	sort.Slice(pairs, func(i, j int) bool { return pairs[i].gid < pairs[j].gid })

	var b bytes.Buffer
	b.WriteString("/CIDInit /ProcSet findresource begin\n12 dict begin\nbegincmap\n")
	b.WriteString("/CIDSystemInfo << /Registry (Adobe) /Ordering (UCS) /Supplement 0 >> def\n")
	b.WriteString("/CMapName /Adobe-Identity-UCS def\n/CMapType 2 def\n")
	b.WriteString("1 begincodespacerange\n<0000> <FFFF>\nendcodespacerange\n")
	// bfchar in chunks of 100.
	for start := 0; start < len(pairs); start += 100 {
		end := start + 100
		if end > len(pairs) {
			end = len(pairs)
		}
		fmt.Fprintf(&b, "%d beginbfchar\n", end-start)
		for _, p := range pairs[start:end] {
			fmt.Fprintf(&b, "<%04X> <", uint16(p.gid))
			text := p.text
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

// fontDescriptorMetrics returns ascent, descent (negative), bbox and cap
// height in /1000 em units.
func fontDescriptorMetrics(ef *embeddedFont) (ascent, descent int, bbox [4]int, capHeight int) {
	var buf sfnt.Buffer
	upem := ef.unitsPerEm
	scale := func(v fixed.Int26_6) int { return int(v) * 1000 / (upem * 64) }
	ppem := fixed.I(upem)
	if b, err := ef.font.Bounds(&buf, ppem, font.HintingNone); err == nil {
		bbox = [4]int{scale(b.Min.X), scale(b.Min.Y), scale(b.Max.X), scale(b.Max.Y)}
	} else {
		bbox = [4]int{0, -200, 1000, 800}
	}
	if m, err := ef.font.Metrics(&buf, ppem, font.HintingNone); err == nil {
		ascent = scale(m.Ascent)
		descent = -scale(m.Descent)
		capHeight = scale(m.CapHeight)
	}
	if ascent == 0 {
		ascent = 800
	}
	if descent == 0 {
		descent = -200
	}
	if capHeight <= 0 {
		capHeight = ascent * 7 / 10
	}
	return
}

// rawFontBytes recovers the original font program bytes. For single-font
// sources this uses sfnt.WriteSourceTo; collection-backed fonts (.ttc) are
// handled by the raw-bytes registry populated at install time.
func rawFontBytes(f *opentype.Font) ([]byte, error) {
	if b := lookupRawFontBytes(f); b != nil {
		return b, nil
	}
	var buf bytes.Buffer
	if _, err := f.WriteSourceTo(nil, &buf); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}
