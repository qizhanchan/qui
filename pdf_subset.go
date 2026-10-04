package qui

import (
	"sync"

	"golang.org/x/image/font/opentype"
	"golang.org/x/image/font/sfnt"
)

// fontRawBytes retains the original font program bytes for every parsed
// font, keyed by the parsed *opentype.Font. It is the reliable path for PDF
// embedding: sfnt.WriteSourceTo cannot recover bytes for a font extracted
// from a .ttc/.otc collection, so font.go records the raw bytes (the whole
// file, including the collection) at parse time.
//
// A dedicated mutex (not fontCacheMu) keeps this independent of the font
// registry's locking, since parseFirstFont is called both with and without
// fontCacheMu held.
var (
	fontRawBytesMu sync.Mutex
	fontRawBytes   = map[*opentype.Font][]byte{}
)

// rememberRawFontBytes records the raw program bytes for a parsed font.
func rememberRawFontBytes(f *opentype.Font, data []byte) {
	if f == nil || len(data) == 0 {
		return
	}
	fontRawBytesMu.Lock()
	fontRawBytes[f] = data
	fontRawBytesMu.Unlock()
}

// lookupRawFontBytes returns the retained raw bytes for f, or nil.
func lookupRawFontBytes(f *opentype.Font) []byte {
	fontRawBytesMu.Lock()
	defer fontRawBytesMu.Unlock()
	return fontRawBytes[f]
}

// subsetFontProgram returns a TrueType program suitable for /FontFile2
// containing (at least) the glyphs in `used`. For a TrueType (glyf) font it
// produces a real subset: only the used glyphs (plus composite-glyph
// components) carry outlines, while GIDs are preserved so /CIDToGIDMap
// /Identity stays valid. For a .ttc collection it first extracts face 0.
// Non-glyf (CFF/OpenType) fonts fall back to embedding the standalone face.
func subsetFontProgram(f *opentype.Font, raw []byte, used map[sfnt.GlyphIndex]string) ([]byte, error) {
	version, tables, err := parseSFNTTables(raw)
	if err != nil {
		return nil, err
	}
	keep := map[uint16]bool{0: true}
	for g := range used {
		keep[uint16(g)] = true
	}
	out, err := subsetTrueType(version, tables, keep)
	if err != nil {
		// CFF/OpenType or an unexpected layout — embed the (standalone)
		// face whole. Still valid and selectable, just larger.
		return writeSFNT(version, tables), nil
	}
	return out, nil
}

// parseSFNTTables reads the sfnt table directory of a single-font file, or
// face 0 of a .ttc/.otc collection, returning the sfnt version and a map of
// table tag -> bytes (copied out of raw).
func parseSFNTTables(raw []byte) (uint32, map[string][]byte, error) {
	if len(raw) < 12 {
		return 0, nil, errShortFont
	}
	dirOff := 0
	if string(raw[0:4]) == "ttcf" {
		if len(raw) < 16 {
			return 0, nil, errShortFont
		}
		numFonts := be32(raw, 8)
		if numFonts == 0 {
			return 0, nil, errShortFont
		}
		dirOff = int(be32(raw, 12)) // offset to face 0's table directory
		if dirOff+12 > len(raw) {
			return 0, nil, errShortFont
		}
	}
	version := be32(raw, dirOff)
	numTables := int(be16(raw, dirOff+4))
	tables := make(map[string][]byte, numTables)
	rec := dirOff + 12
	for i := 0; i < numTables; i++ {
		if rec+16 > len(raw) {
			return 0, nil, errShortFont
		}
		tag := string(raw[rec : rec+4])
		off := int(be32(raw, rec+8))
		length := int(be32(raw, rec+12))
		if off < 0 || length < 0 || off+length > len(raw) {
			return 0, nil, errShortFont
		}
		b := make([]byte, length)
		copy(b, raw[off:off+length])
		tables[tag] = b
		rec += 16
	}
	return version, tables, nil
}

