//go:build darwin && cgo

package typeset

import (
	"github.com/qizhanchan/qui/internal/gfx"
	"math"
	"runtime"
	"testing"
)

func TestBrowserSystemFontFractionalAdvancesAndInstances(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("macOS platform font")
	}
	was := SystemFontsEnabled()
	EnableSystemFonts(true)
	defer EnableSystemFonts(was)
	if !SystemFontFamilyInstalled("System Font") {
		t.Skip("system font unavailable")
	}
	web := webFontMetrics.Load()
	SetWebFontMetrics(true)
	defer SetWebFontMetrics(web)
	f := gfx.Font{Family: "System Font", Size: 16}
	text := "Return a “memory view” object created from the given argument. See Memory Views for more information."
	width, _ := TextMetrics(text, f)
	if math.Abs(float64(width-770.015625)) > 0.05 {
		t.Fatalf("Chrome fractional run width: %.5f, want 770.015625", width)
	}
	italic := f
	italic.Italic = true
	italicWidth := MeasureRunes([]rune("Changed in version 3.10: "), italic, nil)
	if math.Abs(float64(italicWidth-182.3984375)) > 0.05 {
		t.Fatalf("Chrome italic run width: %.5f, want 182.3984375", italicWidth)
	}
	normal, _ := TextMetrics("The Python interpreter has a number of functions", f)
	altered := f
	altered.Variations = map[string]float32{"opsz": 28}
	display, _ := TextMetrics("The Python interpreter has a number of functions", altered)
	if normal-display < 20 {
		t.Fatalf("optical instances shared metrics: normal %.2f display %.2f", normal, display)
	}
	again, _ := TextMetrics("The Python interpreter has a number of functions", f)
	if again != normal {
		t.Fatal("another variation changed cached normal instance")
	}
}
