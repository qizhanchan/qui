package qui

import (
	"fmt"
	"image"
	"sync"
	"testing"

	"golang.org/x/image/font/gofont/goregular"
)

// setGlyphCacheModeForTest switches the glyph-mask cache mode, empties
// the cache, and restores both on cleanup.
func setGlyphCacheModeForTest(t testing.TB, mode int32) {
	t.Helper()
	prev := glyphCacheMode.Load()
	glyphCacheMode.Store(mode)
	glyphCache.reset()
	t.Cleanup(func() {
		glyphCacheMode.Store(prev)
		glyphCache.reset()
	})
}

func renderWithGlyphMode(t testing.TB, mode int32, w, h int, draw func(Canvas)) *image.RGBA {
	t.Helper()
	prev := glyphCacheMode.Load()
	glyphCacheMode.Store(mode)
	glyphCache.reset()
	defer func() {
		glyphCacheMode.Store(prev)
		glyphCache.reset()
	}()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	draw(NewImageCanvas(img))
	return img
}

type pixelDiff struct {
	max   int
	mean  float64 // mean |Δ| over every channel byte
	ndiff int     // channel bytes that differ
}

func diffRGBA(a, b *image.RGBA) pixelDiff {
	var d pixelDiff
	sum := 0
	for i := range a.Pix {
		v := int(a.Pix[i]) - int(b.Pix[i])
		if v < 0 {
			v = -v
		}
		if v > d.max {
			d.max = v
		}
		if v > 0 {
			d.ndiff++
			sum += v
		}
	}
	d.mean = float64(sum) / float64(len(a.Pix))
	return d
}

// glyphCacheScene exercises every text feature the mask route touches.
func glyphCacheScene(scale, tx, ty float32) func(Canvas) {
	return func(cv Canvas) {
		cv.Clear(Color{R: 1, G: 1, B: 1, A: 1})
		cv.Scale(scale, scale)
		cv.Translate(tx, ty)
		// Plain rows, mixed sizes / weights / ink alpha.
		for i := 0; i < 8; i++ {
			f := Font{Size: float32(11 + i%4*2)}
			f.Bold = i%3 == 1
			f.Italic = i%4 == 2
			col := Color{R: 0.1 * float32(i%3), B: 0.6, A: 1 - 0.15*float32(i%3)}
			cv.DrawText(fmt.Sprintf("Row %d: fi ff Wave 1234.567 =SUM(A%d) gjpqy", i, i),
				Rect{X: 4.3, Y: float32(i)*19 + 0.37*float32(i), W: 380, H: 18}, col, f)
		}
		// ClipRect cutting through glyphs.
		d := cv.Save()
		cv.ClipRect(Rect{X: 10.5, Y: 160, W: 63.3, H: 14})
		cv.DrawText("Clipped cell overflowing text", Rect{X: 12, Y: 158, W: 200, H: 20}, Color{A: 1}, Font{Size: 14})
		cv.RestoreTo(d)
		// SaveLayer with alpha.
		d = cv.SaveLayer(Rect{X: 200, Y: 160, W: 190, H: 40}, Paint{Alpha: 0.5})
		cv.DrawText("Layered fi ff", Rect{X: 205, Y: 165, W: 180, H: 30}, Color{B: 1, A: 1}, Font{Size: 15, Italic: true})
		cv.RestoreTo(d)
		// ClipPath (rounded rect) — layer + mask under the hood.
		d = cv.Save()
		cv.ClipPath(NewPath().AddRRect(Rect{X: 10, Y: 190, W: 120, H: 36}, 12))
		cv.DrawText("ClipPath WWW", Rect{X: 5, Y: 190, W: 200, H: 36}, Color{A: 1}, Font{Size: 18, Bold: true})
		cv.RestoreTo(d)
		// Small caps + letter spacing + word spacing.
		cv.DrawText("Small Caps tracked", Rect{X: 10, Y: 232, W: 300, H: 22}, Color{G: 0.4, A: 0.9},
			Font{Size: 14, SmallCaps: true, LetterSpacing: 1.3, WordSpacing: 2.1})
		// Multi-line.
		cv.DrawText("multi line\ntext block ÄÖÜ\nthird", Rect{X: 220, Y: 210, W: 170, H: 70}, Color{R: 0.3, A: 1}, Font{Size: 12})
	}
}

