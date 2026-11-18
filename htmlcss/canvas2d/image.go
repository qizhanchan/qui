package canvas2d

import (
	"bytes"
	"encoding/base64"
	"image"
	"image/jpeg"
	"image/png"
	"math"

	"github.com/qizhanchan/qui"
)

// DrawImage is drawImage(img, sx, sy, sw, sh, dx, dy, dw, dh). The source
// rect is clipped to the image, shrinking the destination in proportion.
func (c *Context) DrawImage(img image.Image, sx, sy, sw, sh, dx, dy, dw, dh float64) {
	if c.bmp == nil || img == nil || !finite(sx, sy, sw, sh, dx, dy, dw, dh) {
		return
	}
	b := img.Bounds()
	iw, ih := float64(b.Dx()), float64(b.Dy())
	if iw == 0 || ih == 0 || sw == 0 || sh == 0 || dw == 0 || dh == 0 {
		return
	}
	// Normalize negative sizes.
	if sw < 0 {
		sx, sw = sx+sw, -sw
	}
	if sh < 0 {
		sy, sh = sy+sh, -sh
	}
	if dw < 0 {
		dx, dw = dx+dw, -dw
	}
	if dh < 0 {
		dy, dh = dy+dh, -dh
	}
	// Clip the source to the image.
	kx, ky := dw/sw, dh/sh
	if sx < 0 {
		dx -= sx * kx
		dw += sx * kx
		sw += sx
		sx = 0
	}
	if sy < 0 {
		dy -= sy * ky
		dh += sy * ky
		sh += sy
		sy = 0
	}
	if sx+sw > iw {
		dw -= (sx + sw - iw) * kx
		sw = iw - sx
	}
	if sy+sh > ih {
		dh -= (sy + sh - ih) * ky
		sh = ih - sy
	}
	if sw <= 0 || sh <= 0 || dw <= 0 || dh <= 0 {
		return
	}
	src := subImage(img, sx, sy, sw, sh)
	if src == nil {
		return
	}
	dst := qui.Rect{X: float32(dx), Y: float32(dy), W: float32(dw), H: float32(dh)}
	m := c.st.m
	if !c.st.smoothing && m.IsAxisAligned() {
		// Nearest-neighbour: pre-scale to device size, drawn 1:1.
		dev := m.TransformRect(dst)
		pw, ph := int(math.Round(float64(dev.W))), int(math.Round(float64(dev.H)))
		if pw > 0 && ph > 0 && pw*ph <= MaxPixels {
			src = nearest(src, pw, ph)
		}
	}
	bounds := m.TransformRect(dst)
	c.render(bounds, func(cv qui.Canvas, alpha float32) {
		if alpha < 1 {
			cv.SaveLayer(bounds, qui.Paint{Alpha: alpha})
		}
		cv.Concat(m)
		cv.DrawImage(src, dst)
	})
}

// subImage crops img to the (rounded-out) source rect.
func subImage(img image.Image, sx, sy, sw, sh float64) image.Image {
	b := img.Bounds()
	r := image.Rect(
		b.Min.X+int(math.Floor(sx)), b.Min.Y+int(math.Floor(sy)),
		b.Min.X+int(math.Ceil(sx+sw)), b.Min.Y+int(math.Ceil(sy+sh)),
	).Intersect(b)
	if r.Empty() {
		return nil
	}
	if r == b {
		return img
	}
	if s, ok := img.(interface {
		SubImage(image.Rectangle) image.Image
	}); ok {
		return s.SubImage(r)
	}
	out := image.NewRGBA(image.Rect(0, 0, r.Dx(), r.Dy()))
	for y := 0; y < r.Dy(); y++ {
		for x := 0; x < r.Dx(); x++ {
			out.Set(x, y, img.At(r.Min.X+x, r.Min.Y+y))
		}
	}
	return out
}

func nearest(img image.Image, w, h int) *image.RGBA {
	src := toRGBA(img)
	sw, sh := src.Rect.Dx(), src.Rect.Dy()
	out := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		syy := min(sh-1, y*sh/h)
		for x := 0; x < w; x++ {
			sxx := min(sw-1, x*sw/w)
			copy(out.Pix[y*out.Stride+4*x:y*out.Stride+4*x+4], src.Pix[syy*src.Stride+4*sxx:])
		}
	}
	return out
}

