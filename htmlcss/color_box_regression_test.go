package htmlcss

import (
	"testing"
)

func TestColorParsingRegressions(t *testing.T) {
	if _, ok := parseHexColor("#zzz"); ok {
		t.Error("#zzz must be rejected")
	}
	if c, ok := parseHexColor("#fff"); !ok || c.R != 1 {
		t.Errorf("#fff = %v, %v", c, ok)
	}
	if c, ok := parseRGBColor("rgb(0 0 0 / 50%)"); !ok || c.A < 0.49 || c.A > 0.51 {
		t.Errorf("rgb(0 0 0 / 50%%) alpha = %v, want 0.5", c.A)
	}
	if c, ok := parseRGBColor("rgba(0,0,0,5)"); !ok || c.A != 1 {
		t.Errorf("rgba alpha 5 = %v, want clamped to 1", c.A)
	}
}

func TestPaddingShorthandWithCalc(t *testing.T) {
	ins := parseBox(map[string]string{"padding": "calc(2px + 4px) 8px"}, "padding", 16)
	if ins.Top != 6 || ins.Bottom != 6 || ins.Left != 8 || ins.Right != 8 {
		t.Errorf("padding = %+v, want 6/8/6/8", ins)
	}
}

func TestAtRulesDoNotSwallowFollowingRule(t *testing.T) {
	for _, src := range []string{
		`@charset "utf-8"; .btn{color:red}`,
		`@import url(x.css); .btn{color:red}`,
		`@keyframes s{from{opacity:0}to{opacity:1}} .btn{color:red}`,
		`@supports (display:grid){.x{color:blue}} .btn{color:red}`,
	} {
		sheet := ParseCSS(src)
		if len(sheet.Rules) != 1 {
			t.Errorf("%q: %d rules, want just .btn", src, len(sheet.Rules))
			continue
		}
		if d := sheet.Rules[0].Declarations; len(d) != 1 || d[0].Property != "color" || d[0].Value != "red" {
			got := d
			t.Errorf("%q: surviving rule %+v, want color:red", src, got)
		}
	}
}
