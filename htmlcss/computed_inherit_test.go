package htmlcss

import "testing"

func TestExplicitInheritedControlStyle(t *testing.T) {
	res := RenderDoc(`<div style="font-size:150%;line-height:2.2;color:red"><select id="control" style="font-size:inherit;line-height:inherit;color:inherit"><option>x</option></select></div>`, "", Options{})
	cs := res.ByID["control"].(*El).ComputedStyle()
	if cs.FontSize != 24 || cs.LineHeight != 2.2 || !cs.LineHeightOwn || !cs.ColorOwn || cs.Color.R != 1 {
		t.Fatalf("inherited control style: size=%v line=%v own=%v color=%+v own=%v", cs.FontSize, cs.LineHeight, cs.LineHeightOwn, cs.Color, cs.ColorOwn)
	}
}
