package htmlcss

import (
	"math"
	"testing"
	"time"

	"github.com/qizhanchan/qui"
)

// A state rule that changes what the paint swap cannot carry — a transform
// — restyles the element on the state boundary, as a browser does, and so
// runs its transition: react.dev's community cards scale and tilt on
// `.group:hover` (Tailwind's custom-property transform), and a button's own
// `:hover { transform }` lifts it.
func TestStateRuleTransformRestyles(t *testing.T) {
	cases := []struct {
		name, css string
		self      bool
	}{
		{"ancestor", `.card { transform: rotate(2deg); transition: transform 200ms linear }
			.group:hover .card { transform: rotate(-1deg) scale(1.1) }`, false},
		{"ancestor-vars", `.card { --tw-rotate: 2deg; --tw-scale-x: 1; --tw-scale-y: 1;
			transform: translate(0, 0) rotate(var(--tw-rotate)) scaleX(var(--tw-scale-x)) scaleY(var(--tw-scale-y));
			transition: transform 200ms linear }
			.group:hover .card { --tw-rotate: -1deg; --tw-scale-x: 1.1; --tw-scale-y: 1.1 }`, false},
		{"self", `.card { transform: rotate(2deg); transition: transform 200ms linear }
			.card:hover { transform: rotate(-1deg) scale(1.1) }`, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			win := qui.NewTestWindow(qui.Size{W: 400, H: 300})
			res := RenderDoc(`<body><div id=g class=group><div id=c class=card style="width:100px;height:80px;background:red"></div></div></body>`,
				`body { margin: 0 } .group { padding: 20px } `+c.css, Options{ViewportWidth: 400, DocumentRoot: true, Window: win})
			win.SetRoot(res.Root)
			res.Root.Layout(qui.Rect{W: 400, H: 300})
			card := res.ByID["c"].(*El)
			target := res.ByID["g"].(*El)
			if c.self {
				target = card
			}
			scale := func() float64 {
				m := card.Box.PaintTransform()
				return math.Hypot(float64(m.A), float64(m.B))
			}
			if s := scale(); math.Abs(s-1) > 0.01 {
				t.Fatalf("resting scale %.3f", s)
			}
			target.Handle(qui.NewMouseEvent(qui.EventMouseEnter, 30, 30, 0, 0))
			win.DrainJobsForTest()
			res.Engine.FlushPendingStyles()
			if card.anim == nil {
				t.Fatal("hover started no transition")
			}
			card.tickAnimations(time.Now().Add(100 * time.Millisecond))
			if s := scale(); s < 1.02 || s > 1.08 {
				t.Fatalf("scale halfway through the transition = %.3f, want ~1.05", s)
			}
			card.tickAnimations(time.Now().Add(400 * time.Millisecond))
			if s := scale(); math.Abs(s-1.1) > 0.01 {
				t.Fatalf("hovered scale %.3f, want 1.1", s)
			}
			target.Handle(qui.NewMouseEvent(qui.EventMouseLeave, 30, 30, 0, 0))
			win.DrainJobsForTest()
			res.Engine.FlushPendingStyles()
			card.tickAnimations(time.Now().Add(time.Second))
			if s := scale(); math.Abs(s-1) > 0.01 {
				t.Fatalf("scale after leaving %.3f, want 1", s)
			}
		})
	}
}