// GetImageData returns w×h pixels at (x, y) as non-premultiplied RGBA;
// pixels outside the bitmap are transparent black.
func (c *Context) GetImageData(x, y, w, h int) []byte {
	out := make([]byte, 4*w*h)
	if c.bmp == nil {
		return out
	}
	for j := 0; j < h; j++ {
		yy := y + j
		if yy < 0 || yy >= c.h {
			continue
		}
		for i := 0; i < w; i++ {
			xx := x + i
			if xx < 0 || xx >= c.w {
				continue
			}
			s := c.bmp.Pix[yy*c.bmp.Stride+4*xx : yy*c.bmp.Stride+4*xx+4]
			d := out[4*(j*w+i) : 4*(j*w+i)+4]
			a := s[3]
			d[3] = a
			if a == 0 {
				continue
			}
			if a == 255 {
				d[0], d[1], d[2] = s[0], s[1], s[2]
				continue
			}
			for k := 0; k < 3; k++ {
				d[k] = uint8(min(255, (uint32(s[k])*255+uint32(a)/2)/uint32(a)))
			}
		}
	}
	return out
}

// PutImageData writes non-premultiplied RGBA (w×h) at (dx, dy), limited
// to the dirty rect (in data coordinates). No transform, clip, alpha or
// compositing applies.
func (c *Context) PutImageData(data []byte, w, h, dx, dy, dirtyX, dirtyY, dirtyW, dirtyH int) {
	if c.bmp == nil || len(data) < 4*w*h {
		return
	}
	if dirtyW < 0 {
		dirtyX, dirtyW = dirtyX+dirtyW, -dirtyW
	}
	if dirtyH < 0 {
		dirtyY, dirtyH = dirtyY+dirtyH, -dirtyH
	}
	r := image.Rect(dirtyX, dirtyY, dirtyX+dirtyW, dirtyY+dirtyH).Intersect(image.Rect(0, 0, w, h))
	for j := r.Min.Y; j < r.Max.Y; j++ {
		yy := dy + j
		if yy < 0 || yy >= c.h {
			continue
		}
		for i := r.Min.X; i < r.Max.X; i++ {
			xx := dx + i
			if xx < 0 || xx >= c.w {
				continue
			}
			s := data[4*(j*w+i) : 4*(j*w+i)+4]
			d := c.bmp.Pix[yy*c.bmp.Stride+4*xx : yy*c.bmp.Stride+4*xx+4]
			a := uint32(s[3])
			d[3] = s[3]
			for k := 0; k < 3; k++ {
				d[k] = uint8((uint32(s[k])*a + 127) / 255)
			}
		}
	}
	c.changed()
}

// Snapshot returns a copy of the bitmap (for drawImage(canvas) of itself,
// patterns, toBlob).
func (c *Context) Snapshot() *image.RGBA {
	if c.bmp == nil {
		return nil
	}
	out := image.NewRGBA(c.bmp.Rect)
	copy(out.Pix, c.bmp.Pix)
	return out
}

// Encode returns the bitmap as image/png (default) or image/jpeg
// (quality 0..1, default 0.92) and the MIME type used.
func (c *Context) Encode(mime string, quality float64) ([]byte, string) {
	img := c.bmp
	if img == nil {
		img = image.NewRGBA(image.Rect(0, 0, 1, 1)) // Chrome encodes an empty canvas as "data:,"
		if c.w == 0 || c.h == 0 {
			return nil, ""
		}
	}
	var buf bytes.Buffer
	if mime == "image/jpeg" {
		q := 92
		if quality >= 0 && quality <= 1 {
			q = int(math.Round(quality * 100))
		}
		// JPEG has no alpha: composite onto black, as Chrome does.
		flat := image.NewRGBA(img.Rect)
		for i := 0; i < len(img.Pix); i += 4 {
			flat.Pix[i], flat.Pix[i+1], flat.Pix[i+2], flat.Pix[i+3] = img.Pix[i], img.Pix[i+1], img.Pix[i+2], 255
		}
		if err := jpeg.Encode(&buf, flat, &jpeg.Options{Quality: max(1, q)}); err == nil {
			return buf.Bytes(), "image/jpeg"
		}
		buf.Reset()
	}
	// image.RGBA is premultiplied; png.Encode un-premultiplies it.
	if err := png.Encode(&buf, img); err != nil {
		return nil, ""
	}
	return buf.Bytes(), "image/png"
}

// DataURL is toDataURL.
func (c *Context) DataURL(mime string, quality float64) string {
	b, t := c.Encode(mime, quality)
	if t == "" {
		return "data:,"
	}
	return "data:" + t + ";base64," + base64.StdEncoding.EncodeToString(b)
}

// Copy returns img as a fresh premultiplied RGBA at the origin.
func Copy(img image.Image) *image.RGBA { return toRGBA(img) }
