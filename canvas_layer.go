package qui

import "image"

// BlendMode selects the Porter-Duff / blend operator used to composite
// a source pixel against a destination pixel. Consumed exclusively by
// SaveLayer's composite pass — the regular DrawShape rasterizers use a
// hard-coded SrcOver internally (via fillRectBlend / blendPixel).
//
// Skia enumerates ~30 SkBlendMode values; qui ships the subset that
// covers everyday UI needs plus common filter-pipeline building blocks. Add
// new modes here as concrete callers require them — every mode below
// is exercised by a test in canvas_layer_test.go so a lazy addition
// that silently falls through to SrcOver fails loudly.
type BlendMode int8

const (
	// BlendSrcOver is the standard alpha-composite: out = src + dst*(1-src.a).
	// The default (zero-value) mode — matches Skia and CSS canvas.
	BlendSrcOver BlendMode = iota
	// BlendSrc replaces the destination with the source (Porter-Duff Copy).
	// Useful for cutting a hole (paint (0,0,0,0) with BlendSrc) or for
	// initializing a layer that ignores its parent.
	BlendSrc
	// BlendDstIn keeps the destination only where the source has coverage:
	// out = dst * src.a. The primitive behind mask filters — paint a
	// coverage source into a temporary layer, then composite the covered
	// region back with DstIn to punch through anything outside the mask.
	BlendDstIn
	// BlendDstOut is the inverse: out = dst * (1 - src.a). Used for
	// erasing / cutouts (donut cutout, inner-shadow inner-mask step).
	BlendDstOut
	// BlendMultiply darkens: out = src * dst (component-wise). Common
	// for tinting under a shadow layer.
	BlendMultiply
	// BlendScreen lightens: out = 1 - (1-src) * (1-dst). Used for
	// bloom / glare-style additive-but-clamped compositing.
	BlendScreen
	// BlendPlus adds linearly (clamped): out = min(1, src + dst).
	// Skia's "plus" — used for additive light overlays where SrcOver's
	// alpha-weighted addition undersells the intensity.
	BlendPlus
)

// layerFrame is one entry on the imageCanvas's active-layer stack.
// Pushed by SaveLayer, popped by Restore / RestoreTo once the save
// depth is unwound. The frame remembers everything needed to blit the
// offscreen back onto its parent:
//   - outer:      the buffer that was active BEFORE this layer was pushed.
//     Composite target when this layer pops. May itself be another
//     layer's offscreen for nested SaveLayer chains.
//   - img:        the offscreen buffer draws go into while this layer
//     is active. Its Rect is aligned with the parent's device space
//     (non-zero-origin RGBA) so rasterizers can write to pixel (x, y)
//     directly without any translation math.
//   - paint:      the BlendMode + Alpha to apply on composite. Color is
//     ignored — the offscreen carries the drawn colors already.
//   - saveDepth:  the canvasState.Save() depth that also wrapped this
//     layer. Restore-past-this-depth is what triggers composite.
//   - clip:       the clip in effect when the layer was pushed, in the
//     parent's device space. The composite is confined to it. Kept
//     separately from img.Bounds() because a FILTERED layer is allowed to
//     grow past its own bounds (a drop shadow that couldn't would be
//     invisible) but never past the clip — see cpuBackend.PopLayer.
type layerFrame struct {
	outer     *image.RGBA
	img       *image.RGBA
	paint     Paint
	clip      image.Rectangle
	saveDepth int
	// clipMask, when non-nil, is an alpha coverage mask (same physical
	// bounds as img) that multiplies img's alpha channel BEFORE
	// compositing. Populated by ClipPath. Never populated by a normal
	// SaveLayer — the mask stays nil there so composite is a straight
	// blit through paint.BlendMode.
	clipMask *image.Alpha
}

// layerStack is the imageCanvas-side companion to canvasState. Stored
// via *layerStack so value-copies of imageCanvas (the Canvas interface
// passes it around by value) share the same stack — pushing a layer
// inside a widget's Draw is visible to the outer widget's continuation.
type layerStack struct {
	frames []layerFrame
}

