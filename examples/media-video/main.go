// Media smoke test: open an mp4 / mov and play it (with
// synchronized audio) inside a minimal qui window. Video runs via
// the AVAssetReader + CVOpenGLTextureCache path; audio runs via
// AVAudioFile + AVAudioEngine. The two share an audioClock so
// video frames promote against audio host time.
//
// Usage:
//
//	go run ./examples/media-video /path/to/video.mp4
//
// Play / Pause / Stop control transport. The scrubber seeks; the
// volume slider gates the audio. Spacebar toggles play/pause.
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
		log.Fatal("usage: media-video <path-to-video-file>")
	}
	path := flag.Arg(0)

	app, err := qui.NewApp()
	if err != nil {
		log.Fatal(err)
	}
	window, err := app.NewWindow("media-video", 960, 640)
	if err != nil {
		log.Fatal(err)
	}
	window.SetRenderer(qui.NewGLRenderer())

	view := media.NewVideoView()
	defer view.Destroy()
	view.SetFlex(1) // fill remaining vertical space

	if err := view.Open(path); err != nil {
		log.Fatalf("open %q: %v", path, err)
	}
	track := view.VideoTrack()

	title := widgets.NewLabel(fmt.Sprintf("%s  (%dx%d @ %.2gfps, %s)",
		path, track.Width, track.Height, track.FPS, formatDur(track.Duration)))
	title.Style().Foreground = qui.ColorWhite
	title.Style().Font.Size = 13

	status := widgets.NewLabel("paused")
	status.Style().Foreground = qui.Color{R: 0.7, G: 0.85, B: 1, A: 1}
	status.Style().Font.Size = 16

	playBtn := widgets.NewButton("Play", nil)
	pauseBtn := widgets.NewButton("Pause", nil)
	stopBtn := widgets.NewButton("Stop", nil)

	durSec := float32(track.Duration.Seconds())
	scrub := widgets.NewSlider(0, durSec, 0, nil)
	scrub.SetFlex(1)

	scrub.OnChange = func(v float32) {
		_ = view.SeekTo(time.Duration(v * float32(time.Second)))
	}

	// Volume slider — fixed-width on the right of the controls row.
	// No-op when the file has no audio track.
	volume := widgets.NewSlider(0, 1, 1, nil)
	volume.OnChange = func(v float32) {
		view.SetVolume(v)
	}
	volLabel := widgets.NewLabel("vol")
	volLabel.Style().Foreground = qui.ColorWhite
	volLabel.Style().Font.Size = 12

	// Playback speed picker. The clock + audio renderer both honor
	// the rate — audio pitch is preserved by AVAudioUnitVarispeed on
	// darwin, video frames pace against the rate-scaled host time.
	speedLabel := widgets.NewLabel("speed")
	speedLabel.Style().Foreground = qui.ColorWhite
	speedLabel.Style().Font.Size = 12
	speedOptions := []string{"0.5x", "0.75x", "1x", "1.25x", "1.5x", "2x", "3x"}
	speedRates := []float32{0.5, 0.75, 1.0, 1.25, 1.5, 2.0, 3.0}
	speed := widgets.NewSelect(window, speedOptions, func(idx int, _ string) {
		if idx < 0 || idx >= len(speedRates) {
			return
		}
		view.SetRate(speedRates[idx])
	})
	speed.SelectedIdx = 2 // 1x
	speed.SetFixedWidth(80)
	playBtn.OnClick = func() {
		if err := view.Play(); err != nil {
			log.Println("play:", err)
		}
	}
	pauseBtn.OnClick = func() {
		if err := view.Pause(); err != nil {
			log.Println("pause:", err)
		}
	}
	stopBtn.OnClick = func() {
		if err := view.Stop(); err != nil {
			log.Println("stop:", err)
		}
	}

	view.OnTimeUpdate(func(p time.Duration) {
		// SetText handles the InvalidateLayout; scrub still needs an
		// explicit InvalidateRect because Slider's Value mutation
		// doesn't self-invalidate by design (it's mutated from many
		// drag callsites; centralized invalidation lives in the
		// caller's input handler).
		status.SetText(fmt.Sprintf("%s / %s",
			formatDur(p), formatDur(track.Duration)))
		scrub.Value = float32(p.Seconds())
		window.InvalidateRect(scrub.Bounds())
	})
	view.OnEnded(func() {
		status.SetText("ended")
	})

	btnGroup := qui.NewContainer(
		qui.FlexLayout{Direction: qui.Horizontal, Gap: 6, AlignItems: qui.AlignCenter},
		playBtn, pauseBtn, stopBtn,
	)
	volGroup := qui.NewContainer(
		qui.FlexLayout{Direction: qui.Horizontal, Gap: 6, AlignItems: qui.AlignCenter},
		volLabel, volume,
	)
	speedGroup := qui.NewContainer(
		qui.FlexLayout{Direction: qui.Horizontal, Gap: 6, AlignItems: qui.AlignCenter},
		speedLabel, speed,
	)
	controls := qui.NewContainer(
		qui.FlexLayout{Direction: qui.Horizontal, Gap: 20, AlignItems: qui.AlignCenter},
		btnGroup, scrub, speedGroup, volGroup,
	)
	controls.Style().Padding = qui.Insets{Top: 6, Right: 4, Bottom: 4, Left: 4}
	// Add an audio-track hint to the title so users can see at a
	// glance whether the file has audio.
	audioHint := ""
	if at := view.AudioTrack(); at != nil {
		audioHint = fmt.Sprintf(", audio %dHz×%d", at.SampleRate, at.Channels)
	} else {
		audioHint = ", silent"
	}
	title.SetText(fmt.Sprintf("%s  (%dx%d @ %.2gfps, %s%s)",
		path, track.Width, track.Height, track.FPS, formatDur(track.Duration), audioHint))

	root := qui.NewContainer(
		qui.FlexLayout{Direction: qui.Vertical, Gap: 10},
		title, view, status, controls,
	)
	root.Style().Background = qui.Color{R: 0.10, G: 0.10, B: 0.12, A: 1}
	root.Style().Padding = qui.Insets{Top: 10, Right: 14, Bottom: 12, Left: 14}
	window.SetRoot(root)

	// Spacebar toggles play/pause for ergonomics.
	accel := qui.NewAcceleratorRegistry()
	_ = accel.Register("Space", func() {
		if view.State() == media.StatePlaying {
			_ = view.Pause()
		} else {
			_ = view.Play()
		}
	})
	window.SetAcceleratorRegistry(accel)

	app.Run()
}

func formatDur(d time.Duration) string {
	if d <= 0 {
		return "0:00"
	}
	m := int(d.Minutes())
	s := int(d.Seconds()) - m*60
	return fmt.Sprintf("%d:%02d", m, s)
}
