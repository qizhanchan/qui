package main

import (
	"log"
	"time"

	"github.com/qizhanchan/qui"
	"github.com/qizhanchan/qui/anim"
	"github.com/qizhanchan/qui/widgets"
)

// Three demos, arranged top-to-bottom:
//
//  1. A Timeline loops a color square moving along a ping-pong X path.
//  2. A Spring animates a button's "bounce" reaction on click.
//  3. A Tween fades a label's opacity in/out when the window loads.
//
// All three use RegisterAnimator. The Timeline lives for the program's
// entire lifetime; the Spring is re-triggered on click; the Tween runs
// once at startup.
func main() {
	app, err := qui.NewApp()
	if err != nil {
		log.Fatal(err)
	}
	window, err := app.NewWindow("qui — animation framework demo", 720, 480)
	if err != nil {
		log.Fatal(err)
	}
	window.SetRenderer(qui.NewGLRenderer())

	// --- Demo 1: Timeline-driven color + position ping-pong ---
	stage := qui.NewContainer(qui.AbsoluteLayout{})
	stage.Style().Background = qui.Color{R: 0.10, G: 0.10, B: 0.13, A: 1}

	mover := widgets.NewLabel("Timeline")
	mover.Style().Background = qui.Color{R: 0.3, G: 0.5, B: 0.9, A: 1}
	mover.Style().Foreground = qui.ColorWhite
	mover.Style().Font.Size = 14
	mover.Style().Padding = qui.Insets{Top: 12, Right: 12, Bottom: 12, Left: 12}
	mover.SetAbsolutePosition(qui.AbsolutePosition{
		Anchor: qui.AnchorLeft | qui.AnchorTop,
		Top:    20,
		Left:   20,
	})
	stage.AddChild(mover)

	timeline := &anim.Timeline{
		Loop:        anim.LoopPingPong,
		DirtyBounds: func() qui.Rect { return window.Bounds() },
	}
	timeline.AddFloatFunc(func(left float32) {
		mover.UpdateAbsolutePosition(func(position *qui.AbsolutePosition) { position.Left = left })
	}, []anim.Keyframe[float32]{
		{At: 0, Value: 20, Easing: anim.Linear},
		{At: 1500 * time.Millisecond, Value: 560, Easing: anim.EaseInOutCubic},
	})
	timeline.AddColor(func(c qui.Color) { mover.Style().Background = c }, []anim.Keyframe[qui.Color]{
		{At: 0, Value: qui.Color{R: 0.3, G: 0.5, B: 0.9, A: 1}, Easing: anim.Linear},
		{At: 1500 * time.Millisecond, Value: qui.Color{R: 0.95, G: 0.4, B: 0.5, A: 1}, Easing: anim.EaseInOutCubic},
	})
	window.RegisterAnimator(timeline)

	// --- Demo 2: Spring-driven scale on click (simulated via padding) ---
	springPad := float32(24)
	springBtn := widgets.NewButton("Spring!", nil)
	springBtn.Style().Padding = qui.Insets{Top: springPad, Right: springPad, Bottom: springPad, Left: springPad}
	var spring *anim.Spring
	springBtn.OnClick = func() {
		if spring != nil {
			spring.Stop()
		}
		spring = &anim.Spring{
			Stiffness: 180,
			Damping:   12,
			Mass:      1,
			Value:     64,
			Target:    24,
			Apply: func(v float32) {
				springBtn.Style().Padding = qui.Insets{Top: v, Right: v, Bottom: v, Left: v}
				springBtn.InvalidateLayout()
			},
			DirtyBounds: func() qui.Rect { return window.Bounds() },
		}
		window.RegisterAnimator(spring)
	}
	springBtn.SetAbsolutePosition(qui.AbsolutePosition{
		Anchor: qui.AnchorLeft | qui.AnchorTop,
		Top:    120,
		Left:   40,
	})
	stage.AddChild(springBtn)

	// --- Demo 3: Tween opacity of a welcome label once at startup ---
	welcome := widgets.NewLabel("Welcome — animation framework online")
	welcome.Style().Foreground = qui.Color{R: 1, G: 1, B: 1, A: 0}
	welcome.Style().Font.Size = 18
	welcome.SetAbsolutePosition(qui.AbsolutePosition{
		Anchor: qui.AnchorLeft | qui.AnchorTop,
		Top:    260,
		Left:   40,
	})
	stage.AddChild(welcome)

	fade := &anim.Tween[float32]{
		From:     0,
		To:       1,
		Duration: 1200 * time.Millisecond,
		Easing:   anim.EaseOutCubic,
		Lerp:     anim.LerpFloat,
		Apply: func(a float32) {
			welcome.Style().Foreground = qui.Color{R: 1, G: 1, B: 1, A: a}
		},
		DirtyBounds: func() qui.Rect { return welcome.Bounds() },
	}
	window.RegisterAnimator(fade)

	window.SetRoot(stage)
	app.Run()
}
