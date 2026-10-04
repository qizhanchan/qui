package widgets

import (
	"image"
	"testing"

	. "github.com/qizhanchan/qui"
)

func passwordInput(text string) *Input {
	tf := focusedInput(text)
	tf.Password = true
	return tf
}

// The mask used to render through the font's U+2022. On a system CJK
// primary font (PingFang / Noto CJK / YaHei) that glyph is full-width, so a
// masked field came out ~1.6x wider than the same text unmasked. The mask is
// geometry now, so its width tracks the field's font size instead.
func TestInputPasswordWidthTracksNormalText(t *testing.T) {
	const text = "password123"
	plain := focusedInput(text)
	masked := passwordInput(text)

	plainW := plain.widthUpToRune(len([]rune(text)))
	maskedW := masked.widthUpToRune(len([]rune(text)))
	if plainW <= 0 {
		t.Fatalf("unmasked width = %v, want > 0", plainW)
	}
	// Not equal — different glyphs — but within a quarter of each other, so
	// the two fields read as the same size. The old bullet path was 1.63x.
	if ratio := maskedW / plainW; ratio < 0.75 || ratio > 1.25 {
		t.Errorf("masked/plain width ratio = %.2f, want within [0.75, 1.25] (masked %v, plain %v)",
			ratio, maskedW, plainW)
	}
}

// Every caret path has to agree with the pitch the dots are drawn at,
// otherwise the caret drifts away from the dots as you type.
func TestInputPasswordCaretMetricsAreUniform(t *testing.T) {
	tf := passwordInput("secret")
	pitch := tf.maskPitch()
	for i := 0; i <= 6; i++ {
		if got, want := tf.widthUpToRune(i), float32(i)*pitch; got != want {
			t.Errorf("widthUpToRune(%d) = %v, want %v", i, got, want)
		}
	}
	// Past the end clamps to the rune count rather than running off.
	if got, want := tf.widthUpToRune(99), 6*pitch; got != want {
		t.Errorf("widthUpToRune(99) = %v, want %v", got, want)
	}
}

func TestInputPasswordHitTestSnapsToDots(t *testing.T) {
	tf := passwordInput("secret")
	content := tf.inputBounds()
	pitch := tf.maskPitch()
	for i := 0; i <= 6; i++ {
		// Click just past the middle of dot i-1 lands on boundary i.
		x := content.X + float32(i)*pitch
		if got := tf.runeIndexAt(x); got != i {
			t.Errorf("runeIndexAt(boundary %d) = %d, want %d", i, got, i)
		}
	}
	// Well left of the field clamps to 0, well right clamps to the end.
	if got := tf.runeIndexAt(content.X - 500); got != 0 {
		t.Errorf("runeIndexAt(far left) = %d, want 0", got)
	}
	if got := tf.runeIndexAt(content.X + 5000); got != 6 {
		t.Errorf("runeIndexAt(far right) = %d, want 6", got)
	}
}

func TestInputPasswordArrowKeysStepOneRune(t *testing.T) {
	tf := passwordInput("héllo") // non-ASCII: step runes, not bytes
	tf.cursorPos = 5
	tf.Handle(NewKeyEvent(EventKeyDown, KeyLeft, 0))
	if tf.cursorPos != 4 {
		t.Errorf("after Left cursorPos = %d, want 4", tf.cursorPos)
	}
	tf.Handle(NewKeyEvent(EventKeyDown, KeyRight, 0))
	if tf.cursorPos != 5 {
		t.Errorf("after Right cursorPos = %d, want 5", tf.cursorPos)
	}
	// Clamped at both ends.
	tf.cursorPos = 0
	tf.Handle(NewKeyEvent(EventKeyDown, KeyLeft, 0))
	if tf.cursorPos != 0 {
		t.Errorf("Left at start = %d, want 0", tf.cursorPos)
	}
	tf.cursorPos = 5
	tf.Handle(NewKeyEvent(EventKeyDown, KeyRight, 0))
	if tf.cursorPos != 5 {
		t.Errorf("Right at end = %d, want 5", tf.cursorPos)
	}
}

func renderInput(t *testing.T, tf *Input, w, h int) *image.RGBA {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	tf.Layout(Rect{X: 0, Y: 0, W: float32(w), H: float32(h)})
	tf.Draw(NewImageCanvas(img))
	return img
}

// contentInkColumns reports which x columns carry text/dot ink, and how many
// contiguous runs of them there are. build() is rendered twice — once as
// given, once emptied — and the two are diffed, so the field's background,
// border and active indicator cancel out and only the content remains.
func contentInkColumns(t *testing.T, build func() *Input, w, h int) (cols []int, runs int) {
	t.Helper()
	got := renderInput(t, build(), w, h)
	blank := build()
	blank.Text = ""
	blank.Placeholder = ""
	base := renderInput(t, blank, w, h)

	prev := false
	for x := 0; x < w; x++ {
		inked := false
		for y := 0; y < h; y++ {
			if pixelsDiffer(got, base, x, y) {
				inked = true
				break
			}
		}
		if inked {
			cols = append(cols, x)
			if !prev {
				runs++
			}
		}
		prev = inked
	}
	return cols, runs
}

