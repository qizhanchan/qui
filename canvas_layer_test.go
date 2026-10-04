package qui

import (
	"image"
	"testing"
)

// SaveLayer semantics — drawn pixels compose back through the layer's
// paint. The first test is the base case: opaque red drawn inside a
// SaveLayer at Alpha=0.5 should composite onto a black background as a
// 50% red / 50% black pixel.
func TestSaveLayerAlphaComposite(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 4, 4))
	// Fill the framebuffer with opaque black so we can assert against a
	// known destination.
	for y := 0; y < 4; y++ {
		for x := 0; x < 4; x++ {
			img.Pix[y*img.Stride+x*4+3] = 255
		}
	}
	c := NewImageCanvas(img)

	depth := c.SaveLayer(Rect{W: 4, H: 4}, Paint{Alpha: 0.5})
	c.FillRect(Rect{W: 4, H: 4}, Color{R: 1, G: 0, B: 0, A: 1})
	c.RestoreTo(depth)

	// Center pixel should be roughly (128, 0, 0, 255): 50% red over black.
	i := img.PixOffset(2, 2)
	got := [4]uint8{img.Pix[i], img.Pix[i+1], img.Pix[i+2], img.Pix[i+3]}
	if abs(int(got[0])-128) > 2 || got[1] != 0 || got[2] != 0 || got[3] != 255 {
		t.Fatalf("center pixel = %v, want ~(128, 0, 0, 255)", got)
	}
}

// A SaveLayer with the default (unset) paint should behave as a pass-
// through: draws inside the layer look identical to draws that
// bypassed SaveLayer entirely. Guards against effectiveAlpha treating
// zero-value Paint.Alpha as "hidden layer" — the intended behavior is
// to treat unset alpha as 1.0 (opaque pass-through).
func TestSaveLayerDefaultPaintIsPassthrough(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 4, 4))
	c := NewImageCanvas(img)

	depth := c.SaveLayer(Rect{W: 4, H: 4}, Paint{})
	c.FillRect(Rect{W: 4, H: 4}, Color{R: 0.5, G: 0, B: 0, A: 1})
	c.RestoreTo(depth)

	i := img.PixOffset(2, 2)
	got := [4]uint8{img.Pix[i], img.Pix[i+1], img.Pix[i+2], img.Pix[i+3]}
	// Rendering half-red on a transparent layer: layer buffer has (128, 0, 0, 255)
	// after fill, then SrcOver composite onto empty framebuffer keeps it.
	if abs(int(got[0])-128) > 2 || got[1] != 0 || got[2] != 0 || got[3] != 255 {
		t.Fatalf("pass-through layer center pixel = %v, want ~(128, 0, 0, 255)", got)
	}
}

// Nested SaveLayer chains composite inside-out — the innermost layer's
// draws end up on the outermost target through both compositing steps.
func TestSaveLayerNested(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 4, 4))
	for y := 0; y < 4; y++ {
		for x := 0; x < 4; x++ {
			img.Pix[y*img.Stride+x*4+3] = 255
		}
	}
	c := NewImageCanvas(img)

	outer := c.SaveLayer(Rect{W: 4, H: 4}, Paint{Alpha: 1})
	inner := c.SaveLayer(Rect{W: 4, H: 4}, Paint{Alpha: 0.5})
	c.FillRect(Rect{W: 4, H: 4}, Color{R: 1, G: 0, B: 0, A: 1})
	c.RestoreTo(inner)
	c.RestoreTo(outer)

	i := img.PixOffset(2, 2)
	got := [4]uint8{img.Pix[i], img.Pix[i+1], img.Pix[i+2], img.Pix[i+3]}
	// Inner composite: 50% red onto layer (empty) → (128, 0, 0, 128).
	// Outer composite: (128, 0, 0, 128) SrcOver black → (128, 0, 0, 255).
	if abs(int(got[0])-128) > 3 || got[1] != 0 || got[2] != 0 || got[3] != 255 {
		t.Fatalf("nested center pixel = %v, want ~(128, 0, 0, 255)", got)
	}
}

// SaveLayer bounds clip the offscreen — pixels outside the requested
// bounds are not drawn by anything inside the layer. Draws that fall
// outside the layer's declared bounds are silently discarded, matching
// Skia's contract.
func TestSaveLayerBoundsClipsDraws(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 8, 8))
	c := NewImageCanvas(img)

	depth := c.SaveLayer(Rect{X: 2, Y: 2, W: 4, H: 4}, Paint{})
	c.FillRect(Rect{W: 8, H: 8}, Color{R: 1, G: 0, B: 0, A: 1})
	c.RestoreTo(depth)

	// Pixel (0, 0) is outside the layer bounds — should stay transparent.
	i := img.PixOffset(0, 0)
	got := [4]uint8{img.Pix[i], img.Pix[i+1], img.Pix[i+2], img.Pix[i+3]}
	if got[3] != 0 {
		t.Fatalf("outside-bounds pixel = %v, want transparent", got)
	}
	// Pixel (4, 4) is inside the layer bounds — should be red.
	i = img.PixOffset(4, 4)
	got = [4]uint8{img.Pix[i], img.Pix[i+1], img.Pix[i+2], img.Pix[i+3]}
	if got[0] < 250 {
		t.Fatalf("inside-bounds pixel = %v, want ~red", got)
	}
}

