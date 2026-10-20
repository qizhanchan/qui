//go:build js && wasm

package web

import (
	"image"
	"math"
	"strconv"
	"strings"
	"syscall/js"

	"github.com/qizhanchan/qui/internal/platform/contract"
)

// browserText is the web backend's contract.TextFallback: text the wasm
// build's embedded fonts cannot draw — Chinese, Japanese, Korean, most
// non-Latin scripts — is measured and rasterized by the browser's canvas
// text engine, with the browser's default fonts (PingFang / Microsoft
// YaHei / Noto CJK, whatever the system provides). Without it a wasm app
// has no CJK glyphs at all and renders tofu.
//
// It works per grapheme cluster (see contract.TextFallback), which is
// exact for CJK, where every cluster is an independent glyph.
type browserText struct {
	canvas js.Value
	ctx    js.Value
	font   string // last ctx.font set, to skip redundant assignments
}

func newBrowserText() *browserText {
	var canvas js.Value
	if oc := js.Global().Get("OffscreenCanvas"); oc.Truthy() {
		canvas = oc.New(1, 1)
	} else {
		canvas = js.Global().Get("document").Call("createElement", "canvas")
	}
	opts := js.Global().Get("Object").New()
	opts.Set("willReadFrequently", true)
	ctx := canvas.Call("getContext", "2d", opts)
	if !ctx.Truthy() {
		return nil
	}
	return &browserText{canvas: canvas, ctx: ctx}
}

// cssFont is the CSS font shorthand for f. The engine's family (if any)
// comes first; the browser falls back from it to its default sans-serif
// stack, which is what carries CJK.
func cssFont(f contract.FontDesc) string {
	var b strings.Builder
	if f.Italic {
		b.WriteString("italic ")
	}
	weight := f.Weight
	if weight <= 0 {
		weight = 400
	}
	b.WriteString(strconv.Itoa(weight))
	b.WriteByte(' ')
	b.WriteString(strconv.FormatFloat(float64(f.Size), 'f', 2, 32))
	b.WriteString("px ")
	if f.Family != "" {
		b.WriteString(strconv.Quote(f.Family))
		b.WriteString(", ")
	}
	b.WriteString("system-ui, sans-serif")
	return b.String()
}

func (t *browserText) setFont(f contract.FontDesc) {
	if css := cssFont(f); css != t.font {
		t.ctx.Set("font", css)
		t.font = css
	}
}

func (t *browserText) Advance(cluster string, f contract.FontDesc) (float32, bool) {
	t.setFont(f)
	w := t.ctx.Call("measureText", cluster).Get("width").Float()
	return float32(w), w > 0
}

func (t *browserText) Mask(cluster string, f contract.FontDesc) (*image.Alpha, int, int, bool) {
	t.setFont(f)
	m := t.ctx.Call("measureText", cluster)
	num := func(name string, def float64) float64 {
		if v := m.Get(name); v.Type() == js.TypeNumber {
			return v.Float()
		}
		return def
	}
	advance := num("width", 0)
	if advance <= 0 {
		return nil, 0, 0, false
	}
	// Ink can overhang the advance (italics, wide punctuation), so size the
	// mask by the larger of the two plus a pixel of padding for AA.
	left := math.Max(num("actualBoundingBoxLeft", 0), 0)
	right := math.Max(num("actualBoundingBoxRight", advance), advance)
	ascent := math.Max(num("fontBoundingBoxAscent", num("actualBoundingBoxAscent", float64(f.Size))), num("actualBoundingBoxAscent", 0))
	descent := math.Max(num("fontBoundingBoxDescent", num("actualBoundingBoxDescent", float64(f.Size)*0.25)), num("actualBoundingBoxDescent", 0))
	const pad = 1
	originX := int(math.Ceil(left)) + pad
	baseline := int(math.Ceil(ascent)) + pad
	w := originX + int(math.Ceil(right)) + pad
	h := baseline + int(math.Ceil(descent)) + pad
	if w <= 0 || h <= 0 || w > 4096 || h > 4096 {
		return nil, 0, 0, false
	}
	// Resizing a canvas resets its context state, the font included.
	t.canvas.Set("width", w)
	t.canvas.Set("height", h)
	t.font = ""
	t.setFont(f)
	t.ctx.Set("fillStyle", "#fff")
	t.ctx.Set("textBaseline", "alphabetic")
	t.ctx.Call("clearRect", 0, 0, w, h)
	t.ctx.Call("fillText", cluster, originX, baseline)
	data := t.ctx.Call("getImageData", 0, 0, w, h).Get("data")
	rgba := make([]byte, w*h*4)
	js.CopyBytesToGo(rgba, data)
	mask := image.NewAlpha(image.Rect(0, 0, w, h))
	for i := range mask.Pix {
		mask.Pix[i] = rgba[i*4+3]
	}
	return mask, originX, baseline, true
}

// ColorImage implements contract.ColorGlyphs: the browser draws emoji with
// its own color emoji font (Apple Color Emoji, Segoe UI Emoji, Noto Color
// Emoji), whatever fillStyle says. The image is cropped to the advance and
// one line of the font's height, which is what the engine's emoji layout
// expects of a provider.
func (t *browserText) ColorImage(cluster string, f contract.FontDesc) (*image.RGBA, bool) {
	f.Family = ""
	t.setFont(f)
	m := t.ctx.Call("measureText", cluster)
	advance := m.Get("width").Float()
	if advance <= 0 {
		return nil, false
	}
	num := func(name string, def float64) float64 {
		if v := m.Get(name); v.Type() == js.TypeNumber {
			return v.Float()
		}
		return def
	}
	ascent := num("fontBoundingBoxAscent", float64(f.Size)*0.9)
	descent := num("fontBoundingBoxDescent", float64(f.Size)*0.25)
	w := int(math.Ceil(advance))
	h := int(math.Ceil(ascent + descent))
	if w <= 0 || h <= 0 || w > 4096 || h > 4096 {
		return nil, false
	}
	t.canvas.Set("width", w)
	t.canvas.Set("height", h)
	t.font = ""
	t.setFont(f)
	t.ctx.Set("fillStyle", "#000")
	t.ctx.Set("textBaseline", "alphabetic")
	t.ctx.Call("clearRect", 0, 0, w, h)
	t.ctx.Call("fillText", cluster, 0, math.Ceil(ascent))
	data := t.ctx.Call("getImageData", 0, 0, w, h).Get("data")
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	js.CopyBytesToGo(img.Pix, data)
	// ImageData is straight alpha; image.RGBA is premultiplied.
	empty := true
	for i := 0; i+3 < len(img.Pix); i += 4 {
		a := uint32(img.Pix[i+3])
		if a == 0 {
			continue
		}
		empty = false
		if a < 255 {
			img.Pix[i] = uint8(uint32(img.Pix[i]) * a / 255)
			img.Pix[i+1] = uint8(uint32(img.Pix[i+1]) * a / 255)
			img.Pix[i+2] = uint8(uint32(img.Pix[i+2]) * a / 255)
		}
	}
	return img, !empty
}
