package qui

import (
	"log"
	"os"
	"time"
)

// QUI_SNAPSHOT: render-and-exit for any qui app.
//
// With QUI_SNAPSHOT=/path/out.png set, App.Run waits for the main window
// to paint and then go quiet (no frame for QUI_SNAPSHOT_SETTLE, default
// 300ms — long enough for a mount-time reconcile and first layout to
// finish), writes the frame to that path as PNG, and quits. Combined with
// the headless backend it is an offscreen rendering check that needs no
// display, no GPU and no code change:
//
//	QUI_PLATFORM=headless QUI_SNAPSHOT=/tmp/ui.png go run ./examples/reactive-html
//
// QUI_SNAPSHOT_TIMEOUT (default 10s) bounds the wait for a UI that never
// settles (a spinner); the snapshot is taken anyway when it expires, so a
// CI job always gets a picture to look at instead of a hang.

type envSnapshot struct {
	path      string
	settle    time.Duration
	deadline  time.Time
	lastPaint time.Time
	painted   bool
}

func newEnvSnapshot() *envSnapshot {
	path := os.Getenv("QUI_SNAPSHOT")
	if path == "" {
		return nil
	}
	return &envSnapshot{
		path:     path,
		settle:   envDuration("QUI_SNAPSHOT_SETTLE", 300*time.Millisecond),
		deadline: time.Now().Add(envDuration("QUI_SNAPSHOT_TIMEOUT", 10*time.Second)),
	}
}

func envDuration(name string, def time.Duration) time.Duration {
	if v := os.Getenv(name); v != "" {
		if d, err := time.ParseDuration(v); err == nil && d >= 0 {
			return d
		}
		log.Printf("qui: ignoring %s=%q (want a duration like 500ms)", name, v)
	}
	return def
}

// timeout caps the loop's sleep so the settle deadline is noticed even
// when nothing else wakes the loop.
func (s *envSnapshot) timeout(t time.Duration) time.Duration {
	if s == nil || s.settle <= 0 {
		return t
	}
	if t <= 0 || t > s.settle {
		return s.settle
	}
	return t
}

// poll runs after every loop iteration. It reports true once the snapshot
// has been written (or failed) and the app asked to quit.
func (s *envSnapshot) poll(a *App) bool {
	w := a.MainWindow()
	if w == nil {
		return false
	}
	now := time.Now()
	if w.framePainted {
		s.painted = true
		s.lastPaint = now
	}
	quiet := s.painted && now.Sub(s.lastPaint) >= s.settle && w.PendingJobs() == 0
	if !quiet && now.Before(s.deadline) {
		return false
	}
	img := w.Snapshot()
	if img == nil {
		log.Printf("qui: QUI_SNAPSHOT: window rendered no frame")
	} else if err := SaveSnapshotPNG(s.path, img); err != nil {
		log.Printf("qui: QUI_SNAPSHOT: %v", err)
	} else {
		log.Printf("qui: QUI_SNAPSHOT: wrote %s (%dx%d)", s.path, img.Rect.Dx(), img.Rect.Dy())
	}
	a.Quit()
	return true
}
