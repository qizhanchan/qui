package htmlcss

import "testing"

func TestNestedListUAMarginsAndAuthorOverride(t *testing.T) {
	SetWebCompat(true)
	defer SetWebCompat(false)
	res := RenderDoc(`<body><menu id="outer"><li><div><ul id="nested"><li><ol id="authored"></ol></li></ul></div></li></menu></body>`, `#authored{margin:7px 9px}`, Options{})
	outer := res.ByID["outer"].(*El).lastCS
	if outer.Margin.Top != outer.FontSize || outer.Margin.Bottom != outer.FontSize {
		t.Fatalf("outer list lost its UA margins: %v", outer.Margin)
	}
	nested := res.ByID["nested"].(*El).lastCS
	if nested.Margin.Top != 0 || nested.Margin.Bottom != 0 || nested.Padding.Left != 40 {
		t.Fatalf("nested list UA defaults: margin=%v padding=%v", nested.Margin, nested.Padding)
	}
	authored := res.ByID["authored"].(*El).lastCS.Margin
	if authored.Top != 7 || authored.Bottom != 7 || authored.Left != 9 || authored.Right != 9 {
		t.Fatalf("UA margins overrode authored margins: %v", authored)
	}
}
