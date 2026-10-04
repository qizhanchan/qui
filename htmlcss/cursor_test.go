package htmlcss

import (
	"testing"

	"github.com/qizhanchan/qui"
)

// declaredCursorOf reads back what applyCursor pushed onto the widget.
func declaredCursorOf(t *testing.T, w qui.Widget) (qui.CursorShape, bool) {
	t.Helper()
	dc, ok := w.(interface {
		DeclaredCursor() (qui.CursorShape, bool)
	})
	if !ok {
		t.Fatalf("widget %T does not expose DeclaredCursor", w)
	}
	return dc.DeclaredCursor()
}

func TestParseCursorKeywords(t *testing.T) {
	cases := []struct {
		css      string
		shape    qui.CursorShape
		declared bool
		reset    bool
	}{
		{"pointer", qui.CursorHand, true, false},
		{"POINTER", qui.CursorHand, true, false},
		{"text", qui.CursorText, true, false},
		{"vertical-text", qui.CursorText, true, false},
		{"crosshair", qui.CursorCrosshair, true, false},
		{"cell", qui.CursorCrosshair, true, false},
		{"col-resize", qui.CursorResizeEW, true, false},
		{"ns-resize", qui.CursorResizeNS, true, false},
		// Recognized but with no native shape: declared (so it suppresses a
		// nested I-beam) yet degraded to the plain arrow.
		{"not-allowed", qui.CursorDefault, true, false},
		{"move", qui.CursorDefault, true, false},
		{"grabbing", qui.CursorDefault, true, false},
		{"default", qui.CursorDefault, true, false},
		// auto hands the decision back to the widget's built-in shape.
		{"auto", qui.CursorDefault, false, true},
		// Unknown → invalid declaration → inherited value stands.
		{"bogus-shape", qui.CursorDefault, false, false},
		// url() falls through to the keyword fallback in the list.
		{"url(hand.png), pointer", qui.CursorHand, true, false},
	}
	for _, c := range cases {
		shape, declared, reset := parseCursor(c.css)
		if shape != c.shape || declared != c.declared || reset != c.reset {
			t.Errorf("parseCursor(%q) = (%v, %v, %v), want (%v, %v, %v)",
				c.css, shape, declared, reset, c.shape, c.declared, c.reset)
		}
	}
}

// The declared shape must land on the element's widget, and it must inherit
// — that is what makes `cursor: pointer` on a card cover the label inside.
func TestCursorAppliesAndInherits(t *testing.T) {
	res := RenderDoc(
		`<body><div id="card">Click me<span id="inner">now</span></div>
		 <div id="plain">no cursor here</div></body>`,
		`#card { cursor: pointer; width: 120px; height: 40px; }`,
		Options{},
	)

	shape, declared := declaredCursorOf(t, res.ByID["card"])
	if !declared || shape != qui.CursorHand {
		t.Errorf("#card declared=(%v,%v), want (CursorHand,true)", shape, declared)
	}
	if inner, ok := res.ByID["inner"]; ok {
		shape, declared := declaredCursorOf(t, inner)
		if !declared || shape != qui.CursorHand {
			t.Errorf("#inner should inherit pointer, got (%v,%v)", shape, declared)
		}
	}
	if _, declared := declaredCursorOf(t, res.ByID["plain"]); declared {
		t.Error("#plain declared a cursor without a `cursor` rule")
	}
}

// `cursor: auto` on a child must NOT inherit the ancestor's pointer.
func TestCursorAutoResetsInheritance(t *testing.T) {
	res := RenderDoc(
		`<body><div id="card"><div id="field">text</div></div></body>`,
		`#card { cursor: pointer; } #field { cursor: auto; }`,
		Options{},
	)
	if _, declared := declaredCursorOf(t, res.ByID["field"]); declared {
		t.Error("`cursor: auto` should drop the inherited declaration")
	}
}

// Restyling to a state without `cursor` has to clear the old declaration,
// otherwise a stale shape sticks to the element for its lifetime.
func TestCursorClearedOnRestyle(t *testing.T) {
	res := RenderDoc(
		`<body><div id="card" class="on">hi</div></body>`,
		`.on { cursor: pointer; } .off { color: #333; }`,
		Options{},
	)
	card := res.ByID["card"].(*El)
	if shape, declared := declaredCursorOf(t, card); !declared || shape != qui.CursorHand {
		t.Fatalf("initial declared=(%v,%v), want (CursorHand,true)", shape, declared)
	}
	card.SetClass("off")
	res.Engine.Restyle()
	if _, declared := declaredCursorOf(t, card); declared {
		t.Error("cursor declaration survived a restyle that no longer sets it")
	}
}

// The UA sheet gives a real link a hand, but a bare <a> (no href) is not a
// link and must not claim one.
func TestAnchorUACursor(t *testing.T) {
	res := RenderDoc(
		`<body><a id="link" href="https://example.com">go</a>
		 <a id="bare">not a link</a></body>`,
		``,
		Options{},
	)
	if link, ok := res.ByID["link"]; ok {
		if shape, declared := declaredCursorOf(t, link); !declared || shape != qui.CursorHand {
			t.Errorf("<a href> declared=(%v,%v), want (CursorHand,true)", shape, declared)
		}
	}
	if bare, ok := res.ByID["bare"]; ok {
		if _, declared := declaredCursorOf(t, bare); declared {
			t.Error("<a> without href should not claim the link cursor")
		}
	}
}
