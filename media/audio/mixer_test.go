package audio

import (
	"math"
	"testing"
	"time"
)

func constant(l, r float32) Source {
	return SourceFunc(func(dst []float32) int {
		for i := 0; i < len(dst); i += 2 {
			dst[i], dst[i+1] = l, r
		}
		return len(dst)
	})
}

func TestMixGainPanAndEnd(t *testing.T) {
	m := NewOfflineMixer(Options{})
	a := m.Play(constant(0.25, 0.25))
	b := m.Play(NewBuffer([]float32{0.1, 0.1, 0.1, 0.1}, false)) // two frames, then ends
	out := make([]float32, 8)
	m.Mix(out)
	if math.Abs(float64(out[0]-0.35)) > 1e-5 || math.Abs(float64(out[4]-0.25)) > 1e-5 {
		t.Fatalf("mix = %v", out)
	}
	if b.Playing() || m.Voices() != 1 {
		t.Fatal("a finished buffer must leave the mix")
	}
	a.SetPan(1)
	m.Mix(out)
	if out[0] > 1e-6 || math.Abs(float64(out[1]-0.25*math.Sqrt2)) > 1e-5 {
		t.Fatalf("hard right pan = %v", out[:2])
	}
	a.SetGain(0)
	m.Mix(out)
	if out[1] != 0 {
		t.Fatal("zero gain should be silent")
	}
	a.Stop()
	m.Mix(out)
	if m.Voices() != 0 {
		t.Fatal("stopped voice still mixed")
	}
}

func TestSoftClipAndLoop(t *testing.T) {
	m := NewOfflineMixer(Options{})
	m.Play(constant(3, -3))
	out := make([]float32, 2)
	m.Mix(out)
	if out[0] <= 0.8 || out[0] > 1 || out[1] >= -0.8 || out[1] < -1 {
		t.Fatalf("limiter output %v", out)
	}
	loop := NewBuffer([]float32{1, 1, 2, 2}, true)
	got := make([]float32, 10)
	if n := loop.Read(got); n != 10 || got[8] != 1 || got[6] != 2 {
		t.Fatalf("loop read %d %v", n, got)
	}
}

// The real-time mixer advances voices with or without a device.
func TestRealtimeMixerAdvances(t *testing.T) {
	m, err := NewMixer(Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	var frames int
	m.Play(SourceFunc(func(dst []float32) int { frames += len(dst) / 2; return len(dst) }))
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		m.mu.Lock()
		n := frames
		m.mu.Unlock()
		if n > m.SampleRate()/50 {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("mixer pulled only %d frames (silent=%v)", frames, m.Silent())
}