// subsetTrueType rebuilds a glyf-based font keeping only the glyphs in
// `keep` (closed over composite components). GIDs are preserved: the new
// numGlyphs is maxKeptGID+1 and unkept glyphs become empty entries.
func subsetTrueType(version uint32, tables map[string][]byte, keep map[uint16]bool) ([]byte, error) {
	glyf, ok1 := tables["glyf"]
	loca, ok2 := tables["loca"]
	head, ok3 := tables["head"]
	maxp, ok4 := tables["maxp"]
	hhea, ok5 := tables["hhea"]
	hmtx, ok6 := tables["hmtx"]
	if !ok1 || !ok2 || !ok3 || !ok4 || !ok5 || !ok6 {
		return nil, errNotTrueType
	}
	if len(head) < 54 || len(maxp) < 6 || len(hhea) < 36 {
		return nil, errShortFont
	}
	numGlyphs := int(be16(maxp, 4))
	longLoca := int16(be16(head, 50)) != 0
	origH := int(be16(hhea, 34))

	// Parse original loca offsets into glyf.
	glyphOffsets := make([]uint32, numGlyphs+1)
	if longLoca {
		if len(loca) < 4*(numGlyphs+1) {
			return nil, errShortFont
		}
		for i := 0; i <= numGlyphs; i++ {
			glyphOffsets[i] = be32(loca, 4*i)
		}
	} else {
		if len(loca) < 2*(numGlyphs+1) {
			return nil, errShortFont
		}
		for i := 0; i <= numGlyphs; i++ {
			glyphOffsets[i] = uint32(be16(loca, 2*i)) * 2
		}
	}
	glyphBytes := func(gid int) []byte {
		if gid < 0 || gid >= numGlyphs {
			return nil
		}
		s, e := glyphOffsets[gid], glyphOffsets[gid+1]
		if e <= s || int(e) > len(glyf) {
			return nil
		}
		return glyf[s:e]
	}

	// Close `keep` over composite-glyph components.
	stack := make([]uint16, 0, len(keep))
	for g := range keep {
		stack = append(stack, g)
	}
	for len(stack) > 0 {
		gid := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		for _, comp := range compositeComponents(glyphBytes(int(gid))) {
			if !keep[comp] {
				keep[comp] = true
				stack = append(stack, comp)
			}
		}
	}

	maxGID := 0
	for g := range keep {
		if int(g) > maxGID {
			maxGID = int(g)
		}
	}
	if maxGID >= numGlyphs {
		maxGID = numGlyphs - 1
	}
	newNum := maxGID + 1

	// Rebuild glyf + long loca (GID-preserving).
	var newGlyf []byte
	newLoca := make([]uint32, newNum+1)
	for gid := 0; gid < newNum; gid++ {
		newLoca[gid] = uint32(len(newGlyf))
		if keep[uint16(gid)] {
			gb := glyphBytes(gid)
			newGlyf = append(newGlyf, gb...)
			if len(newGlyf)%2 != 0 {
				newGlyf = append(newGlyf, 0)
			}
		}
	}
	newLoca[newNum] = uint32(len(newGlyf))

	locaBytes := make([]byte, 4*(newNum+1))
	for i, off := range newLoca {
		putBE32(locaBytes, 4*i, off)
	}

	// Rebuild hmtx with a full long-metric per glyph (numberOfHMetrics = newNum).
	origMetric := func(gid int) (adv uint16, lsb int16) {
		if origH == 0 {
			return 0, 0
		}
		if gid < origH {
			if 4*gid+4 <= len(hmtx) {
				return be16(hmtx, 4*gid), int16(be16(hmtx, 4*gid+2))
			}
			return 0, 0
		}
		if 4*(origH-1)+2 <= len(hmtx) {
			adv = be16(hmtx, 4*(origH-1))
		}
		off := 4*origH + 2*(gid-origH)
		if off+2 <= len(hmtx) {
			lsb = int16(be16(hmtx, off))
		}
		return adv, lsb
	}
	hmtxBytes := make([]byte, 4*newNum)
	for gid := 0; gid < newNum; gid++ {
		adv, lsb := origMetric(gid)
		putBE16(hmtxBytes, 4*gid, adv)
		putBE16(hmtxBytes, 4*gid+2, uint16(lsb))
	}

	// Edited copies of head / hhea / maxp.
	newHead := append([]byte(nil), head...)
	putBE16(newHead, 50, 1) // indexToLocFormat = long
	newHhea := append([]byte(nil), hhea...)
	putBE16(newHhea, 34, uint16(newNum)) // numberOfHMetrics
	newMaxp := append([]byte(nil), maxp...)
	putBE16(newMaxp, 4, uint16(newNum)) // numGlyphs

	out := map[string][]byte{
		"glyf": newGlyf,
		"loca": locaBytes,
		"hmtx": hmtxBytes,
		"head": newHead,
		"hhea": newHhea,
		"maxp": newMaxp,
	}
	// Carry over optional hinting/instruction tables verbatim.
	for _, tag := range []string{"cvt ", "fpgm", "prep", "gasp"} {
		if b, ok := tables[tag]; ok {
			out[tag] = b
		}
	}
	return writeSFNT(version, out), nil
}

