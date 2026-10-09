package qui

import (
	"fmt"
	"testing"

	"golang.org/x/image/font/gofont/goregular"
)

func TestTextShapeCacheMatchesUncached(t *testing.T) {
	invalidateShapeCaches()
	fonts := []Font{
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
			got, ok := shapeSingleLine(text, f, dir)
			again, _ := shapeSingleLine(text, f, dir)
			want, wok := shapeSingleLineUncached(append([]rune(nil), text...), f, dir)
			if ok != wok || got.advance != want.advance || len(got.runs) != len(want.runs) {
				t.Fatalf("%+v dir %d: cached (%v, %.2f, %d runs) != uncached (%v, %.2f, %d runs)",
					f, dir, ok, got.advance, len(got.runs), wok, want.advance, len(want.runs))
			}
			if got != again {
				t.Errorf("%+v: second call missed the cache", f)
			}
			if dir == TextDirectionAuto {
				adv, _ := paragraphAdvance(text, f)
				p, _ := shapeParagraph(text, f, TextDirectionAuto)
				if adv != p.advance {
					t.Errorf("%+v: memoised advance %.2f != %.2f", f, adv, p.advance)
				}
				if prev, dup := seen[got.advance]; dup && !f.Bold {
					t.Errorf("%+v: advance %.2f equals %s's — key collision?", f, got.advance, prev)
				}
				seen[got.advance] = fmt.Sprintf("%+v", f)
			}
		}
	}
}

// The cache must own its runes: a caller reusing its buffer (an editor)
// cannot rewrite a cached line.
func TestTextShapeCacheOwnsRunes(t *testing.T) {
	invalidateShapeCaches()
	buf := []rune("abc")
	line, _ := shapeSingleLine(buf, Font{Size: 14}, TextDirectionAuto)
	buf[0] = 'X'
	if string(line.text) != "abc" {
		t.Fatalf("cached line text aliased the caller's slice: %q", string(line.text))
	}
	if again, _ := shapeSingleLine([]rune("abc"), Font{Size: 14}, TextDirectionAuto); again != line {
		t.Fatal("expected a cache hit")
	}
}

func TestTextShapeCacheInvalidation(t *testing.T) {
	invalidateShapeCaches()
	text := []rune("invalidate")
	a, _ := shapeSingleLine(text, Font{Size: 13}, TextDirectionAuto)
	SetEmojiProvider(currentEmojiProvider())
	if b, _ := shapeSingleLine(text, Font{Size: 13}, TextDirectionAuto); b == a {
		t.Error("SetEmojiProvider did not invalidate shaped lines")
	}
	if err := LoadFontVariantFromBytes("qui-shapecache-test", FontWeightNormal, false, goregular.TTF); err != nil {
		t.Fatal(err)
	}
	if n := shapedLineCache.len(); n != 0 {
		t.Errorf("font registration left %d shaped lines cached", n)
	}
}

func TestTextShapeCacheBounded(t *testing.T) {
	c := newLRUCache[int, int](8, 1<<20)
	for i := 0; i < 100; i++ {
		c.put(i, i, 10)
	}
	if c.len() != 8 {
		t.Fatalf("entries = %d, want 8", c.len())
	}
	if _, ok := c.get(99); !ok {
		t.Fatal("most recent entry evicted")
	}
	b := newLRUCache[int, int](1000, 100)
	for i := 0; i < 100; i++ {
		b.put(i, i, 30)
	}
	if b.len() != 3 || b.bytes > 100 {
		t.Fatalf("byte bound: %d entries / %d bytes", b.len(), b.bytes)
	}
}