func TestGlyphCachePixelEquivalence(t *testing.T) {
	for _, scale := range []float32{1, 1.25, 1.5, 2} {
		for _, frac := range [][2]float32{{0, 0}, {0.37, 0.61}} {
			w, h := int(400*scale), int(290*scale)
			draw := glyphCacheScene(scale, frac[0], frac[1])
			legacy := renderWithGlyphMode(t, glyphCacheOff, w, h, draw)
			exact := renderWithGlyphMode(t, glyphCacheExact, w, h, draw)
			bucketed := renderWithGlyphMode(t, glyphCacheBucketed, w, h, draw)
			de, db := diffRGBA(legacy, exact), diffRGBA(legacy, bucketed)
			t.Logf("scale %.2f translate %v: exact max=%d n=%d | bucketed max=%d n=%d mean=%.4f",
				scale, frac, de.max, de.ndiff, db.max, db.ndiff, db.mean)
			if de.max > 1 {
				t.Errorf("scale %.2f translate %v: exact-key mode differs from Path route by %d levels", scale, frac, de.max)
			}
			if db.max > 12 || db.mean >= 0.3 {
				t.Errorf("scale %.2f translate %v: bucketed mode max=%d mean=%.4f (want max<=12, mean<0.3)", scale, frac, db.max, db.mean)
			}
		}
	}
}

// A cached frame must match a cold one exactly (cache hits reproduce the
// mask they were rasterised as).
func TestGlyphCacheWarmMatchesCold(t *testing.T) {
	setGlyphCacheModeForTest(t, glyphCacheBucketed)
	draw := glyphCacheScene(1.5, 0.37, 0.61)
	cold := image.NewRGBA(image.Rect(0, 0, 600, 435))
	draw(NewImageCanvas(cold))
	if n, _ := glyphCache.stats(); n == 0 {
		t.Fatal("glyph cache stayed empty after drawing text")
	}
	warm := image.NewRGBA(cold.Rect)
	draw(NewImageCanvas(warm))
	if d := diffRGBA(cold, warm); d.max != 0 {
		t.Fatalf("warm frame differs from cold frame: max=%d n=%d", d.max, d.ndiff)
	}
}

// Rotation, skew, mirroring and non-uniform scale must keep the Path
// route bit-for-bit.
func TestGlyphCacheNonUniformMatrixFallsBack(t *testing.T) {
	cases := map[string]func(Canvas){
		"rotate":     func(cv Canvas) { cv.Translate(100, 60); cv.Rotate(0.6) },
		"nonuniform": func(cv Canvas) { cv.Scale(1.5, 1) },
		"mirror":     func(cv Canvas) { cv.Translate(300, 0); cv.Scale(-1, 1) },
	}
	for name, setup := range cases {
		draw := func(cv Canvas) {
			cv.Clear(Color{R: 1, G: 1, B: 1, A: 1})
			setup(cv)
			cv.DrawText("Rotated 123 fi", Rect{X: 2, Y: 2, W: 160, H: 22}, Color{R: 0.2, A: 0.8}, Font{Size: 14, Bold: true})
			cv.DrawText("two\nlines", Rect{X: 2, Y: 30, W: 160, H: 40}, Color{A: 1}, Font{Size: 12, Italic: true})
		}
		legacy := renderWithGlyphMode(t, glyphCacheOff, 320, 200, draw)
		prev := glyphCacheMode.Load()
		glyphCacheMode.Store(glyphCacheBucketed)
		glyphCache.reset()
		cached := image.NewRGBA(legacy.Rect)
		draw(NewImageCanvas(cached))
		entries, _ := glyphCache.stats()
		glyphCacheMode.Store(prev)
		glyphCache.reset()
		if d := diffRGBA(legacy, cached); d.max != 0 {
			t.Errorf("%s: output changed with glyph cache enabled (max=%d n=%d)", name, d.max, d.ndiff)
		}
		if entries != 0 {
			t.Errorf("%s: glyph cache used under a non-uniform matrix (%d entries)", name, entries)
		}
	}
}

func TestGlyphCacheResetOnFontRegistration(t *testing.T) {
	setGlyphCacheModeForTest(t, glyphCacheBucketed)
	img := image.NewRGBA(image.Rect(0, 0, 200, 40))
	NewImageCanvas(img).DrawText("Hello", Rect{W: 200, H: 40}, Color{A: 1}, Font{Size: 14})
	if n, _ := glyphCache.stats(); n == 0 {
		t.Fatal("glyph cache stayed empty after drawing text")
	}
	if err := LoadFontVariantFromBytes("qui-glyphcache-test", FontWeightNormal, false, goregular.TTF); err != nil {
		t.Fatal(err)
	}
	if n, b := glyphCache.stats(); n != 0 || b != 0 {
		t.Fatalf("font registration left %d masks (%d bytes) cached", n, b)
	}
}

