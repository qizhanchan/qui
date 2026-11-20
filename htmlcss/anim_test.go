package htmlcss

import (
	"math"
	"strings"
	"testing"
	"time"

	"github.com/qizhanchan/qui"
	"github.com/qizhanchan/qui/widgets"
)

func TestTransitionOpacityAndEvents(t *testing.T) {
	_, res := renderWeb(t, `<!doctype html><html><head><style>
#b { opacity: 1; transition: opacity 100ms linear, width 50ms; width: 10px; height: 10px; }
#b.off { opacity: 0.2; width: 20px; }
</style></head><body><div id=b></div></body></html>`)
	b := res.ByID["b"].(*El)
	var events []string
	res.Engine.SetAnimationEventHandler(func(el *El, typ, name string, elapsed float64) {
		events = append(events, typ+":"+name)
	})
	b.SetClass("off")
	res.Engine.FlushPendingStyles()
	if b.anim == nil || b.anim.trans["opacity"] == nil {
		t.Fatal("no opacity transition started")
	}
	start := b.anim.trans["opacity"].start
	b.tickAnimations(start.Add(50 * time.Millisecond))
	if o := b.Style().Opacity; math.Abs(float64(o)-0.6) > 0.02 {
		t.Fatalf("opacity halfway = %v, want 0.6", o)
	}
	if done := b.tickAnimations(start.Add(150 * time.Millisecond)); !done {
		t.Fatal("transition still running after its duration")
	}
	if o := b.Style().Opacity; math.Abs(float64(o)-0.2) > 0.001 {
		t.Fatalf("final opacity = %v", o)
	}
	got := strings.Join(events, " ")
	for _, want := range []string{"transitionrun:opacity", "transitionstart:opacity", "transitionend:opacity", "transitionend:width"} {
		if !strings.Contains(got, want) {
			t.Errorf("events %q lack %q", got, want)
		}
	}
}

func TestKeyframesFillForwards(t *testing.T) {
	_, res := renderWeb(t, `<!doctype html><html><head><style>
@keyframes fadeIn { from { opacity: 0; transform: translateY(20px) } to { opacity: 1; transform: none } }
#a { opacity: 0; animation: fadeIn 200ms ease-out 2 alternate forwards; width: 10px; height: 10px; }
</style></head><body><div id=a></div></body></html>`)
	a := res.ByID["a"].(*El)
	var events []string
	res.Engine.SetAnimationEventHandler(func(el *El, typ, name string, elapsed float64) {
		events = append(events, typ)
	})
	if a.anim == nil || len(a.anim.anims) != 1 {
		t.Fatalf("animation not started: %+v", a.anim)
	}
	start := a.anim.anims[0].start
	a.tickAnimations(start.Add(time.Millisecond))
	if o := a.Style().Opacity; o > 0.1 || o == 0 {
		t.Fatalf("opacity at start = %v, want ~0", o)
	}
	if a.Box.Transform == nil {
		t.Fatal("no transform at start")
	}
	a.tickAnimations(start.Add(210 * time.Millisecond)) // second, reversed iteration
	if o := a.Style().Opacity; o < 0.9 && o != 0 {
		t.Fatalf("opacity early in the reversed iteration = %v, want near 1", o)
	}
	if done := a.tickAnimations(start.Add(500 * time.Millisecond)); !done {
		t.Fatal("still running after 2 iterations")
	}
	// alternate ×2 ends on the "from" keyframe; forwards holds it.
	if o := a.Style().Opacity; o > 0.01 && o != 0.0001 {
		t.Fatalf("held opacity = %v, want 0 (from)", o)
	}
	if got := strings.Join(events, " "); got != "animationstart animationiteration animationend" {
		t.Fatalf("events %q", got)
	}
}

