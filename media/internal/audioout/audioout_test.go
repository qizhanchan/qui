package audioout

import "testing"

// TestOpenOrUnsupported exercises the sink seam: Open either returns a working
// sink or ErrNotSupported (CGO disabled / no device). Both are valid outcomes;
// the point is that the seam never panics and Close is idempotent.
func TestOpenOrUnsupported(t *testing.T) {
	s, err := Open(Format{SampleRate: 48000, Channels: 2})
	if err == ErrNotSupported {
		if Available() {
			t.Fatal("Available() is true but Open returned ErrNotSupported")
		}
		return
	}
	if err != nil {
		t.Skipf("no audio device available: %v", err)
	}
	if s == nil {
		t.Fatal("Open returned a nil sink without an error")
	}
	if _, err := s.Write(make([]byte, 4096)); err != nil {
		t.Errorf("Write: %v", err)
	}
	s.SetVolume(0.5)
	if err := s.Pause(); err != nil {
		t.Errorf("Pause: %v", err)
	}
	if err := s.Close(); err != nil {
		t.Errorf("Close: %v", err)
	}
	if err := s.Close(); err != nil {
		t.Errorf("second Close: %v", err)
	}
}

func TestAvailableConsistent(t *testing.T) {
	// Available must be stable across calls.
	if Available() != Available() {
		t.Fatal("Available() is not stable")
	}
}
