package typeset

import (
	"fmt"
	"testing"

	"golang.org/x/image/font/gofont/goregular"

	"github.com/qizhanchan/qui/internal/gfx"
)

func TestTextShapeCacheMatchesUncached(t *testing.T) {
	invalidateShapeCaches()
	fonts := []gfx.Font{
		{Size: 14},
		{Size: 14, LetterSpacing: 2},
		{Size: 14, WordSpacing: 5},
		{Size: 14, SmallCaps: true},
		{Size: 14, Bold: true, Italic: true},
	}
	text := []rune("Shape me once, then hit")
	seen := map[float32]string{}
	for _, f := range fonts {
		for _, dir := range []TextDirection{TextDirectionAuto, TextDirectionRTL} {
			got, ok := ShapeSingleLine(text, f, dir)
			again, _ := ShapeSingleLine(text, f, dir)
			want, wok := shapeSingleLineUncached(append([]rune(nil), text...), f, dir)
			if ok != wok || got.Advance != want.Advance || len(got.Runs) != len(want.Runs) {
				t.Fatalf("%+v dir %d: cached (%v, %.2f, %d runs) != uncached (%v, %.2f, %d runs)",
					f, dir, ok, got.Advance, len(got.Runs), wok, want.Advance, len(want.Runs))
			}
			if got != again {
				t.Errorf("%+v: second call missed the cache", f)
			}
			if dir == TextDirectionAuto {
				adv, _ := paragraphAdvance(text, f)
				p, _ := ShapeParagraph(text, f, TextDirectionAuto)
				if adv != p.Advance {
					t.Errorf("%+v: memoised advance %.2f != %.2f", f, adv, p.Advance)
				}
				if prev, dup := seen[got.Advance]; dup && !f.Bold {
					t.Errorf("%+v: advance %.2f equals %s's — key collision?", f, got.Advance, prev)
				}
				seen[got.Advance] = fmt.Sprintf("%+v", f)
			}
		}
	}
}

// The cache must own its runes: a caller reusing its buffer (an editor)
// cannot rewrite a cached line.
func TestTextShapeCacheOwnsRunes(t *testing.T) {
	invalidateShapeCaches()
	buf := []rune("abc")
	line, _ := ShapeSingleLine(buf, gfx.Font{Size: 14}, TextDirectionAuto)
	buf[0] = 'X'
	if string(line.Text) != "abc" {
		t.Fatalf("cached line text aliased the caller's slice: %q", string(line.Text))
	}
	if again, _ := ShapeSingleLine([]rune("abc"), gfx.Font{Size: 14}, TextDirectionAuto); again != line {
		t.Fatal("expected a cache hit")
	}
}

func TestTextShapeCacheInvalidation(t *testing.T) {
	invalidateShapeCaches()
	text := []rune("invalidate")
	a, _ := ShapeSingleLine(text, gfx.Font{Size: 13}, TextDirectionAuto)
	SetEmojiProvider(CurrentEmojiProvider())
	if b, _ := ShapeSingleLine(text, gfx.Font{Size: 13}, TextDirectionAuto); b == a {
		t.Error("SetEmojiProvider did not invalidate shaped lines")
	}
	if err := LoadFontVariantFromBytes("qui-shapecache-test", gfx.FontWeightNormal, false, goregular.TTF); err != nil {
		t.Fatal(err)
	}
	if n := ShapedLineCache.Len(); n != 0 {
		t.Errorf("font registration left %d shaped lines cached", n)
	}
}

func TestTextShapeCacheBounded(t *testing.T) {
	c := newLRUCache[int, int](8, 1<<20)
	for i := 0; i < 100; i++ {
		c.Put(i, i, 10)
	}
	if c.Len() != 8 {
		t.Fatalf("entries = %d, want 8", c.Len())
	}
	if _, ok := c.Get(99); !ok {
		t.Fatal("most recent entry evicted")
	}
	b := newLRUCache[int, int](1000, 100)
	for i := 0; i < 100; i++ {
		b.Put(i, i, 30)
	}
	if b.Len() != 3 || b.Bytes > 100 {
		t.Fatalf("byte bound: %d entries / %d bytes", b.Len(), b.Bytes)
	}
}
