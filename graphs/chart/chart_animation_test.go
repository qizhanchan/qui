package chart

import (
	"testing"
	"time"

	"github.com/qizhanchan/qui"
)

func TestChartAnimationAdvancesAndStops(t *testing.T) {
	c := New(Option{
		AnimationDuration: 100,
		Series: []SeriesOption{{
			Type: SeriesLine,
			Data: Numbers(1, 3, 2),
		}},
	})
	c.Layout(qui.Rect{W: 400, H: 300})
	start := time.Unix(100, 0)
	if dirty := c.Tick(start); dirty.IsEmpty() {
		t.Fatal("first animation tick did not dirty the chart")
	}
	if !c.animating || c.animationProgress != 0 {
		t.Fatalf("first tick state = animating %v progress %v", c.animating, c.animationProgress)
	}

	if dirty := c.Tick(start.Add(50 * time.Millisecond)); dirty.IsEmpty() {
		t.Fatal("mid-animation tick did not dirty the chart")
	}
	if c.animationProgress < .49 || c.animationProgress > .51 {
		t.Fatalf("mid progress = %v, want ~.5", c.animationProgress)
	}
	rec := &qui.RecordingCanvas{}
	c.Draw(rec)
	if len(rec.Layers) != 1 || rec.Layers[0].Paint.Alpha <= 0 || rec.Layers[0].Paint.Alpha >= 1 {
		t.Fatalf("animation layer = %+v, want a partial-alpha series layer", rec.Layers)
	}
	if len(rec.Polylines) == 0 {
		t.Fatal("animated line series was not drawn")
	}

	if dirty := c.Tick(start.Add(100 * time.Millisecond)); dirty.IsEmpty() {
		t.Fatal("final animation tick did not dirty the chart")
	}
	if c.animating || c.animationProgress != 1 {
		t.Fatalf("final state = animating %v progress %v", c.animating, c.animationProgress)
	}
	if dirty := c.Tick(start.Add(200 * time.Millisecond)); !dirty.IsEmpty() {
		t.Fatalf("settled chart kept dirtying: %+v", dirty)
	}
	c.RestartAnimation()
	if !c.animating || c.animationProgress != 0 {
		t.Fatalf("RestartAnimation did not reset progress: animating=%v progress=%v", c.animating, c.animationProgress)
	}
}

func TestChartAnimationCanBeDisabled(t *testing.T) {
	animation := false
	c := New(Option{
		Animation: &animation,
		Series:    []SeriesOption{{Type: SeriesBar, Data: Numbers(1, 2)}},
	})
	c.Layout(qui.Rect{W: 400, H: 300})
	if c.animating {
		t.Fatal("animation should be disabled")
	}
	if dirty := c.Tick(time.Unix(1, 0)); !dirty.IsEmpty() {
		t.Fatalf("disabled chart dirtied on tick: %+v", dirty)
	}
}

func TestAnimationEasingCatalog(t *testing.T) {
	if got := easeAnimation(.5, "linear"); got != .5 {
		t.Fatalf("linear midpoint = %v", got)
	}
	if got := easeAnimation(.5, "cubicIn"); got != .125 {
		t.Fatalf("cubicIn midpoint = %v", got)
	}
	if got := easeAnimation(.5, "cubicInOut"); got != .5 {
		t.Fatalf("cubicInOut midpoint = %v", got)
	}
	if got := easeAnimation(.5, "quarticOut"); got != .9375 {
		t.Fatalf("quarticOut midpoint = %v", got)
	}
}