func TestGlyphCacheEvictionTinyBudget(t *testing.T) {
	setGlyphCacheModeForTest(t, glyphCacheBucketed)
	const budget = 8 << 10
	prev := glyphCache.setBudget(budget)
	t.Cleanup(func() { glyphCache.setBudget(prev) })
	draw := glyphCacheScene(2, 0.37, 0.61)
	img := image.NewRGBA(image.Rect(0, 0, 800, 580))
	draw(NewImageCanvas(img))
	n, b := glyphCache.stats()
	if b > budget {
		t.Fatalf("cache holds %d bytes, budget %d", b, budget)
	}
	if n == 0 {
		t.Fatal("tiny budget cached nothing (masks smaller than budget/8 should stay)")
	}
	// Evicting must not change pixels beyond float rounding: a re-
	// rasterised mask may come from a different instance of its key.
	glyphCache.setBudget(prev)
	glyphCache.reset()
	ref := image.NewRGBA(img.Rect)
	draw(NewImageCanvas(ref))
	if d := diffRGBA(ref, img); d.max > 1 {
		t.Fatalf("output under eviction differs: max=%d", d.max)
	}
	// The LRU keeps the most recent mask.
	small := newGlyphMaskCache(3 * (glyphMaskOverhead + 16))
	small.maxEntry = 1 << 20
	mk := func(i int) *glyphMask {
		return &glyphMask{key: glyphMaskKey{gid: 1, fx: uint32(i)}, w: 2, h: 2, cov: make([]float32, 4)}
	}
	for i := 0; i < 10; i++ {
		small.put(mk(i))
		small.get(mk(0).key) // keep 0 hot
	}
	if small.get(mk(0).key) == nil || small.get(mk(9).key) == nil {
		t.Fatal("LRU evicted a recently used mask")
	}
	if small.get(mk(1).key) != nil {
		t.Fatal("LRU kept a cold mask over budget")
	}
}

// Concurrent offscreen renders (exports, snapshots) share the glyph and
// metrics caches. Run with -race.
func TestGlyphCacheConcurrentRenderRace(t *testing.T) {
	setGlyphCacheModeForTest(t, glyphCacheBucketed)
	scales := []float32{1, 2}
	ref := make([]*image.RGBA, len(scales))
	for i, s := range scales {
		ref[i] = image.NewRGBA(image.Rect(0, 0, int(400*s), int(290*s)))
		glyphCacheScene(s, 0.25, 0.5)(NewImageCanvas(ref[i]))
	}
	glyphCache.reset()
	var wg sync.WaitGroup
	errs := make(chan string, 16)
	for g := 0; g < 6; g++ {
		wg.Add(1)
		go func(g int) {
			defer wg.Done()
			i := g % len(scales)
			for iter := 0; iter < 2; iter++ {
				img := image.NewRGBA(ref[i].Rect)
				glyphCacheScene(scales[i], 0.25, 0.5)(NewImageCanvas(img))
				TextMetrics(fmt.Sprintf("measure %d", g), Font{Size: 13, LetterSpacing: float32(g)})
				BuildTextLayout("wrap me please across lines", Font{Size: 12, WordSpacing: float32(g)}, TextLayoutOptions{MaxWidth: 80})
				// ≤1: which instance of a key rasterises first is racy.
				if d := diffRGBA(ref[i], img); d.max > 1 {
					errs <- fmt.Sprintf("goroutine %d: concurrent render differs (max=%d)", g, d.max)
				}
			}
		}(g)
	}
	wg.Wait()
	close(errs)
	for e := range errs {
		t.Error(e)
	}
}

// x/image's opentype.Face.Metrics lazily fills the face's metrics fields
// on first use; faces are shared, so concurrent first calls on a fresh
// face raced. Drop the face caches each round so every round starts on
// fresh faces, then hit them from several goroutines at once.
func TestFontMetricsConcurrentRace(t *testing.T) {
	fonts := []Font{{Size: 11}, {Size: 13, Bold: true}, {Size: 15, Italic: true}, {Size: 0}}
	want := make([]float32, len(fonts))
	for i, f := range fonts {
		want[i] = float32(fontMetricsFor(f).Height)
	}
	for round := 0; round < 10; round++ {
		fontCacheMu.Lock()
		clearFontFaceCachesUnlocked()
		fontCacheMu.Unlock()
		start := make(chan struct{})
		var wg sync.WaitGroup
		for g := 0; g < 8; g++ {
			wg.Add(1)
			go func(g int) {
				defer wg.Done()
				<-start
				i := g % len(fonts)
				if got := float32(fontMetricsFor(fonts[i]).Height); got != want[i] {
					t.Errorf("font %d: metrics height %v, want %v", i, got, want[i])
				}
				TextXHeightCenterY(Rect{H: 20}, fonts[i])
				TextMetrics("abc", fonts[i])
			}(g)
		}
		close(start)
		wg.Wait()
	}
}