// compositeComponents returns the component GIDs referenced by a composite
// glyph, or nil for a simple/empty glyph.
func compositeComponents(g []byte) []uint16 {
	if len(g) < 10 {
		return nil
	}
	if int16(be16(g, 0)) >= 0 {
		return nil // simple glyph
	}
	var comps []uint16
	p := 10 // skip numberOfContours + bbox
	for {
		if p+4 > len(g) {
			break
		}
		flags := be16(g, p)
		comp := be16(g, p+2)
		comps = append(comps, comp)
		p += 4
		// Arguments.
		if flags&0x0001 != 0 { // ARG_1_AND_2_ARE_WORDS
			p += 4
		} else {
			p += 2
		}
		// Transform.
		switch {
		case flags&0x0008 != 0: // WE_HAVE_A_SCALE
			p += 2
		case flags&0x0040 != 0: // WE_HAVE_AN_X_AND_Y_SCALE
			p += 4
		case flags&0x0080 != 0: // WE_HAVE_A_TWO_BY_TWO
			p += 8
		}
		if flags&0x0020 == 0 { // MORE_COMPONENTS
			break
		}
	}
	return comps
}

// writeSFNT serializes a table set into a standalone sfnt font, computing
// table checksums and the head checkSumAdjustment.
func writeSFNT(version uint32, tables map[string][]byte) []byte {
	tags := make([]string, 0, len(tables))
	for t := range tables {
		tags = append(tags, t)
	}
	sortStrings(tags)

	n := len(tags)
	// Header.
	entrySelector := 0
	for (1 << (entrySelector + 1)) <= n {
		entrySelector++
	}
	searchRange := (1 << entrySelector) * 16
	rangeShift := n*16 - searchRange

	headerLen := 12 + 16*n
	// Lay out tables 4-byte aligned.
	type placed struct {
		tag    string
		off    int
		length int
		data   []byte
	}
	placedTables := make([]placed, 0, n)
	off := headerLen
	for _, t := range tags {
		d := tables[t]
		placedTables = append(placedTables, placed{tag: t, off: off, length: len(d), data: d})
		off += (len(d) + 3) &^ 3
	}
	total := off
	buf := make([]byte, total)

	// Offset table.
	putBE32(buf, 0, version)
	putBE16(buf, 4, uint16(n))
	putBE16(buf, 6, uint16(searchRange))
	putBE16(buf, 8, uint16(entrySelector))
	putBE16(buf, 10, uint16(rangeShift))

	rec := 12
	headRecOff := -1
	for _, pt := range placedTables {
		copy(buf[rec:rec+4], pt.tag)
		checksum := tableChecksum(pt.data)
		putBE32(buf, rec+4, checksum)
		putBE32(buf, rec+8, uint32(pt.off))
		putBE32(buf, rec+12, uint32(pt.length))
		copy(buf[pt.off:pt.off+pt.length], pt.data)
		if pt.tag == "head" {
			headRecOff = pt.off
		}
		rec += 16
	}

	// head.checkSumAdjustment = 0xB1B0AFBA - checksum(entire file).
	if headRecOff >= 0 && headRecOff+12 <= len(buf) {
		putBE32(buf, headRecOff+8, 0)
		fileSum := tableChecksum(buf)
		putBE32(buf, headRecOff+8, 0xB1B0AFBA-fileSum)
	}
	return buf
}

func tableChecksum(b []byte) uint32 {
	var sum uint32
	i := 0
	for ; i+4 <= len(b); i += 4 {
		sum += be32(b, i)
	}
	if i < len(b) {
		var last [4]byte
		copy(last[:], b[i:])
		sum += be32(last[:], 0)
	}
	return sum
}

func be16(b []byte, i int) uint16 { return uint16(b[i])<<8 | uint16(b[i+1]) }
func be32(b []byte, i int) uint32 {
	return uint32(b[i])<<24 | uint32(b[i+1])<<16 | uint32(b[i+2])<<8 | uint32(b[i+3])
}
func putBE16(b []byte, i int, v uint16) { b[i] = byte(v >> 8); b[i+1] = byte(v) }
func putBE32(b []byte, i int, v uint32) {
	b[i] = byte(v >> 24)
	b[i+1] = byte(v >> 16)
	b[i+2] = byte(v >> 8)
	b[i+3] = byte(v)
}

func sortStrings(s []string) {
	for i := 1; i < len(s); i++ {
		for j := i; j > 0 && s[j-1] > s[j]; j-- {
			s[j-1], s[j] = s[j], s[j-1]
		}
	}
}

var (
	errShortFont   = fmtErr("qui/pdf: truncated font data")
	errNotTrueType = fmtErr("qui/pdf: not a glyf-based TrueType font")
)

type fmtErr string

func (e fmtErr) Error() string { return string(e) }
