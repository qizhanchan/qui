package font

import (
	"bytes"
	"fmt"
	"os"
	"sync"
	"testing"

	"github.com/qizhanchan/qui/third_party/typesetting/font/opentype"
)

// qui patch: gvar decodes per glyph on first use. Concurrent first uses of
// the same glyphs must agree with a sequential decode (run with -race).
func TestLazyGvarConcurrentDecode(t *testing.T) {
	const path = "/System/Library/Fonts/SFNS.ttf" // a variable font with gvar
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Skip("no system variable font:", err)
	}
	load := func() *Font {
		ld, err := opentype.NewLoaders(bytes.NewReader(raw))
		if err != nil {
			t.Fatal(err)
		}
		f, err := NewFont(ld[0])
		if err != nil {
			t.Fatal(err)
		}
		return f
	}
	wght := []Variation{{Tag: opentype.MustNewTag("wght"), Value: 700}}
	seq := NewFace(load())
	seq.SetVariations(wght)
	want := make([]string, 400)
	for g := range want {
		want[g] = fmt.Sprint(seq.GlyphData(GID(g)))
	}
	shared := load()
	var wg sync.WaitGroup
	errs := make(chan string, 8)
	for w := 0; w < 8; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			face := NewFace(shared)
			face.SetVariations(wght)
			for g := range want {
				if got := fmt.Sprint(face.GlyphData(GID(g))); got != want[g] {
					errs <- fmt.Sprintf("glyph %d differs", g)
					return
				}
			}
		}()
	}
	wg.Wait()
	close(errs)
	for e := range errs {
		t.Fatal(e)
	}
}
