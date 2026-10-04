package qui

import (
	"image"
	"math"
)

// ColorFilter transforms every pixel of a source buffer through a
// stateless per-pixel function. Applied by SaveLayer's composite step
// BEFORE Porter-Duff blending, so a shape drawn inside a
// ColorFilter-carrying layer looks as if every source color was
// pre-transformed. Grayscale, invert, hue-shift, saturation adjust
// all fit here.
//
// ApplyColor operates on STRAIGHT (non-premultiplied) RGBA and returns
// STRAIGHT — implementations write clean per-channel math without
// worrying about premul divisions. The caller unpacks / repacks
// premul at the boundary.
type ColorFilter interface {
	ApplyColor(c Color) Color
}

// MatrixColorFilter is a 4x5 color-matrix filter, the standard model
// used by CSS `filter: contrast(...)`, Android ColorMatrix, and Skia's
// SkColorMatrix. Each output channel is a linear combination of the
// four input channels plus a constant offset:
//
//	out.r = m[0]*r + m[1]*g + m[2]*b + m[3]*a + m[4]
//	out.g = m[5]*r + m[6]*g + m[7]*b + m[8]*a + m[9]
//	out.b = m[10]*r + m[11]*g + m[12]*b + m[13]*a + m[14]
//	out.a = m[15]*r + m[16]*g + m[17]*b + m[18]*a + m[19]
//
// Identity matrix leaves the color unchanged; helper constructors
// below cover common effects. Output channels are NOT clamped — the
// composite pass clamps to [0, 1] when writing back to bytes.
type MatrixColorFilter struct {
	M [20]float32
}

// ApplyColor implements ColorFilter.
func (f MatrixColorFilter) ApplyColor(c Color) Color {
	return Color{
		R: f.M[0]*c.R + f.M[1]*c.G + f.M[2]*c.B + f.M[3]*c.A + f.M[4],
		G: f.M[5]*c.R + f.M[6]*c.G + f.M[7]*c.B + f.M[8]*c.A + f.M[9],
		B: f.M[10]*c.R + f.M[11]*c.G + f.M[12]*c.B + f.M[13]*c.A + f.M[14],
		A: f.M[15]*c.R + f.M[16]*c.G + f.M[17]*c.B + f.M[18]*c.A + f.M[19],
	}
}

// IdentityMatrixColorFilter returns the no-op color matrix. Handy as a
// starting point for callers building a custom matrix, and as a
// sentinel value that produces no visual change.
func IdentityMatrixColorFilter() MatrixColorFilter {
	return MatrixColorFilter{M: [20]float32{
		1, 0, 0, 0, 0,
		0, 1, 0, 0, 0,
		0, 0, 1, 0, 0,
		0, 0, 0, 1, 0,
	}}
}

// GrayscaleColorFilter returns a matrix that maps every color to its
// luminance in the ITU-R BT.601 formula (Rec.601, which is what CSS
// `filter: grayscale(1)` uses). Alpha unchanged.
func GrayscaleColorFilter() MatrixColorFilter {
	const rW, gW, bW = 0.299, 0.587, 0.114
	return MatrixColorFilter{M: [20]float32{
		rW, gW, bW, 0, 0,
		rW, gW, bW, 0, 0,
		rW, gW, bW, 0, 0,
		0, 0, 0, 1, 0,
	}}
}

// InvertColorFilter returns a matrix that flips each color channel
// (1 - c) while leaving alpha untouched.
func InvertColorFilter() MatrixColorFilter {
	return MatrixColorFilter{M: [20]float32{
		-1, 0, 0, 0, 1,
		0, -1, 0, 0, 1,
		0, 0, -1, 0, 1,
		0, 0, 0, 1, 0,
	}}
}

// ImageFilter is applied to a WHOLE layer buffer at composite time —
// it returns a new (or same) image that replaces the source buffer
// before Porter-Duff blending. Blur, drop-shadow, offset, and other
// effects that need multi-pixel neighborhood access live here (a
// ColorFilter would only see one pixel at a time).
//
// The input `src` is a premultiplied RGBA whose Rect is aligned with
// the compositing target's device space. The returned image should
// also be premultiplied and aligned; implementations are free to
// return a NEW image with a LARGER rect (drop-shadow blurs past the
// content edge — the composite honors the returned Rect).
type ImageFilter interface {
	ApplyImage(src *image.RGBA) *image.RGBA
}

// DropShadowImageFilter renders a Gaussian-ish drop shadow of the
// layer's alpha silhouette behind the original content. The shadow
// follows the layer's shape (not just a rect), so a rounded-rect
// button, a circular avatar, and an SVG heart all cast shadows that
// track their outlines — this is the value proposition over
// qui.DrawShadow, which stamps a rounded-rect coverage regardless of
// what content is on top.
//
// Offset: (dx, dy) in physical pixels from the layer's origin.
// Blur:   Gaussian σ (in physical pixels). 0 → sharp shadow.
// Color:  straight RGBA of the shadow. Alpha is respected.
type DropShadowImageFilter struct {
	Offset Point
	Blur   float32
	Color  Color
}

