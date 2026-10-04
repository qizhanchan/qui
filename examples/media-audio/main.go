// Media smoke test: open an audio file and play it inside a
// minimal qui window. Validates the cgo plumbing end-to-end —
// AVAudioPlayer open, transport controls, the AVAudioPlayerDelegate
// → quiAudioDidEnd → Go OnEnded chain, and the handle registry.
//
// Usage:
//
//	go run ./examples/media-audio /path/to/song.mp3
//
// Click Play / Pause to control transport. The label updates each
// frame with the current position. When playback finishes, the
// label flips to "ended".
package main

import (
	"flag"
	"fmt"
	"log"
	"time"

	"github.com/qizhanchan/qui"
	"github.com/qizhanchan/qui/media"
	"github.com/qizhanchan/qui/widgets"
)

func main() {
	flag.Parse()
	if flag.NArg() < 1 {
		log.Fatal("usage: media-audio <path-to-audio-file>")
	}
	path := flag.Arg(0)

	app, err := qui.NewApp()
	if err != nil {
		log.Fatal(err)
	}
	window, err := app.NewWindow("media-audio", 480, 200)
	if err != nil {
		log.Fatal(err)
	}
	window.SetRenderer(qui.NewGLRenderer())

	player, err := media.NewAudioPlayer()
	if err != nil {
		log.Fatalf("media: %v", err)
	}
	defer player.Close()

	if err := player.Open(path); err != nil {
		log.Fatalf("open %q: %v", path, err)
	}

	title := widgets.NewLabel(fmt.Sprintf("%s  (%s)", path, formatDur(player.Duration())))
	title.Style().Foreground = qui.ColorWhite
	title.Style().Font.Size = 14

	status := widgets.NewLabel("paused")
	status.Style().Foreground = qui.Color{R: 0.7, G: 0.85, B: 1, A: 1}
	status.Style().Font.Size = 18

	playBtn := widgets.NewButton("Play", nil)
	pauseBtn := widgets.NewButton("Pause", nil)
	stopBtn := widgets.NewButton("Stop", nil)

	playBtn.OnClick = func() {
		if err := player.Play(); err != nil {
			log.Println("play:", err)
		}
	}
	pauseBtn.OnClick = func() {
		if err := player.Pause(); err != nil {
			log.Println("pause:", err)
		}
	}
	stopBtn.OnClick = func() {
		if err := player.Stop(); err != nil {
			log.Println("stop:", err)
		}
	}

	player.OnEnded(func() {
		// AVAudioPlayer delivers this on the main thread (the
		// thread we called Play from), so it's safe to touch widgets
		// directly. Still, this is rare — most apps should hop to
		// main if they're unsure.
		status.SetText("ended")
	})

	controls := qui.NewContainer(
		qui.FlexLayout{Direction: qui.Horizontal, Gap: 8},
		playBtn, pauseBtn, stopBtn,
	)
	root := qui.NewContainer(
		qui.FlexLayout{Direction: qui.Vertical, Gap: 12},
		title, status, controls,
	)
	root.Style().Background = qui.Color{R: 0.12, G: 0.12, B: 0.14, A: 1}
	window.SetRoot(root)

	// Per-frame: refresh the status label with the current position.
	// Implemented as a Tickable via the framework's animator hook —
	// register a tiny driver that updates the label each frame.
	window.RegisterAnimator(&statusTicker{
		player: player,
		label:  status,
		window: window,
	})

	app.Run()
}

func formatDur(d time.Duration) string {
	if d <= 0 {
		return "?"
	}
	m := int(d.Minutes())
	s := int(d.Seconds()) - m*60
	return fmt.Sprintf("%d:%02d", m, s)
}

// statusTicker is a tiny qui.Animator that pokes the status label
// every frame. We use the animator interface instead of making the
// Label itself Tickable so we don't have to subclass widgets.Label.
type statusTicker struct {
	player *media.AudioPlayer
	label  *widgets.Label
	window *qui.Window
}

func (s *statusTicker) Tick(now time.Time) (qui.Rect, bool) {
	// Only refresh once we're actively playing; idle state would
	// burn CPU for no reason.
	if s.player.State() == media.StatePlaying {
		s.label.SetText(fmt.Sprintf("%s / %s",
			formatDur(s.player.Position()),
			formatDur(s.player.Duration())))
		return s.label.Bounds(), false
	}
	return qui.Rect{}, false
}

// Stop is required by qui.Animator. The status ticker never reports
// done=true, so the framework never calls Stop on its own. It's a
// no-op so we don't have to plumb cleanup through the example.
func (s *statusTicker) Stop() {}
