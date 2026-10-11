package audioout

import (
	"testing"
	"time"
)

// TestHostTimeTracksPlayback plays silence (volume 0) and checks the
// content clock advances with wall time, holds while paused and restarts
// after Flush.
func TestHostTimeTracksPlayback(t *testing.T) {
	if !Available() {
		t.Skip("no sink")
	}
	s, err := Open(Format{SampleRate: 44100, Channels: 2})
	if err != nil {
		t.Skipf("no audio device: %v", err)
	}
	defer s.Close()
	s.SetVolume(0)
	pcm := make([]byte, 44100*4/2) // 0.5 s
	if n, _ := s.Write(pcm); n != len(pcm) {
		t.Fatalf("Write accepted %d of %d", n, len(pcm))
	}
	if n, _ := s.Write(pcm); n != 0 {
		t.Fatalf("Write past capacity accepted %d bytes", n)
	}
	s.Play()
	start := time.Now()
	time.Sleep(400 * time.Millisecond)
	ht := s.HostTime()
	el := time.Since(start)
	// Device buffers delay content (~140 ms of priming on macOS); the
	// clock may lead wall time only by sampling jitter.
	if ht < el-300*time.Millisecond || ht > el+20*time.Millisecond {
		t.Errorf("HostTime %v after %v of playback", ht, el)
	}
	s.Pause()
	time.Sleep(50 * time.Millisecond)
	p1 := s.HostTime()
	time.Sleep(200 * time.Millisecond)
	if p2 := s.HostTime(); p2-p1 > 20*time.Millisecond {
		t.Errorf("HostTime advanced while paused: %v -> %v", p1, p2)
	}
	s.Flush()
	if ht := s.HostTime(); ht != 0 {
		t.Errorf("HostTime after Flush = %v", ht)
	}
	s.Write(pcm)
	s.Play()
	time.Sleep(400 * time.Millisecond)
	if ht := s.HostTime(); ht < 150*time.Millisecond || ht > 400*time.Millisecond {
		t.Errorf("HostTime after flush+play = %v", ht)
	}
}
