package media

import (
	"errors"
	"testing"
)

// TestBackendRegistered guards against the most embarrassing bug we
// could ship: the darwin backend's init() somehow not running, so
// AudioPlayer reports ErrNotSupported on macOS. Mirrors the same
// kind of "is the platform backend wired up" smoke check that
// scene3d would benefit from.
func TestBackendRegistered(t *testing.T) {
	if activeBackend() == nil {
		t.Fatal("no backend registered; check init() in backend_darwin.go / backend_other.go")
	}
	caps := activeBackend().Capabilities()
	if len(caps.AudioCodecs) == 0 {
		t.Error("backend reports no audio codecs — Phase 1 needs at least one")
	}
}

// TestAudioPlayerNotSupported and TestOpenNonexistent exercise the
// public surface without requiring a media fixture in the repo. The
// real "does playback actually work?" validation lives in
// examples/media-audio, which the user runs manually with a file
// they choose.
func TestAudioPlayerOpenNonexistent(t *testing.T) {
	if activeBackend() == nil {
		t.Skip("no backend on this platform — covered by build tags, not tests")
	}
	p, err := NewAudioPlayer()
	if err != nil {
		t.Fatalf("NewAudioPlayer: %v", err)
	}
	defer p.Close()

	err = p.Open("/nonexistent/path/to/file.mp3")
	if err == nil {
		t.Fatal("expected Open to fail on missing file, got nil")
	}
	if errors.Is(err, ErrClosed) {
		t.Errorf("got ErrClosed for missing-file open, expected a wrapped backend error: %v", err)
	}
}

// TestStateTransitions exercises the Go-side state machine without
// any actual audio data — methods on a player without a source open
// should return ErrNoTrack, not panic or hang.
func TestAudioPlayerMethodsWithoutSource(t *testing.T) {
	if activeBackend() == nil {
		t.Skip("no backend on this platform")
	}
	p, err := NewAudioPlayer()
	if err != nil {
		t.Fatalf("NewAudioPlayer: %v", err)
	}
	defer p.Close()

	if got := p.State(); got != StateIdle {
		t.Errorf("fresh player state = %v, want %v", got, StateIdle)
	}
	if err := p.Play(); err != ErrNoTrack {
		t.Errorf("Play() with no source = %v, want %v", err, ErrNoTrack)
	}
	if err := p.Pause(); err != ErrNoTrack {
		t.Errorf("Pause() with no source = %v, want %v", err, ErrNoTrack)
	}
	if err := p.SeekTo(0); err != ErrNoTrack {
		t.Errorf("SeekTo() with no source = %v, want %v", err, ErrNoTrack)
	}
	if got := p.Duration(); got != 0 {
		t.Errorf("Duration() with no source = %v, want 0", got)
	}
	if got := p.Position(); got != 0 {
		t.Errorf("Position() with no source = %v, want 0", got)
	}
	// These shouldn't crash even though there's no source.
	p.SetVolume(0.5)
	p.SetRate(1.0)
	p.SetLoop(true)
}