// BlendMode dispatch — each Porter-Duff operator produces the expected
// premultiplied output. Inputs are premul: red-at-50%-opacity src and
// fully-opaque blue dst.
func TestBlendPorterDuff(t *testing.T) {
	// Premul src = straight(1,0,0,0.5) * 0.5 = (0.5, 0, 0, 0.5)
	// Premul dst = straight(0,0,1,1)   * 1   = (0, 0, 1, 1)
	sr, sg, sb, sa := float32(0.5), float32(0), float32(0), float32(0.5)
	dr, dg, db, da := float32(0), float32(0), float32(1), float32(1)
	cases := []struct {
		mode BlendMode
		want [4]float32 // R, G, B, A (premul)
		name string
	}{
		{
			// SrcOver premul: out = src + dst*(1-sa)
			// R: 0.5 + 0*0.5 = 0.5; B: 0 + 1*0.5 = 0.5; A: 0.5 + 1*0.5 = 1
			mode: BlendSrcOver,
			want: [4]float32{0.5, 0, 0.5, 1},
			name: "SrcOver",
		},
		{
			// Src passthrough
			mode: BlendSrc,
			want: [4]float32{0.5, 0, 0, 0.5},
			name: "Src",
		},
		{
			// DstIn premul: out = dst * sa
			mode: BlendDstIn,
			want: [4]float32{0, 0, 0.5, 0.5},
			name: "DstIn",
		},
		{
			// DstOut premul: out = dst * (1 - sa)
			mode: BlendDstOut,
			want: [4]float32{0, 0, 0.5, 0.5},
			name: "DstOut",
		},
		{
			// Plus: out = src + dst (may exceed 1; clamped by caller)
			mode: BlendPlus,
			want: [4]float32{0.5, 0, 1, 1.5},
			name: "Plus",
		},
		{
			// Screen premul: out = src + dst - src*dst
			// R: 0.5 + 0 - 0 = 0.5; B: 0 + 1 - 0 = 1; A: 0.5 + 1 - 0.5 = 1
			mode: BlendScreen,
			want: [4]float32{0.5, 0, 1, 1},
			name: "Screen",
		},
	}
	for _, tc := range cases {
		r, g, b, a := blendPorterDuff(tc.mode, sr, sg, sb, sa, dr, dg, db, da)
		got := [4]float32{r, g, b, a}
		for i := range got {
			d := got[i] - tc.want[i]
			if d < 0 {
				d = -d
			}
			if d > 0.01 {
				t.Errorf("%s: got %v, want %v", tc.name, got, tc.want)
				break
			}
		}
	}
}

// Restore balancing — a SaveLayer + a plain Save + Restore + Restore
// should leave the state stack at the original depth AND composite the
// layer. Guards against a bug where Restore forgot to check for layer
// frames on the top and left them dangling.
func TestSaveLayerRestoreBalancing(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 4, 4))
	c := NewImageCanvas(img)

	initialDepth := c.Save()
	c.Restore()
	if c.CurrentMatrix() != IdentityMatrix() {
		t.Fatalf("initial matrix corrupted")
	}
	_ = initialDepth

	layerDepth := c.SaveLayer(Rect{W: 4, H: 4}, Paint{Alpha: 0.5})
	innerDepth := c.Save()
	c.FillRect(Rect{W: 4, H: 4}, Color{R: 1, A: 1})
	// Two Restores to undo Save + SaveLayer. The layer composite should
	// run when we cross the layer's depth.
	c.Restore()
	c.Restore()
	_ = innerDepth
	_ = layerDepth

	i := img.PixOffset(2, 2)
	got := [4]uint8{img.Pix[i], img.Pix[i+1], img.Pix[i+2], img.Pix[i+3]}
	if abs(int(got[0])-128) > 2 {
		t.Fatalf("post-balanced-restore red channel = %d, want ~128", got[0])
	}
}

// The per-frame dirty region is snapped out to whole physical pixels.
// Widget bounds are fractional, and a clip that splits a pixel makes the
// frame's primitives disagree about it: an AA fill covers such a pixel
// partially while a layer composite is a per-pixel blit that takes it
// whole. The visible symptom was a one-pixel line of the content UNDER a
// widget along the dirty rect's edge.
func TestSnapRectOutwardAlignsToPhysicalPixels(t *testing.T) {
	in := Rect{X: 475.27, Y: 547.53, W: 140.58, H: 32}
	// A menu row at DPR 2: 547.53 → 1095.06 and 579.53 → 1159.06 both
	// split a physical pixel and must grow out to 1095 / 1160.
	got := snapRectOutward(in, 2, 2)
	for _, edge := range []float32{got.X * 2, got.Y * 2, (got.X + got.W) * 2, (got.Y + got.H) * 2} {
		if edge != float32(int(edge)) {
			t.Errorf("edge %v is not a whole physical pixel (rect %+v)", edge, got)
		}
	}
	// Snapping only ever GROWS: shrinking would drop invalidated pixels.
	if got.X > in.X || got.Y > in.Y ||
		got.X+got.W < in.X+in.W || got.Y+got.H < in.Y+in.H {
		t.Errorf("snapped %+v does not contain %+v", got, in)
	}
	// Already-aligned rects are untouched, and a degenerate scale is a
	// no-op rather than a division by zero.
	aligned := Rect{X: 10, Y: 20, W: 30, H: 40}
	if out := snapRectOutward(aligned, 2, 2); out != aligned {
		t.Errorf("aligned rect changed: %+v", out)
	}
	if out := snapRectOutward(in, 0, 0); out != in {
		t.Errorf("scale 0 should be a no-op, got %+v", out)
	}
}