func TestParseEasingAndTime(t *testing.T) {
	if d := parseTime(".3s"); d != 300*time.Millisecond {
		t.Fatal(d)
	}
	if d := parseTime("150ms"); d != 150*time.Millisecond {
		t.Fatal(d)
	}
	e := parseEasing("ease-in-out")
	if v := e(0.5); math.Abs(v-0.5) > 0.01 {
		t.Fatalf("ease-in-out(0.5) = %v", v)
	}
	if v := parseEasing("steps(4, end)")(0.6); v != 0.5 {
		t.Fatalf("steps = %v", v)
	}
}

// A running animation reports its dirty rect in window space: inside a
// scrolled view that is the element's on-screen place, not its content
// coordinates (which would repaint the wrong band and leave it stale).
func TestAnimatorDirtyRectInWindowSpace(t *testing.T) {
	win, res := renderWeb(t, `<!doctype html><html><head><style>
body { margin: 0 }
#pad { height: 300px }
#b { opacity: 1; transition: opacity 100ms linear; width: 50px; height: 40px; }
#b.off { opacity: 0.2 }
</style></head><body><div id=pad></div><div id=b></div><div style="height:600px"></div></body></html>`)
	sv := widgets.NewScrollView()
	sv.SetContentAuto(res.Root)
	win.SetRoot(sv)
	sv.Measure(qui.Size{W: 400, H: 300})
	sv.Layout(qui.Rect{W: 400, H: 300})
	sv.ScrollTo(200)
	sv.Layout(qui.Rect{W: 400, H: 300})
	b := res.ByID["b"].(*El)
	b.SetClass("off")
	res.Engine.FlushPendingStyles()
	if b.anim == nil {
		t.Fatal("no transition started")
	}
	dirty, _ := (&elAnimator{e: b}).Tick(b.anim.trans["opacity"].start.Add(50 * time.Millisecond))
	want := qui.PaintBoundsInWindow(b)
	if dirty != want {
		t.Fatalf("animator dirty rect %+v, want window-space %+v", dirty, want)
	}
	if want.Y > 150 {
		t.Fatalf("element should sit near y=100 on screen after scrolling 200px, at %+v", want)
	}
}

// Toggling only animation-play-state (react.dev's marquee starts paused
// and a script sets it running when the section scrolls into view) must
// resume the animation, and pausing must hold the current frame.
func TestAnimationPlayStateToggle(t *testing.T) {
	_, res := renderWeb(t, `<!doctype html><html><head><style>
@keyframes fade { from { opacity: 0 } to { opacity: 1 } }
#a { animation: fade 1000ms linear infinite; width: 10px; height: 10px; }
</style></head><body><div id=a style="animation-play-state:paused"></div></body></html>`)
	a := res.ByID["a"].(*El)
	if a.anim == nil || len(a.anim.anims) != 1 {
		t.Fatalf("animation not created: %+v", a.anim)
	}
	run := a.anim.anims[0]
	if !a.tickAnimations(time.Now().Add(400 * time.Millisecond)) {
		t.Fatal("a paused animation still asks for frames")
	}
	if o := a.Style().Opacity; o > 0.05 {
		t.Fatalf("paused animation advanced: opacity %v", o)
	}
	a.SetAttr("style", "animation-play-state: running;")
	res.Engine.FlushPendingStyles()
	if run.spec.paused {
		t.Fatal("play-state change did not reach the running animation")
	}
	resumed := time.Now()
	if a.tickAnimations(resumed.Add(500 * time.Millisecond)) {
		t.Fatal("a resumed infinite animation reports done")
	}
	if o := a.Style().Opacity; math.Abs(float64(o)-0.5) > 0.1 {
		t.Fatalf("opacity 500ms after resume = %v, want ~0.5", o)
	}
	a.SetAttr("style", "animation-play-state: paused;")
	res.Engine.FlushPendingStyles()
	held := a.Style().Opacity
	a.tickAnimations(time.Now().Add(300 * time.Millisecond))
	if o := a.Style().Opacity; math.Abs(float64(o-held)) > 0.02 {
		t.Fatalf("paused again but opacity moved %v -> %v", held, o)
	}
}
