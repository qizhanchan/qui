package htmlcss

import (
	"bytes"
	"encoding/binary"
	"errors"
	"image"
	"image/color"
	"image/png"
	"io"
)

// Windows .ico / .cur files, registered with the image package so <img>
// and image.Decode read them like any other format. An icon directory
// holds several sizes; the decoder returns the largest (then deepest)
// one. Entries are either an embedded PNG or a headerless BMP (DIB):
// 1/4/8-bit palettized, 24-bit or 32-bit BGRA, with the 1-bit AND mask
// as transparency when there is no alpha channel.

func init() {
	image.RegisterFormat("ico", "\x00\x00\x01\x00", decodeICO, decodeICOConfig)
	image.RegisterFormat("cur", "\x00\x00\x02\x00", decodeICO, decodeICOConfig)
}

type icoEntry struct {
	w, h   int
	bpp    int
	size   uint32
	offset uint32
}

var errICO = errors.New("ico: invalid format")

// icoBest reads the directory and picks the entry to show.
func icoBest(data []byte) (icoEntry, error) {
	if len(data) < 6 {
		return icoEntry{}, errICO
	}
	n := int(binary.LittleEndian.Uint16(data[4:]))
	if n == 0 || len(data) < 6+16*n {
		return icoEntry{}, errICO
	}
	var best icoEntry
	for i := 0; i < n; i++ {
		d := data[6+16*i:]
		e := icoEntry{
			w:      int(d[0]),
			h:      int(d[1]),
			bpp:    int(binary.LittleEndian.Uint16(d[6:])),
			size:   binary.LittleEndian.Uint32(d[8:]),
			offset: binary.LittleEndian.Uint32(d[12:]),
		}
		if e.w == 0 {
			e.w = 256
		}
		if e.h == 0 {
			e.h = 256
		}
		if uint64(e.offset)+uint64(e.size) > uint64(len(data)) || e.size == 0 {
			continue
		}
		if e.w*e.h > best.w*best.h || (e.w*e.h == best.w*best.h && e.bpp > best.bpp) {
			best = e
		}
	}
	if best.size == 0 {
		return icoEntry{}, errICO
	}
	return best, nil
}

func decodeICOConfig(r io.Reader) (image.Config, error) {
	data, err := io.ReadAll(r)
	if err != nil {
		return image.Config{}, err
	}
	e, err := icoBest(data)
	if err != nil {
		return image.Config{}, err
	}
	blob := data[e.offset : e.offset+e.size]
	if bytes.HasPrefix(blob, []byte("\x89PNG")) {
		return png.DecodeConfig(bytes.NewReader(blob))
	}
	return image.Config{ColorModel: color.NRGBAModel, Width: e.w, Height: e.h}, nil
}

func decodeICO(r io.Reader) (image.Image, error) {
	data, err := io.ReadAll(r)
	if err != nil {
		return nil, err
	}
	e, err := icoBest(data)
	if err != nil {
		return nil, err
	}
	blob := data[e.offset : e.offset+e.size]
	if bytes.HasPrefix(blob, []byte("\x89PNG")) {
		return png.Decode(bytes.NewReader(blob))
	}
	return decodeDIB(blob)
}

// decodeDIB decodes an icon's BMP payload: a BITMAPINFOHEADER whose
// height counts the XOR image and the AND mask together, the palette,
// the bottom-up XOR pixels, then the 1-bit AND mask.
func decodeDIB(b []byte) (image.Image, error) {
	if len(b) < 40 {
		return nil, errICO
	}
	hdr := int(binary.LittleEndian.Uint32(b))
	w := int(int32(binary.LittleEndian.Uint32(b[4:])))
	h := int(int32(binary.LittleEndian.Uint32(b[8:]))) / 2
	bpp := int(binary.LittleEndian.Uint16(b[14:]))
	compression := binary.LittleEndian.Uint32(b[16:])
	colors := int(binary.LittleEndian.Uint32(b[32:]))
	if hdr < 40 || w <= 0 || h <= 0 || w > 1024 || h > 1024 || (compression != 0 && compression != 3) {
		return nil, errICO
	}
	var pal []color.NRGBA
	if bpp <= 8 {
		if colors == 0 {
			colors = 1 << bpp
		}
		p := hdr
		if p+4*colors > len(b) {
			return nil, errICO
		}
		pal = make([]color.NRGBA, colors)
		for i := range pal {
			q := b[p+4*i:]
			pal[i] = color.NRGBA{R: q[2], G: q[1], B: q[0], A: 255}
		}
	}
	xorStart := hdr + 4*len(pal)
	xorStride := ((w*bpp + 31) / 32) * 4
	andStride := ((w + 31) / 32) * 4
	andStart := xorStart + xorStride*h
	if xorStart+xorStride*h > len(b) {
		return nil, errICO
	}
	hasMask := andStart+andStride*h <= len(b)

	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	anyAlpha := false
	for y := 0; y < h; y++ {
		row := b[xorStart+xorStride*(h-1-y):]
		for x := 0; x < w; x++ {
			var c color.NRGBA
			switch bpp {
			case 32:
				q := row[4*x:]
				c = color.NRGBA{R: q[2], G: q[1], B: q[0], A: q[3]}
				if q[3] != 0 {
					anyAlpha = true
				}
			case 24:
				q := row[3*x:]
				c = color.NRGBA{R: q[2], G: q[1], B: q[0], A: 255}
			case 8:
				c = palAt(pal, int(row[x]))
			case 4:
				c = palAt(pal, int(row[x/2]>>(4*(1-uint(x%2)))&0x0f))
			case 1:
				c = palAt(pal, int(row[x/8]>>(7-uint(x%8))&1))
			default:
				return nil, errICO
			}
			img.SetNRGBA(x, y, c)
		}
	}
	// The AND mask is the transparency of icons without an alpha channel
	// (and of 32-bit ones whose alpha is all zero).
	if hasMask && (bpp != 32 || !anyAlpha) {
		for y := 0; y < h; y++ {
			row := b[andStart+andStride*(h-1-y):]
			for x := 0; x < w; x++ {
				i := img.PixOffset(x, y)
				if row[x/8]>>(7-uint(x%8))&1 == 1 {
					img.Pix[i+3] = 0
				} else {
					img.Pix[i+3] = 255
				}
			}
		}
	}
	return img, nil
}

func palAt(pal []color.NRGBA, i int) color.NRGBA {
	if i < len(pal) {
		return pal[i]
	}
	return color.NRGBA{}
}
