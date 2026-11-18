package typeset

import (
	"golang.org/x/image/font/gofont/goregular"
	"golang.org/x/image/font/opentype"
	"testing"
)

func TestSystemFontLegacyUnicodeCmap(t *testing.T) {
	version, tables, err := ParseSFNTTables(goregular.TTF)
	if err != nil {
		t.Fatal(err)
	}
	original := tables["cmap"]
	var sub []byte
	for i := 0; i < int(be16(original, 2)); i++ {
		off := int(be32(original, 8+8*i))
		if be16(original, off) == 4 {
			sub = original[off : off+int(be16(original, off+2))]
			break
		}
	}
	if sub == nil {
		t.Fatal("fixture has no BMP map")
	}
	for _, encoding := range []uint16{0, 1, 2} {
		cmap := make([]byte, 12+len(sub))
		putBE16(cmap, 2, 1)
		putBE16(cmap, 6, encoding)
		putBE32(cmap, 8, 12)
		copy(cmap[12:], sub)
		tables["cmap"] = cmap
		data := writeSFNT(version, tables)
		if _, err := opentype.Parse(data); err == nil {
			t.Fatal("fixture must exercise an unsupported legacy identifier")
		}
		f, usable := parseSystemFontBytes(data)
		if f == nil || len(usable) == 0 {
			t.Fatalf("legacy encoding %d rejected", encoding)
		}
		index, err := f.GlyphIndex(nil, 'A')
		if err != nil || index == 0 {
			t.Fatalf("mapping lost: %d %v", index, err)
		}
	}
}
