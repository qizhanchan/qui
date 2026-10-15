package htmlcss

import (
	"bytes"
	"encoding/binary"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/image/bmp"
	"golang.org/x/image/tiff"
)

// icoFile wraps entries (raw PNG or DIB payloads) in an ICONDIR.
func icoFile(entries [][]byte, sizes [][2]int, bpps []int) []byte {
	var b bytes.Buffer
	binary.Write(&b, binary.LittleEndian, [3]uint16{0, 1, uint16(len(entries))})
	off := 6 + 16*len(entries)
	for i, e := range entries {
		b.WriteByte(byte(sizes[i][0]))
		b.WriteByte(byte(sizes[i][1]))
		b.Write([]byte{0, 0})
		binary.Write(&b, binary.LittleEndian, [2]uint16{1, uint16(bpps[i])})
		binary.Write(&b, binary.LittleEndian, [2]uint32{uint32(len(e)), uint32(off)})
		off += len(e)
	}
	for _, e := range entries {
		b.Write(e)
	}
	return b.Bytes()
}

// dib32 is a w×h 32-bit BGRA icon bitmap (no AND mask) filled with c.
func dib32(w, h int, c color.NRGBA) []byte {
	var b bytes.Buffer
	binary.Write(&b, binary.LittleEndian, uint32(40))
	binary.Write(&b, binary.LittleEndian, [2]int32{int32(w), int32(2 * h)})
	binary.Write(&b, binary.LittleEndian, [2]uint16{1, 32})
	b.Write(make([]byte, 24))
	for i := 0; i < w*h; i++ {
		b.Write([]byte{c.B, c.G, c.R, c.A})
	}
	return b.Bytes()
}

// dib8Masked is a 2×2 8-bit icon whose AND mask hides the top-left pixel.
func dib8Masked() []byte {
	var b bytes.Buffer
	binary.Write(&b, binary.LittleEndian, uint32(40))
	binary.Write(&b, binary.LittleEndian, [2]int32{2, 4})
	binary.Write(&b, binary.LittleEndian, [2]uint16{1, 8})
	b.Write(make([]byte, 16))
	binary.Write(&b, binary.LittleEndian, uint32(2)) // colors used
	b.Write(make([]byte, 4))
	b.Write([]byte{0, 0, 255, 0, 0, 255, 0, 0}) // palette: red, green
	b.Write([]byte{1, 1, 0, 0})                 // bottom row: green ×2 (stride 4)
	b.Write([]byte{0, 0, 0, 0})                 // top row: red ×2
	b.Write([]byte{0, 0, 0, 0})                 // AND bottom row: opaque
	b.Write([]byte{0x80, 0, 0, 0})              // AND top row: (0,0) transparent
	return b.Bytes()
}

func TestICOPicksLargestEntry(t *testing.T) {
	big := image.NewNRGBA(image.Rect(0, 0, 4, 4))
	var pngBuf bytes.Buffer
	png.Encode(&pngBuf, big)
	data := icoFile(
		[][]byte{dib32(2, 2, color.NRGBA{R: 255, A: 255}), pngBuf.Bytes()},
		[][2]int{{2, 2}, {4, 4}}, []int{32, 32})
	img, format, err := image.Decode(bytes.NewReader(data))
	if err != nil || format != "ico" {
		t.Fatalf("decode: %v (format %q)", err, format)
	}
	if img.Bounds().Dx() != 4 {
		t.Errorf("picked %v, want the 4×4 PNG entry", img.Bounds())
	}
	cfg, _, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil || cfg.Width != 4 || cfg.Height != 4 {
		t.Errorf("config = %+v, %v; want 4×4", cfg, err)
	}
}

func TestICODIBPixelsAndMask(t *testing.T) {
	img, _, err := image.Decode(bytes.NewReader(icoFile([][]byte{dib32(2, 2, color.NRGBA{R: 10, G: 20, B: 30, A: 200})}, [][2]int{{2, 2}}, []int{32})))
	if err != nil {
		t.Fatal(err)
	}
	if got := color.NRGBAModel.Convert(img.At(1, 1)).(color.NRGBA); got != (color.NRGBA{R: 10, G: 20, B: 30, A: 200}) {
		t.Errorf("32-bit pixel = %v", got)
	}

	img, _, err = image.Decode(bytes.NewReader(icoFile([][]byte{dib8Masked()}, [][2]int{{2, 2}}, []int{8})))
	if err != nil {
		t.Fatal(err)
	}
	at := func(x, y int) color.NRGBA { return color.NRGBAModel.Convert(img.At(x, y)).(color.NRGBA) }
	if c := at(0, 0); c.A != 0 {
		t.Errorf("masked pixel alpha = %d, want 0", c.A)
	}
	if c := at(1, 0); c != (color.NRGBA{R: 255, A: 255}) {
		t.Errorf("top-right = %v, want opaque red", c)
	}
	if c := at(0, 1); c != (color.NRGBA{G: 255, A: 255}) {
		t.Errorf("bottom-left = %v, want opaque green (rows are bottom-up)", c)
	}
}

// <img src> reads BMP and TIFF (golang.org/x/image) besides PNG/JPEG/GIF.
func TestImgDecodesBMPAndTIFF(t *testing.T) {
	dir := t.TempDir()
	src := image.NewNRGBA(image.Rect(0, 0, 3, 2))
	var bb, tb bytes.Buffer
	if err := bmp.Encode(&bb, src); err != nil {
		t.Fatal(err)
	}
	if err := tiff.Encode(&tb, src, nil); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(dir, "a.bmp"), bb.Bytes(), 0o644)
	os.WriteFile(filepath.Join(dir, "a.tiff"), tb.Bytes(), 0o644)
	for _, name := range []string{"a.bmp", "a.tiff"} {
		img := loadRasterImage(name, dir)
		if img == nil || img.Bounds().Dx() != 3 || img.Bounds().Dy() != 2 {
			t.Errorf("%s: decoded %v, want 3×2", name, img)
		}
	}
	if _, _, err := image.DecodeConfig(bytes.NewReader([]byte("RIFF\x00\x00\x00\x00WEBPVP8 "))); err == nil || err == image.ErrFormat {
		t.Errorf("webp not registered with image.Decode (err %v)", err)
	}
}
