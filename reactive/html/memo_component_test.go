package html_test

import (
	"testing"

	"github.com/qizhanchan/qui"
	"github.com/qizhanchan/qui/reactive"
	h "github.com/qizhanchan/qui/reactive/html"
)

func TestMemoComponentSkipsEqualProps(t *testing.T) {
	win := qui.NewTestWindow(qui.Size{W: 200, H: 100})
	childRenders := 0
	var bump func(int)
	rt := h.Mount(win, ``, func() h.Node {
		n, set := reactive.UseState(0)
		bump = set
		return h.Div(h.Span(string(rune('0'+n))),
			h.MemoComponent("Child", "", "same", func(a, b string) bool { return a == b }, func(string) h.Node {
				childRenders++
				return h.Span("child")
			}))
	})
	if childRenders != 1 {
		t.Fatalf("first render: %d", childRenders)
	}
	bump(1)
	rt.Flush()
	win.DrainJobsForTest()
	rt.Flush()
	if childRenders != 1 {
		t.Errorf("an equal-props child re-rendered (%d)", childRenders)
	}
}
