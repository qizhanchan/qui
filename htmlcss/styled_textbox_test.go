package htmlcss

import (
	"testing"

	"github.com/qizhanchan/qui"
	"github.com/qizhanchan/qui/widgets"
)

// A text element that also carries box decorations (border / explicit or
// min size) must build as a Box wrapping the text — not collapse into a
// bare Label, which paints no border and ignores width. Regression for
// the compact-toolbar "10" box that lost its width and split its digits.
func TestStyledTextDivBecomesBox(t *testing.T) {
	root := Render(
		`<body><div class="chip">10</div></body>`,
		`.chip { width: 30px; min-width: 30px; height: 22px; border-width: 1px; border-color: #ccc; }`,
		Options{},
	)

	// content-box (default): outer size = declared + padding + border, so the
	// 30x22 chip with a 1px border becomes a 32x24 outer box, min-width 32.
	var chip *widgets.Box
	var walk func(w qui.Widget)
	walk = func(w qui.Widget) {
		if bx, ok := w.(*El); ok && bx.Style().MinWidth == 32 {
			chip = &bx.Box
		}
		if c, ok := w.(interface{ ChildList() []qui.Widget }); ok {
			for _, ch := range c.ChildList() {
				walk(ch)
			}
		}
	}
	walk(root)

	if chip == nil {
		t.Fatal("styled text div did not build as a *widgets.Box with the CSS box model")
	}
	if chip.Style().Width != 32 || chip.Style().Height != 24 {
		t.Errorf("chip size = %vx%v, want 32x24 (content-box: 30x22 + 1px border)", chip.Style().Width, chip.Style().Height)
	}
	if chip.Style().BorderSize != 1 {
		t.Errorf("chip border = %v, want 1", chip.Style().BorderSize)
	}
	// It should still contain the text as a Label child.
	kids := chip.ChildList()
	if len(kids) != 1 {
		t.Fatalf("chip children = %d, want 1 (text label)", len(kids))
	}
	if lbl, ok := kids[0].(*widgets.Label); !ok || lbl.Text() != "10" {
		t.Errorf("chip child = %T %q, want Label \"10\"", kids[0], "10")
	}

	// And the min-width must protect it from flex shrink in a tight row.
	if got := widgets.NewBox(qui.FlexLayout{}, chip); got == nil {
		t.Fatal("unreachable")
	}
}