// ApplyImage implements ImageFilter.
func (f DropShadowImageFilter) ApplyImage(src *image.RGBA) *image.RGBA {
	if src == nil {
		return nil
	}
	if f.Color.A <= 0 {
		return src
	}
	// Result buffer is the union of the src's rect and the offset+blur
	// halo. Blur radius in pixels ≈ ceil(σ*3) safely covers 99.7% of
	// the Gaussian; the box-blur approximation is close enough that
	// σ*3 catches the visible tail.
	blur := f.Blur
	if blur < 0 {
		blur = 0
	}
	halo := int(math.Ceil(float64(blur * 3)))
	if halo < 0 {
		halo = 0
	}
	dxInt := int(math.Round(float64(f.Offset.X)))
	dyInt := int(math.Round(float64(f.Offset.Y)))
	srcRect := src.Bounds()
	shadowRect := image.Rect(
		srcRect.Min.X+dxInt-halo,
		srcRect.Min.Y+dyInt-halo,
		srcRect.Max.X+dxInt+halo,
		srcRect.Max.Y+dyInt+halo,
	)
	outRect := srcRect.Union(shadowRect)
	out := image.NewRGBA(outRect)

	// Extract src's alpha channel into a compact []uint8 sized to
	// (srcW + 2*halo) × (srcH + 2*halo) so blur has room to spread.
	bufW := srcRect.Dx() + 2*halo
	bufH := srcRect.Dy() + 2*halo
	alphaBuf := make([]uint8, bufW*bufH)
	// Copy src's alpha into the center of the buffer at (halo, halo).
	for y := 0; y < srcRect.Dy(); y++ {
		srcRow := (y)*src.Stride + 3 // start at alpha byte of column 0
		bufRow := (y + halo) * bufW
		for x := 0; x < srcRect.Dx(); x++ {
			alphaBuf[bufRow+halo+x] = src.Pix[srcRow+x*4]
		}
	}
	// Two-pass box blur ≈ Gaussian with σ ≈ blur.
	if blur > 0 {
		sigma := float64(blur)
		boxR := int(math.Round((math.Sqrt(1+3*sigma*sigma) - 1) / 2))
		if boxR < 1 {
			boxR = 1
		}
		tmp := make([]uint8, bufW*bufH)
		boxBlurH(alphaBuf, tmp, bufW, bufH, boxR)
		boxBlurV(tmp, alphaBuf, bufW, bufH, boxR)
		boxBlurH(alphaBuf, tmp, bufW, bufH, boxR)
		boxBlurV(tmp, alphaBuf, bufW, bufH, boxR)
	}
	// Composite the shadow layer into `out` at (dxInt-halo, dyInt-halo)
	// offset from src's origin. Shadow is a tinted alpha at each pixel.
	sr := f.Color.R
	sg := f.Color.G
	sb := f.Color.B
	sa := f.Color.A
	shadowOriginX := srcRect.Min.X + dxInt - halo
	shadowOriginY := srcRect.Min.Y + dyInt - halo
	for by := 0; by < bufH; by++ {
		for bx := 0; bx < bufW; bx++ {
			a := alphaBuf[by*bufW+bx]
			if a == 0 {
				continue
			}
			px := shadowOriginX + bx
			py := shadowOriginY + by
			if px < outRect.Min.X || px >= outRect.Max.X ||
				py < outRect.Min.Y || py >= outRect.Max.Y {
				continue
			}
			// Premul RGBA of shadow at this pixel.
			cov := float32(a) / 255
			effA := sa * cov
			outIdx := out.PixOffset(px, py)
			out.Pix[outIdx+0] = uint8(sr * effA * 255)
			out.Pix[outIdx+1] = uint8(sg * effA * 255)
			out.Pix[outIdx+2] = uint8(sb * effA * 255)
			out.Pix[outIdx+3] = uint8(effA * 255)
		}
	}
	// Composite the original src ON TOP of the shadow via premul SrcOver.
	for y := srcRect.Min.Y; y < srcRect.Max.Y; y++ {
		for x := srcRect.Min.X; x < srcRect.Max.X; x++ {
			si := src.PixOffset(x, y)
			outIdx := out.PixOffset(x, y)
			srR := float32(src.Pix[si+0]) / 255
			srG := float32(src.Pix[si+1]) / 255
			srB := float32(src.Pix[si+2]) / 255
			srA := float32(src.Pix[si+3]) / 255
			if srA <= 0 {
				continue
			}
			ia := 1 - srA
			dR := float32(out.Pix[outIdx+0]) / 255
			dG := float32(out.Pix[outIdx+1]) / 255
			dB := float32(out.Pix[outIdx+2]) / 255
			dA := float32(out.Pix[outIdx+3]) / 255
			out.Pix[outIdx+0] = clamp255((srR + dR*ia) * 255)
			out.Pix[outIdx+1] = clamp255((srG + dG*ia) * 255)
			out.Pix[outIdx+2] = clamp255((srB + dB*ia) * 255)
			out.Pix[outIdx+3] = clamp255((srA + dA*ia) * 255)
		}
	}
	return out
}

