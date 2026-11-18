package typeset

import (
	"runtime"
	"testing"

	"github.com/qizhanchan/qui/internal/gfx"
	"golang.org/x/image/font"
	"golang.org/x/image/math/fixed"
)

func TestBrowserLegacyMacFontMetrics(t *testing.T) {
	original := webFontMetrics.Load()
	SetWebFontMetrics(true)
	defer SetWebFontMetrics(original)
	m := font.Metrics{Ascent: fixed.I(11), Descent: fixed.I(3), Height: fixed.I(14)}
	got := browserFontMetrics(gfx.Font{Family: "Helvetica", Size: 14}, m)
	extra := fixed.Int26_6(0)
	if runtime.GOOS == "darwin" {
		extra = fixed.I(2)
	}
	if got.Ascent != m.Ascent+extra || got.Height != m.Height+extra || got.Descent != m.Descent {
		t.Fatalf("browser metrics=%+v, base=%+v", got, m)
	}
	if got := browserFontMetrics(gfx.Font{Family: "GoRegular", Size: 14}, m); got != m {
		t.Fatal("nonlegacy font adjusted")
	}
	SetWebFontMetrics(false)
	if got := browserFontMetrics(gfx.Font{Family: "Helvetica", Size: 14}, m); got != m {
		t.Fatal("native metrics adjusted")
	}
}
