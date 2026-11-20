package htmlcss

import (
	"testing"

	"github.com/qizhanchan/qui"
)

// ch is the advance of "0" in the element's own font; ex its x-height.
func TestFontRelativeUnitsUseTheFace(t *testing.T) {
	f := qui.Font{Size: 40, Weight: qui.FontWeightNormal}
	zero := qui.RuneAdvance('0', f)
	if zero <= 0 {
		t.Skip("no font")
	}
	res := RenderDoc(`<div id=a style="font-size:40px;max-width:10ch">x</div><div id=b style="font-size:20px;width:4ex;height:2ch">y</div>`, "", Options{ViewportWidth: 800})
	a := res.ByID["a"].(*El).ComputedStyle()
	if want := 10 * zero; a.MaxWidth < want-0.5 || a.MaxWidth > want+0.5 {
		t.Errorf("max-width: 10ch = %v, want %v (10 × the advance of 0)", a.MaxWidth, want)
	}
	b := res.ByID["b"].(*El).ComputedStyle()
	half := qui.RuneAdvance('0', qui.Font{Size: 20, Weight: qui.FontWeightNormal})
	if b.Height < 2*half-0.5 || b.Height > 2*half+0.5 {
		t.Errorf("height: 2ch = %v, want %v", b.Height, 2*half)
	}
	if b.Width <= 4 || b.Width >= 4*20 {
		t.Errorf("width: 4ex = %v, want a fraction of 4em", b.Width)
	}
}

// The font shorthand: a bare number is the weight, the size needs a unit,
// and the slash may be spaced (GitHub's Primer title tokens).
func TestFontShorthandWeightAndSpacedSlash(t *testing.T) {
	for v, want := range map[string][3]string{
		`600 1rem / 1.5 "Mona Sans VF", -apple-system, sans-serif`: {"600", "1rem", "1.5"},
		`600 1rem/1.5 Arial`:                   {"600", "1rem", "1.5"},
		`italic bold 12px/30px Georgia, serif`: {"bold", "12px", "30px"},
		`700 calc(1rem + 2px) / 1.2 system-ui`: {"700", "calc(1rem + 2px)", "1.2"},
		`small serif`:                          {"normal", "small", "normal"},
		`0 Arial`:                              {"normal", "0", "normal"},
	} {
		m := map[string]string{}
		expandFont(v, m)
		if got := [3]string{m["font-weight"], m["font-size"], m["line-height"]}; got != want {
			t.Errorf("font: %s → weight/size/line-height %q, want %q", v, got, want)
		}
	}
}