func newLayerStack() *layerStack { return &layerStack{} }

// push adds a layer frame. Cheap; the offscreen has already been
// allocated by the caller.
func (s *layerStack) push(f layerFrame) {
	s.frames = append(s.frames, f)
}

// peek returns the top-most layer frame or nil.
func (s *layerStack) peek() *layerFrame {
	if s == nil || len(s.frames) == 0 {
		return nil
	}
	return &s.frames[len(s.frames)-1]
}

// pop removes and returns the top-most layer frame. The caller is
// responsible for actually compositing it back onto layerFrame.outer.
func (s *layerStack) pop() (layerFrame, bool) {
	if s == nil || len(s.frames) == 0 {
		return layerFrame{}, false
	}
	f := s.frames[len(s.frames)-1]
	s.frames = s.frames[:len(s.frames)-1]
	return f, true
}

// activeImg returns the innermost active layer's offscreen buffer, or
// `base` if no layers are pushed. This is what every rasterizer inside
// imageCanvas resolves as its draw target — the c.img field is only
// the fallback when the layer stack is empty.
func (s *layerStack) activeImg(base *image.RGBA) *image.RGBA {
	if s == nil || len(s.frames) == 0 {
		return base
	}
	return s.frames[len(s.frames)-1].img
}

// compositeLayer blits `src` onto `dst` using the Porter-Duff operator
// in `paint.BlendMode` and multiplying src alpha (and per-channel
// premultiplied RGB) by paint.effectiveAlpha(). Iterates only over
// src.Bounds() (which is aligned to dst's device space), so a small
// layer is cheap even when the framebuffer is huge.
//
// Both buffers are treated as PREMULTIPLIED RGBA. This matches what
// the rasterizers actually write — fillRectBlend does
// `r = R*A/255` before the blend, drawCornerAA / blendPixel use
// `src.R * a` where `a` already carries alpha, and so on. Framebuffer
// pixels happen to always end with alpha=255 (the window pre-clears),
// so premul(255) == straight(255) at the final read — the CPU blit +
// GL texture upload sees the same bytes either way. Layer buffers with
// translucent content DO store premul, and nested SaveLayer chains
// require premul semantics to compose consistently with a
// single-layer flat composite.
func compositeLayer(dst, src *image.RGBA, paint Paint) {
	compositeLayerMasked(dst, src, nil, paint)
}

// compositeLayerMasked is compositeLayer with a per-pixel alpha mask
// applied to `src` before the blend. Consumed by ClipPath's Restore
// step: the clip mask multiplies premultiplied src channels
// pixel-wise, then the normal Porter-Duff composite runs. mask.Bounds()
// must line up with src.Bounds() — a nil mask is the no-mask fast path
// and matches compositeLayer's behavior byte-for-byte.
func compositeLayerMasked(dst, src *image.RGBA, mask *image.Alpha, paint Paint) {
	compositeLayerClipped(dst, src, mask, paint, src.Bounds())
}

