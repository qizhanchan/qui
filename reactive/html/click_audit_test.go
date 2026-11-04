package html_test

import (
	"strings"
	"testing"

	"github.com/qizhanchan/qui"
	"github.com/qizhanchan/qui/htmlcss"
	h "github.com/qizhanchan/qui/reactive/html"
)

// The audit names both ends of a nested click by the line that installed
// each handler, and is quiet once the inner one stops the click.
func TestAuditClicksReportsNesting(t *testing.T) {
	var got []htmlcss.ClickNesting
	h.AuditClicks(func(n htmlcss.ClickNesting) { got = append(got, n) })
	defer h.AuditClicks(nil)

	stop := false
	win := qui.NewTestWindow(qui.Size{W: 300, H: 100})
	rt := h.Mount(win, ``, func() h.Node {
		close := h.Button("x").OnClick(func() {})
		if stop {
			close = close.StopClickPropagation()
		}
		return h.Div(h.Span("Tab"), close).OnClick(func() {})
	})
	rt.Flush()
	if len(got) != 1 || !strings.Contains(got[0].Inner, "click_audit_test.go:") ||
		!strings.Contains(got[0].Outer, "click_audit_test.go:") {
		t.Fatalf("audit = %v, want the button inside the div, by source line", got)
	}

	got = nil
	h.AuditClicks(func(n htmlcss.ClickNesting) { got = append(got, n) })
	stop = true
	rt.RequestRender()
	rt.Flush()
	if len(got) != 0 {
		t.Fatalf("audit after StopClickPropagation = %v, want none", got)
	}
}