// BuildTextLayout's memo used to key on fontCacheKey alone, which leaves
// out tracking and small caps: a layout measured without letter spacing
// was served for the spaced font (76.9 px instead of 131.9).
func TestTextLayoutMemoKeyIncludesSpacingAndSmallCaps(t *testing.T) {
	text := "spacing matters here"
	base := Font{Size: 14}
	variants := map[string]Font{
		"LetterSpacing": {Size: 14, LetterSpacing: 3},
		"WordSpacing":   {Size: 14, WordSpacing: 9},
		"SmallCaps":     {Size: 14, SmallCaps: true},
	}
	baseW := BuildTextLayout(text, base, TextLayoutOptions{}).Width
	for name, f := range variants {
		got := BuildTextLayout(text, f, TextLayoutOptions{}).Width
		want := buildTextLayoutUncached(text, f, TextLayoutOptions{}).Width
		if got != want {
			t.Errorf("%s: memoized width %.2f, uncached %.2f", name, got, want)
		}
		if got == baseW {
			t.Errorf("%s: width %.2f identical to the unspaced layout — memo key collision", name, got)
		}
	}
}

// --- Benchmarks: a 600-cell spreadsheet frame (30×20 cells of 72×30). ---

type benchTextCell struct {
	text  string
	rect  Rect
	font  Font
	color Color
	bg    Color
}

func benchTextCells() []benchTextCell {
	sizes := []float32{11, 13, 15}
	colors := []Color{{A: 1}, {R: 0.8, A: 1}, {B: 0.7, A: 1}, {G: 0.5, A: 0.8}}
	var cells []benchTextCell
	for r := 0; r < 30; r++ {
		for c := 0; c < 20; c++ {
			i := r*20 + c
			var s string
			switch i % 3 {
			case 0:
				s = fmt.Sprintf("%d.%02d", 1000+i*37, i%100)
			case 1:
				s = fmt.Sprintf("Item %d", i)
			default:
				s = fmt.Sprintf("=SUM(A%d)", i)
			}
			f := Font{Size: sizes[i%3], Bold: i%7 == 0, Italic: i%11 == 0}
			cells = append(cells, benchTextCell{
				text: s, font: f, color: colors[i%4],
				rect: Rect{X: float32(c * 72), Y: float32(r * 30), W: 72, H: 30},
				bg:   Color{R: 1, G: 1, B: 1 - float32(i%2)*0.05, A: 1},
			})
		}
	}
	return cells
}

func benchTextFrame(cv Canvas, cells []benchTextCell) {
	for _, c := range cells {
		cv.FillRect(c.rect, c.bg)
		d := cv.Save()
		cv.ClipRect(c.rect)
		rect := Rect{X: c.rect.X + 3, Y: c.rect.Y, W: c.rect.W - 6, H: c.rect.H}
		if c.text[0] >= '0' && c.text[0] <= '9' {
			w, _ := TextMetrics(c.text, c.font)
			rect.X = c.rect.X + c.rect.W - 3 - w
		}
		cv.DrawText(c.text, rect, c.color, c.font)
		cv.RestoreTo(d)
	}
}

func benchTextFrame600(b *testing.B, scale float32, mode int32, cold bool) {
	setGlyphCacheModeForTest(b, mode)
	cells := benchTextCells()
	img := image.NewRGBA(image.Rect(0, 0, int(1440*scale), int(900*scale)))
	frame := func() {
		cv := NewImageCanvas(img)
		cv.Scale(scale, scale)
		benchTextFrame(cv, cells)
	}
	frame() // warm shaping / layout memos
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if cold {
			glyphCache.reset()
		}
		frame()
	}
	b.StopTimer()
	n, bytes := glyphCache.stats()
	b.ReportMetric(float64(n), "masks")
	b.ReportMetric(float64(bytes)/1e6, "cacheMB")
}

func BenchmarkTextFrame600Legacy1x(b *testing.B) { benchTextFrame600(b, 1, glyphCacheOff, false) }
func BenchmarkTextFrame600Legacy2x(b *testing.B) { benchTextFrame600(b, 2, glyphCacheOff, false) }
func BenchmarkTextFrame600Warm1x(b *testing.B)   { benchTextFrame600(b, 1, glyphCacheBucketed, false) }
func BenchmarkTextFrame600Warm2x(b *testing.B)   { benchTextFrame600(b, 2, glyphCacheBucketed, false) }
func BenchmarkTextFrame600Cold1x(b *testing.B)   { benchTextFrame600(b, 1, glyphCacheBucketed, true) }
func BenchmarkTextFrame600Cold2x(b *testing.B)   { benchTextFrame600(b, 2, glyphCacheBucketed, true) }

func TestParseGlyphCacheMode(t *testing.T) {
	for in, want := range map[string]int32{"": glyphCacheBucketed, "1": glyphCacheBucketed, "0": glyphCacheOff,
		"off": glyphCacheOff, "FALSE": glyphCacheOff, "exact": glyphCacheExact} {
		if got := parseGlyphCacheMode(in); got != want {
			t.Errorf("parseGlyphCacheMode(%q) = %d, want %d", in, got, want)
		}
	}
}
