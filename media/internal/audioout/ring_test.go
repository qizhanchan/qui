package audioout

import (
	"encoding/binary"
	"testing"
	"time"
)

func ramp(frames, chs int) []byte {
	b := make([]byte, frames*chs*2)
	for i := 0; i < frames; i++ {
		for c := 0; c < chs; c++ {
			binary.LittleEndian.PutUint16(b[(i*chs+c)*2:], uint16(int16(i)))
		}
	}
	return b
}

func s16(b []byte, i int) int16 { return int16(binary.LittleEndian.Uint16(b[2*i:])) }

func TestRingCopyAndClock(t *testing.T) {
	r := newRing(Format{SampleRate: 1000, Channels: 1}, 1, false)
	if n := r.write(ramp(300, 1)); n != 600 {
		t.Fatalf("write accepted %d", n)
	}
	out := make([]byte, 200) // 100 device frames
	r.fill(out)
	if s16(out, 0) != 0 || s16(out, 99) != 99 {
		t.Fatalf("copy wrong: %d %d", s16(out, 0), s16(out, 99))
	}
	r.fill(out)
	// Device frame 150 carries content frame 150 → 150 ms.
	if ht := r.hostTime(150, true); ht != 150*time.Millisecond {
		t.Fatalf("hostTime(150) = %v", ht)
	}
	// Underrun: the third buffer carries only 100 frames; the clock stops
	// at their end however far the device runs.
	r.fill(out)
	r.fill(out)
	if ht := r.hostTime(390, true); ht != 300*time.Millisecond {
		t.Fatalf("hostTime past underrun = %v", ht)
	}
	r.flush()
	if ht := r.hostTime(390, true); ht != 0 {
		t.Fatalf("hostTime after flush = %v", ht)
	}
	r.write(ramp(100, 1))
	r.fill(out) // device frames 400..499 carry new-epoch content 0..99
	if ht := r.hostTime(450, true); ht != 50*time.Millisecond {
		t.Fatalf("hostTime after flush+fill = %v", ht)
	}
}

func TestRingSoftPauseRateVolume(t *testing.T) {
	r := newRing(Format{SampleRate: 1000, Channels: 2}, 1, true)
	r.write(ramp(1000, 2))
	out := make([]byte, 100*4)

	r.setPaused(true)
	r.fill(out) // silence, content does not move
	if s16(out, 0) != 0 || s16(out, 199) != 0 {
		t.Fatal("paused fill is not silent")
	}
	if ht := r.hostTime(50, true); ht != 0 {
		t.Fatalf("clock moved while paused: %v", ht)
	}
	r.setPaused(false)

	r.setSpeed(2)
	r.fill(out) // device 100..199 carry content 0..198 at 2x
	if got := s16(out, 2*10); got != 20 {
		t.Fatalf("2x frame 10 = %d, want 20", got)
	}
	if ht := r.hostTime(150, true); ht != 100*time.Millisecond {
		t.Fatalf("2x hostTime = %v", ht)
	}

	r.setSpeed(1)
	r.setVolume(0.5)
	r.fill(out) // content continues at 200
	if got := s16(out, 0); got != 100 {
		t.Fatalf("1x after 2x, half volume: first sample %d, want 100", got)
	}
	if ht := r.hostTime(200, true); ht != 200*time.Millisecond {
		t.Fatalf("hostTime after rate change = %v", ht)
	}
}
