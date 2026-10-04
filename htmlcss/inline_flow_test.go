package htmlcss

import (
	"strings"
	"testing"

	"github.com/qizhanchan/qui"
	"github.com/qizhanchan/qui/widgets"
)

func findInlineBox(root qui.Widget) *widgets.InlineBox {
	var found *widgets.InlineBox
	walkWidgets(root, func(w qui.Widget) {
		if ib, ok := w.(*widgets.InlineBox); ok && found == nil {
			found = ib
		}
	})
	return found
}

// A paragraph that mixes text with an inline-block badge must become a real
// inline formatting context (InlineBox), not a Label + separate block.
func TestMixedTextAndInlineBlockBuildsInlineBox(t *testing.T) {
	root := Render(
		`<p>Status: <span class="b">OK</span> now.</p>`,
		`.b { display: inline-block; padding: 2px 6px; background: #e6f4ea }`,
		Options{},
	)
	ib := findInlineBox(root)
	if ib == nil {
		t.Fatal("expected an InlineBox for text mixed with an inline-block element")
	}
	if len(ib.ChildList()) != 1 {
		t.Errorf("InlineBox should own the 1 inline-block child, got %d", len(ib.ChildList()))
	}
}

// A run that is purely text-like inline folds into ONE inline formatting
// context (a text-only InlineBox — selectable, link-aware) rather than
// materializing the inline elements as separate widgets.
func TestPureTextRunFoldsIntoOneFlow(t *testing.T) {
	root := Render(`<p>plain <strong>bold</strong> text</p>`, ``, Options{})
	ib := findInlineBox(root)
	if ib == nil {
		t.Fatal("expected the mixed text run to fold into an InlineBox")
	}
	if n := len(ib.ChildList()); n != 0 {
		t.Errorf("pure text run must carry no atomic boxes, got %d", n)
	}
	if got := ib.Text(); !strings.Contains(got, "plain") || !strings.Contains(got, "bold") {
		t.Errorf("folded text = %q, want plain+bold+text", got)
	}
}

// A lone inline-block box (the single-icon <div><img></div> case) collapses
// to the box directly — no InlineBox wrapper — matching pre-IFC behavior.
func TestLoneInlineBlockNotWrapped(t *testing.T) {
	root := Render(
		`<div class="ib"><span class="dot">x</span></div>`,
		`.ib { padding: 4px } .dot { display: inline-block; width: 10px; height: 10px }`,
		Options{},
	)
	if findInlineBox(root) != nil {
		t.Error("a lone inline-block should not be wrapped in an InlineBox")
	}
}

// The inline-block child stays addressable by id even when it lands inside
// an InlineBox (buildElement still registers it).
func TestInlineBlockChildKeepsID(t *testing.T) {
	res := RenderDoc(
		`<p>see <b id="badge" style="display:inline-block">NEW</b> label</p>`,
		``,
		Options{},
	)
	if res.ByID["badge"] == nil {
		t.Error("inline-block element inside an InlineBox should keep its id lookup")
	}
}
