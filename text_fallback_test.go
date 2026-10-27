package qui

import (
	"image"
	"io"
	"strings"
	"testing"

	"github.com/qizhanchan/qui/internal/platform/contract"
	"github.com/qizhanchan/qui/internal/typeset"
)

// fakeTextFallback stands in for a platform text engine: every cluster is
// 10px wide and rasterizes to a solid 8×10 block.
type fakeTextFallback struct{ asked []string }

func (f *fakeTextFallback) Advance(cluster string, d contract.FontDesc) (float32, bool) {
	f.asked = append(f.asked, cluster)
	return 10, true
}

func (f *fakeTextFallback) Mask(cluster string, d contract.FontDesc) (*image.Alpha, int, int, bool) {
	m := image.NewAlpha(image.Rect(0, 0, 8, 10))
	for i := range m.Pix {
		m.Pix[i] = 255
	}
	return m, 0, 9, true
}

// uncoveredRune is a private-use code point no loaded font draws.
const uncoveredRune = ''

func withTextFallback(t *testing.T, tf contract.TextFallback) {
	t.Helper()
	if typeset.TextFacesHaveGlyph(uncoveredRune) {
		t.Skip("a system font covers U+E000; cannot exercise the fallback")
	}
	typeset.SetTextFallback(tf)
	t.Cleanup(func() { typeset.SetTextFallback(nil) })
}

func TestTextFallbackSuppliesAdvanceForUncoveredRunes(t *testing.T) {
	spec := Font{Size: 14}
	without := BuildTextLayout("a"+string(uncoveredRune)+"b", spec, TextLayoutOptions{}).Width
	tf := &fakeTextFallback{}
	withTextFallback(t, tf)
	base := BuildTextLayout("ab", spec, TextLayoutOptions{}).Width
	with := BuildTextLayout("a"+string(uncoveredRune)+"b", spec, TextLayoutOptions{}).Width
	if diff := with - base; diff < 9.5 || diff > 10.5 {
		t.Fatalf("fallback cluster should add its 10px advance: base %v, with %v (without fallback %v)", base, with, without)
	}
	for _, c := range tf.asked {
		if strings.ContainsAny(c, "ab") {
			t.Fatalf("covered text %q must not reach the fallback", c)
		}
	}
}

func TestTextFallbackDrawsInTheTextColor(t *testing.T) {
	withTextFallback(t, &fakeTextFallback{})
	img := image.NewRGBA(image.Rect(0, 0, 60, 30))
	c := NewImageCanvas(img)
	red := Color{R: 1, A: 1}
	c.DrawText(string(uncoveredRune), Rect{X: 2, Y: 2, W: 50, H: 24}, red, Font{Size: 14})
	found := false
	for i := 0; i+3 < len(img.Pix); i += 4 {
		if img.Pix[i] == 255 && img.Pix[i+1] == 0 && img.Pix[i+3] == 255 {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("fallback glyph was not drawn as an opaque red mask")
	}
}

func TestWindowIMEHandlerHooks(t *testing.T) {
	w := NewTestWindow(Size{W: 200, H: 50})
	if _, _, _, _, ok := w.imeCaretRect(); ok {
		t.Fatal("no IME client focused: caret rect must report !ok")
	}
	var h platformHandler = w
	h.onPreedit("ni", 2) // no focused client: must not panic
	h.onCommitText("你")
}

func TestOpenDroppedFilePrefersTheBackend(t *testing.T) {
	f := newFakePlatformWindow()
	w := newWindowOnFake(f)
	if _, err := w.OpenDroppedFile("/dropped/1/missing.txt"); err == nil {
		t.Fatal("a path no backend knows must fall through to os.Open and fail")
	}
	w2 := &Window{plat: &dropOpenerFake{fakePlatformWindow: f}, platformBacked: true, renderer: NoopRenderer{}}
	rc, err := w2.OpenDroppedFile("/dropped/1/a.txt")
	if err != nil {
		t.Fatal(err)
	}
	b, _ := io.ReadAll(rc)
	if string(b) != "hi" {
		t.Fatalf("read %q from backend drop", b)
	}
}

type dropOpenerFake struct{ *fakePlatformWindow }

func (dropOpenerFake) OpenDropped(path string) (io.ReadCloser, bool) {
	if path == "/dropped/1/a.txt" {
		return io.NopCloser(strings.NewReader("hi")), true
	}
	return nil, false
}

// fakeColorFallback also renders color glyphs, so it doubles as the emoji
// provider.
type fakeColorFallback struct{ fakeTextFallback }

func (*fakeColorFallback) ColorImage(cluster string, d contract.FontDesc) (*image.RGBA, bool) {
	img := image.NewRGBA(image.Rect(0, 0, int(d.Size), int(d.Size)))
	for i := 0; i < len(img.Pix); i += 4 {
		img.Pix[i], img.Pix[i+3] = 255, 255
	}
	return img, true
}

func TestColorGlyphFallbackBecomesTheEmojiProvider(t *testing.T) {
	prev := typeset.CurrentEmojiProvider()
	SetEmojiProvider(nil)
	typeset.SetTextFallback(&fakeColorFallback{})
	t.Cleanup(func() { typeset.SetTextFallback(nil); SetEmojiProvider(prev) })
	if _, ok := typeset.CurrentEmojiProvider().(typeset.ColorGlyphEmoji); !ok {
		t.Fatalf("emoji provider = %T, want the color-glyph adapter", typeset.CurrentEmojiProvider())
	}
	spec := Font{Size: 14}
	if w := BuildTextLayout("a😀b", spec, TextLayoutOptions{}).Width - BuildTextLayout("ab", spec, TextLayoutOptions{}).Width; w < 10 {
		t.Fatalf("emoji advance %v, want about the font size", w)
	}
}
