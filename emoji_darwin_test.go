//go:build darwin && cgo

package qui

import "testing"

// Core Text must hand back exactly round(fontSize) pixels wide so
// normalizeEmojiBitmap passes it through untouched: Apple Color Emoji's
// sbix strikes make the natural line 19px wide at 14pt, and shrinking
// that in Go was a second blurring resample.
func TestCoreTextEmojiFitsRequestedWidth(t *testing.T) {
	p := coreTextEmojiProvider{}
	for _, size := range []float32{12, 14, 16, 24, 28, 36, 48} {
		img := p.EmojiImage("\U0001F44C", size)
		if img == nil {
			t.Fatalf("size %v: nil image", size)
		}
		if got, want := img.Bounds().Dx(), int(size+0.5); got != want {
			t.Errorf("size %v: width %d, want %d", size, got, want)
		}
	}
	if p.EmojiImage("\U0001F44C\U0001F3FB", 28).Bounds().Dx() != 28 {
		t.Error("skin-tone cluster not fitted to 28px")
	}
}
