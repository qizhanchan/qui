package html_test

import (
	"strings"
	"testing"

	"github.com/qizhanchan/qui"
	h "github.com/qizhanchan/qui/reactive/html"
)

// A dialog with a CSS width lays its wrapped text out at that width, and
// must be measured there too: measuring the content across the whole
// window (one long line) and only then narrowing the box clipped every
// line past the first few.
func TestDialogWrappedTextMeasuredAtDialogWidth(t *testing.T) {
	win := qui.NewTestWindow(qui.Size{W: 1280, H: 800})
	css := `.narrow { width: 300px; min-width: 0; }
.msg { font-size: 12px; line-height: 18px; white-space: pre-wrap; }`
	msg := "first line\n\n" + strings.Repeat("a long sentence that wraps several times in a narrow box ", 4)
	h.Mount(win, css, func() h.Node {
		return h.Div(h.Dialog(h.DialogProps{
			Title:     "T",
			Body:      h.Div(msg).Class("msg"),
			OnDismiss: func() {},
			Class:     "narrow",
		}))
	})
	host := win.Overlays()[0]
	host.Layout(qui.Rect{W: 1280, H: 800})
	box, el := findClass(host, "q-dialog"), findClass(host, "msg")
	if got := box.Bounds().W; got != 302 { // 300 + the shell's 1px border
		t.Fatalf("dialog width = %v, want 302", got)
	}
	need := el.Measure(qui.Size{W: el.Bounds().W}).H
	if need <= 3*18 {
		t.Fatalf("test text should wrap to more than 3 lines, measures %v", need)
	}
	if got := el.Bounds().H; got < need {
		t.Errorf("message box height %v < its wrapped content %v: the dialog was measured at another width", got, need)
	}
}
