package typeset

import (
	"log"
	"os"
	"strings"
	"sync"
	"unicode"

	"github.com/qizhanchan/qui/third_party/typesetting/shaping"
	"golang.org/x/image/font/opentype"
	"golang.org/x/image/font/sfnt"
)

// QUI_DEBUG_TOFU=1 logs, once per character, every rune that shaped to
// .notdef (glyph 0) — a box on screen — with the fonts the chain held.
// The way to find the next missing font, rather than wait for a user to
// spot the box.
var tofuDebug = os.Getenv("QUI_DEBUG_TOFU") == "1"

var tofuSeen sync.Map // rune → struct{}

// noteTofu collects the runes of runs that drew .notdef. It runs under the
// shaping lock, so the report (which asks the emoji and font registry)
// happens on its own goroutine.
func noteTofu(runs []shaping.Output, text []rune) {
	var missing []rune
	for _, run := range runs {
		for _, g := range run.Glyphs {
			if g.GlyphID != 0 || g.ClusterIndex < 0 || g.ClusterIndex >= len(text) {
				continue
			}
			r := text[g.ClusterIndex]
			if unicode.IsSpace(r) || r == 0x200B {
				continue
			}
			if _, seen := tofuSeen.LoadOrStore(r, struct{}{}); !seen {
				missing = append(missing, r)
			}
		}
	}
	if len(missing) > 0 {
		go reportTofu(missing)
	}
}

func reportTofu(missing []rune) {
	FontCacheMu.RLock()
	var names []string
	for _, f := range append(append([]*opentype.Font{FontParsed}, FontFallbacks...), AutoFallbacks...) {
		if f == nil {
			continue
		}
		if n, ok := systemTypefaceNames.Load(f); ok {
			names = append(names, n.(string))
		} else if n, err := f.Name(&sfnt.Buffer{}, sfnt.NameIDPostScript); err == nil {
			names = append(names, n)
		}
	}
	FontCacheMu.RUnlock()
	for _, r := range missing {
		if IsEmojiRune(r) {
			continue // the EmojiProvider draws it
		}
		log.Printf("qui: tofu: no font draws %q (%U); chain: %s", r, r, strings.Join(names, ", "))
	}
}
