package html_test

import (
	"strings"
	"testing"

	"github.com/qizhanchan/qui"
	"github.com/qizhanchan/qui/reactive"
	h "github.com/qizhanchan/qui/reactive/html"
)

// An inline <span> folded into its parent's text run must show its new text
// when a structural rule (:first-child, sibling combinators) concerns it.
// That case restyles from the parent; a parent whose own style is unchanged
// kept its old inline run, so a real window showed the first text forever.
// The headless window defers style flushes to the frame like a real one
// (NewTestWindow flushes every mutation inline).
func TestFoldedSpanTextUpdatesUnderStructuralRule(t *testing.T) {
	for _, tc := range []struct {
		name string
		css  string
	}{
		{"structural", `.badge span:first-child { font-weight: bold; }`},
		{"plain", `.badge span { font-weight: bold; }`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			win, err := qui.NewHeadlessWindowForTest(400, 300)
			if err != nil {
				t.Fatal(err)
			}
			var setQ func(string)
			rt := h.Mount(win, tc.css, func() h.Node {
				q, set := reactive.UseState("g")
				setQ = set
				return h.Div(h.Div(h.Span(q)).Class("badge").ID("badge"))
			})
			rt.Render()
			badge := func() string {
				win.Step()
				s, err := win.SnapshotText(qui.SnapshotOptions{})
				if err != nil {
					t.Fatal(err)
				}
				for _, l := range strings.Split(s, "\n") {
					if strings.Contains(l, "#badge") {
						return l
					}
				}
				return s
			}
			if l := badge(); !strings.Contains(l, `"g"`) {
				t.Fatalf("initial badge: %s", l)
			}
			for _, q := range []string{"ga", "gam"} {
				setQ(q)
				rt.Render()
				if l := badge(); !strings.Contains(l, `"`+q+`"`) {
					t.Fatalf("after %q the badge shows %s", q, l)
				}
			}
		})
	}
}
