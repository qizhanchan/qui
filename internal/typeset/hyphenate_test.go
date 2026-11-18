package typeset

import (
	"github.com/qizhanchan/qui/internal/gfx"
	"runtime"
	"strings"
	"testing"
)

func TestAutomaticHyphenationSplitsOnlyAtLanguageBoundaries(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("platform dictionary")
	}
	points := platformHyphenPoints("documentation", "en")
	if len(points) == 0 {
		t.Skip("platform dictionary unavailable")
	}
	f := gfx.Font{Size: 16}
	it := InlineItem{Text: "documentation", Font: f, Hyphenate: true, Language: "en"}
	l := BuildInlineLayout([]InlineItem{it}, InlineLayoutOptions{MaxWidth: 75, Wrap: true, BaseFont: f})
	if len(l.Lines) < 2 || len(l.Lines[0].Frags) == 0 || !strings.HasSuffix(l.Lines[0].Frags[0].Text, "-") {
		t.Fatalf("expected language hyphenation: %+v", l.Lines)
	}
	plain := strings.ReplaceAll(flatFragText(l), "-", "")
	if plain != "documentation" {
		t.Fatalf("hyphenation lost text: %q", plain)
	}
	// A nested nowrap span still takes precedence over inherited hyphens:auto.
	it.NoWrapGroup = 1
	nowrap := BuildInlineLayout([]InlineItem{inlineText("lead ", f), it}, InlineLayoutOptions{MaxWidth: 75, Wrap: true, BaseFont: f})
	if len(nowrap.Lines) != 2 || strings.Contains(flatFragText(nowrap), "-") {
		t.Fatalf("nowrap group was hyphenated: %+v", nowrap.Lines)
	}
}