// contentInkHeight is contentInkColumns' vertical twin: the height of the
// content ink's bounding box.
func contentInkHeight(t *testing.T, build func() *Input, w, h int) float32 {
	t.Helper()
	top, bottom := contentInkRows(t, build, w, h)
	if top < 0 {
		return 0
	}
	return float32(bottom - top + 1)
}

// contentInkCenterY is the vertical midpoint of the content ink.
func contentInkCenterY(t *testing.T, build func() *Input, w, h int) float32 {
	t.Helper()
	top, bottom := contentInkRows(t, build, w, h)
	if top < 0 {
		t.Fatal("no content ink to measure")
	}
	return (float32(top) + float32(bottom)) / 2
}

func contentInkRows(t *testing.T, build func() *Input, w, h int) (top, bottom int) {
	t.Helper()
	got := renderInput(t, build(), w, h)
	blank := build()
	blank.Text = ""
	blank.Placeholder = ""
	base := renderInput(t, blank, w, h)

	top, bottom = -1, -1
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			if pixelsDiffer(got, base, x, y) {
				if top < 0 {
					top = y
				}
				bottom = y
				break
			}
		}
	}
	return top, bottom
}

func pixelsDiffer(a, b *image.RGBA, x, y int) bool {
	ar, ag, ab, aa := a.At(x, y).RGBA()
	br, bg, bb, ba := b.At(x, y).RGBA()
	return ar != br || ag != bg || ab != bb || aa != ba
}

// The visible result: one dot per character, each a solid blob rather than a
// speck floating in a full-width cell.
func TestInputPasswordDrawsOneDotPerRune(t *testing.T) {
	build := func() *Input {
		tf := passwordInput("abcd")
		tf.SetFocused(false) // no caret — it would add an ink column of its own
		return tf
	}
	cols, runs := contentInkColumns(t, build, 200, 56)
	if len(cols) == 0 {
		t.Fatal("masked field drew no ink")
	}
	if runs != 4 {
		t.Errorf("ink runs = %d, want 4 (one dot per rune); columns %v", runs, cols)
	}
	// Each dot spans about maskDotRadiusEm*2 of the font size. Allow AA slop.
	wantDot := build().maskDotRadius() * 2
	gotDot := float32(len(cols)) / float32(runs)
	if gotDot < wantDot-1.5 || gotDot > wantDot+1.5 {
		t.Errorf("mean dot width = %.2f px, want ~%.2f", gotDot, wantDot)
	}
}

// The dots have to sit on the same optical line the field's text would.
// Centering them on the content box puts them ~2px high, because the text
// path centers on the cap-height box while a lowercase run's ink sits in the
// x-height band.
func TestInputPasswordDotsAlignWithTextBaseline(t *testing.T) {
	mask := func() *Input {
		tf := passwordInput("oooo")
		tf.SetFocused(false)
		return tf
	}
	text := func() *Input {
		tf := focusedInput("oooo") // all x-height, no ascenders or descenders
		tf.SetFocused(false)
		return tf
	}
	textMid := contentInkCenterY(t, text, 260, 56)
	maskMid := contentInkCenterY(t, mask, 260, 56)
	if diff := textMid - maskMid; diff < -1.5 || diff > 1.5 {
		t.Errorf("mask ink center = %.2f, text ink center = %.2f (off by %.2f px, want within 1.5)",
			maskMid, textMid, diff)
	}
}

func TestInputPasswordPlaceholderIsNotMasked(t *testing.T) {
	build := func() *Input {
		tf := NewInput("placeholder")
		tf.Password = true
		tf.SetFocused(false)
		return tf
	}
	// Empty text: the placeholder must render as real glyphs. Assert on ink
	// HEIGHT rather than glyph count — how many runs "placeholder" breaks into
	// depends on the font's letter spacing (another test in this package swaps
	// the global font), but any text has ascenders/descenders and so stands
	// clearly taller than the mask's flat 2*maskDotRadiusEm band.
	dot := build().maskDotRadius() * 2
	if got := contentInkHeight(t, build, 200, 56); got <= dot*1.5 {
		t.Errorf("placeholder ink height = %.2f px, want well above a %.2f px dot (placeholder was masked?)", got, dot)
	}
}

// Text is never mutated by masking — masking is a paint-time concern.
func TestInputPasswordKeepsRealText(t *testing.T) {
	tf := passwordInput("")
	for _, r := range "s3cr3t" {
		tf.Handle(newCharEvent(r))
	}
	if tf.Text != "s3cr3t" || tf.GetText() != "s3cr3t" {
		t.Errorf("Text = %q / GetText = %q, want s3cr3t", tf.Text, tf.GetText())
	}
}