// compositeLayerClipped is compositeLayerMasked confined to `clip` (device
// space). Callers that pop a layer pass the clip the layer was pushed
// under, so an ImageFilter that grew the buffer still cannot write outside
// the region the frame invalidated.
func compositeLayerClipped(dst, src *image.RGBA, mask *image.Alpha, paint Paint, clip image.Rectangle) {
	if dst == nil || src == nil {
		return
	}
	region := src.Bounds().Intersect(dst.Bounds()).Intersect(clip)
	if region.Empty() {
		return
	}
	opacity := paint.effectiveAlpha()
	if opacity <= 0 {
		return
	}
	mode := paint.BlendMode
	var maskPix []uint8
	var maskStride int
	var maskMin image.Point
	if mask != nil {
		maskPix = mask.Pix
		maskStride = mask.Stride
		maskMin = mask.Rect.Min
	}
	dstStride := dst.Stride
	srcStride := src.Stride
	dstPix := dst.Pix
	srcPix := src.Pix
	for y := region.Min.Y; y < region.Max.Y; y++ {
		dstRow := (y-dst.Rect.Min.Y)*dstStride - dst.Rect.Min.X*4
		srcRow := (y-src.Rect.Min.Y)*srcStride - src.Rect.Min.X*4
		var maskRow int
		if mask != nil {
			maskRow = (y-maskMin.Y)*maskStride - maskMin.X
		}
		for x := region.Min.X; x < region.Max.X; x++ {
			di := dstRow + x*4
			si := srcRow + x*4
			// Read premul, apply opacity uniformly (premul preserves the
			// straight color under a scalar multiply because R_p = R_s*A_s
			// and scaling A_s by k also scales R_p by k).
			m := float32(1)
			if mask != nil {
				m = float32(maskPix[maskRow+x]) / 255
			}
			opa := opacity * m
			sr := float32(srcPix[si+0]) / 255 * opa
			sg := float32(srcPix[si+1]) / 255 * opa
			sb := float32(srcPix[si+2]) / 255 * opa
			sa := float32(srcPix[si+3]) / 255 * opa
			if sa <= 0 && mode == BlendSrcOver {
				continue
			}
			dr := float32(dstPix[di+0]) / 255
			dg := float32(dstPix[di+1]) / 255
			db := float32(dstPix[di+2]) / 255
			da := float32(dstPix[di+3]) / 255
			or, og, ob, oa := blendPorterDuff(mode, sr, sg, sb, sa, dr, dg, db, da)
			dstPix[di+0] = clamp255(or * 255)
			dstPix[di+1] = clamp255(og * 255)
			dstPix[di+2] = clamp255(ob * 255)
			dstPix[di+3] = clamp255(oa * 255)
		}
	}
}

// blendPorterDuff evaluates the Porter-Duff / blend equation for the
// given mode. All inputs and outputs are premultiplied RGBA in the
// [0, 1] range. The formulas are the standard premul forms from the
// CSS Compositing and Blending L1 spec; every branch preserves the
// invariant that RGB ≤ A when both inputs already satisfied it.
//
// The mode set is the fixed subset defined by BlendMode. Adding a mode
// means an entry here + an entry in the enum + a test — the
// framework-level plumbing in DrawShape / imageCanvas.SaveLayer does
// not need to change.
func blendPorterDuff(mode BlendMode, sr, sg, sb, sa, dr, dg, db, da float32) (float32, float32, float32, float32) {
	switch mode {
	case BlendSrc:
		return sr, sg, sb, sa
	case BlendDstIn:
		return dr * sa, dg * sa, db * sa, da * sa
	case BlendDstOut:
		ia := 1 - sa
		return dr * ia, dg * ia, db * ia, da * ia
	case BlendMultiply:
		// Skia / CSS "multiply" in premul:
		//   Cout = Cs*(1-Da) + Cd*(1-Sa) + Cs*Cd
		//   Aout = Sa + Da*(1-Sa)  (standard SrcOver alpha)
		ia := 1 - sa
		iad := 1 - da
		return sr*iad + dr*ia + sr*dr,
			sg*iad + dg*ia + sg*dg,
			sb*iad + db*ia + sb*db,
			sa + da*ia
	case BlendScreen:
		// Skia / CSS "screen" in premul:
		//   Cout = Cs + Cd - Cs*Cd
		//   Aout = Sa + Da - Sa*Da
		return sr + dr - sr*dr,
			sg + dg - sg*dg,
			sb + db - sb*db,
			sa + da - sa*da
	case BlendPlus:
		return sr + dr, sg + dg, sb + db, sa + da
	}
	// BlendSrcOver — the default. Premultiplied SrcOver:
	//   Cout = Cs + Cd*(1-Sa)
	//   Aout = Sa + Da*(1-Sa)
	ia := 1 - sa
	return sr + dr*ia, sg + dg*ia, sb + db*ia, sa + da*ia
}

func clamp255(v float32) uint8 {
	if v <= 0 {
		return 0
	}
	if v >= 255 {
		return 255
	}
	return uint8(v)
}