// applyColorFilterInPlace runs `filter` over every pixel of `img`,
// premul-aware. Reads premul → converts to straight → applies filter
// → converts back to premul → writes. Skipped for a==0 pixels because
// their color is undefined (all zeros).
//
// Consumed by SaveLayer's Restore step when paint.ColorFilter != nil.
func applyColorFilterInPlace(img *image.RGBA, filter ColorFilter) {
	if img == nil || filter == nil {
		return
	}
	bounds := img.Bounds()
	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			i := img.PixOffset(x, y)
			a := float32(img.Pix[i+3]) / 255
			if a <= 0 {
				continue
			}
			// premul → straight
			sc := Color{
				R: float32(img.Pix[i+0]) / 255 / a,
				G: float32(img.Pix[i+1]) / 255 / a,
				B: float32(img.Pix[i+2]) / 255 / a,
				A: a,
			}
			out := filter.ApplyColor(sc)
			// clamp then repack premul
			img.Pix[i+0] = clamp255(clampFloat(out.R, 0, 1) * clampFloat(out.A, 0, 1) * 255)
			img.Pix[i+1] = clamp255(clampFloat(out.G, 0, 1) * clampFloat(out.A, 0, 1) * 255)
			img.Pix[i+2] = clamp255(clampFloat(out.B, 0, 1) * clampFloat(out.A, 0, 1) * 255)
			img.Pix[i+3] = clamp255(clampFloat(out.A, 0, 1) * 255)
		}
	}
}

func clampFloat(v, lo, hi float32) float32 {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

// BlurImageFilter blurs a whole layer with a separable box blur run three
// times (a close Gaussian approximation). Radius is the box-blur radius in
// physical pixels; 0 is a no-op. Used for CSS filter: blur().
type BlurImageFilter struct {
	Radius float32
}

// ApplyImage implements ImageFilter.
func (f BlurImageFilter) ApplyImage(src *image.RGBA) *image.RGBA {
	if src == nil || f.Radius < 0.5 {
		return src
	}
	r := int(f.Radius + 0.5)
	buf := src
	for pass := 0; pass < 3; pass++ {
		buf = boxBlurPass(buf, r, true)
		buf = boxBlurPass(buf, r, false)
	}
	return buf
}

// boxBlurPass averages each pixel over a [-r, r] window along one axis.
// Premultiplied averaging keeps edges from bleeding wrong colors.
func boxBlurPass(src *image.RGBA, r int, horizontal bool) *image.RGBA {
	b := src.Bounds()
	dst := image.NewRGBA(b)
	w, h := b.Dx(), b.Dy()
	get := func(x, y int) (rr, gg, bb, aa int) {
		i := src.PixOffset(b.Min.X+x, b.Min.Y+y)
		a := int(src.Pix[i+3])
		// premultiply
		return int(src.Pix[i]) * a / 255, int(src.Pix[i+1]) * a / 255, int(src.Pix[i+2]) * a / 255, a
	}
	win := 2*r + 1
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			var sr, sg, sb, sa int
			for k := -r; k <= r; k++ {
				px, py := x, y
				if horizontal {
					px = clampInt(x+k, 0, w-1)
				} else {
					py = clampInt(y+k, 0, h-1)
				}
				cr, cg, cb, ca := get(px, py)
				sr += cr
				sg += cg
				sb += cb
				sa += ca
			}
			a := sa / win
			di := dst.PixOffset(b.Min.X+x, b.Min.Y+y)
			if a == 0 {
				dst.Pix[di], dst.Pix[di+1], dst.Pix[di+2], dst.Pix[di+3] = 0, 0, 0, 0
				continue
			}
			// un-premultiply
			dst.Pix[di] = uint8(clampInt((sr/win)*255/a, 0, 255))
			dst.Pix[di+1] = uint8(clampInt((sg/win)*255/a, 0, 255))
			dst.Pix[di+2] = uint8(clampInt((sb/win)*255/a, 0, 255))
			dst.Pix[di+3] = uint8(a)
		}
	}
	return dst
}

func clampInt(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}
