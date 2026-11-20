package typeset

import (
	"strings"
	"testing"
	"time"

	"github.com/qizhanchan/qui/internal/gfx"
)

// A nowrap, ellipsized label given a huge single-line message (gopls's
// "inconsistent vendoring" error, ~30 KB) must stay cheap: the cut used to
// be found by reshaping the whole prefix once per grapheme, O(n²).
func TestEllipsisOnHugeLineIsFast(t *testing.T) {
	spec := gfx.Font{Size: 13}
	line := strings.Repeat("github.com/some/module@v1.2.3: is explicitly required in go.mod, but not marked as explicit in vendor/modules.txt; ", 300)
	start := time.Now()
	l := BuildTextLayout(line, spec, TextLayoutOptions{MaxWidth: 420, Ellipsis: true})
	if d := time.Since(start); d > 10*time.Second { // the O(n²) search took minutes
		t.Fatalf("ellipsizing a %d-byte line took %v", len(line), d)
	}
	if len(l.Lines) != 1 || !strings.HasSuffix(l.Lines[0].Text, "…") || l.Lines[0].Width > 420 {
		t.Fatalf("got %d lines, first %q (width %v)", len(l.Lines), l.Lines[0].Text, l.Lines[0].Width)
	}
}

// The search must find the same cut as checking every prefix.
func TestEllipsisCutIsLongestFit(t *testing.T) {
	spec := gfx.Font{Size: 13}
	for _, line := range []string{
		"The quick brown fox jumps over the lazy dog, again and again",
		"中文和 English 混排的一行比较长的文字，用来测试省略号截断位置",
		"x",
	} {
		for _, w := range []float32{8, 30, 61, 120, 200} {
			got, _, _ := ellipsizeTextLine(line, w, spec, TextDirectionLTR, nil)
			runes := []rune(line)
			b := GraphemeBoundaries(runes)
			want := "…"
			for k := len(b) - 1; k >= 1; k-- {
				c := string(runes[:b[k]]) + "…"
				if s, ok := ShapeSingleLine([]rune(c), spec, TextDirectionLTR); ok &&
					s.Advance <= w-textVisualOverhang(spec) {
					want = c
					break
				}
			}
			if got != want {
				t.Errorf("%q at %v: got %q, want %q", line, w, got, want)
			}
		}
	}
}
