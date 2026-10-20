package contract

import "image"

// FontDesc describes text for a platform text engine: what the engine
// would put in a CSS font shorthand. Size is in the pixels the result is
// measured or rasterized at (device pixels for Mask).
type FontDesc struct {
	Family string
	Size   float32
	Weight int // 100–900; 400 regular, 700 bold
	Italic bool
}

// TextFallback draws text that no font loaded into the engine covers,
// using the platform's own text engine and fonts — the browser's default
// fonts on the web, which carry CJK and most scripts, while a wasm build
// can only see the fonts it embeds.
//
// The engine calls it per grapheme cluster and lays each one out as an
// atomic glyph, exactly like color emoji. That suits scripts without
// shaping across clusters (CJK, symbols); a backend should decline
// (ok=false) what it cannot render alone.
type TextFallback interface {
	// Advance is the cluster's advance width in pixels at f.Size.
	Advance(cluster string, f FontDesc) (advance float32, ok bool)
	// Mask rasterizes the cluster at f.Size device pixels: coverage in
	// the alpha channel, plus where the pen origin (originX) and the
	// baseline (baselineY) fall inside the mask.
	Mask(cluster string, f FontDesc) (mask *image.Alpha, originX, baselineY int, ok bool)
}

// ColorGlyphs is an optional TextFallback ability: rasterizing a cluster
// in its own colors — color emoji — rather than as a coverage mask. The
// engine uses it as its emoji provider when the app has not installed one.
type ColorGlyphs interface {
	// ColorImage renders cluster at f.Size device pixels, premultiplied.
	ColorImage(cluster string, f FontDesc) (*image.RGBA, bool)
}
