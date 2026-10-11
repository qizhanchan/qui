//go:build cgo

package media

import (
	"encoding/binary"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/qizhanchan/qui/media/internal/audioout"
	"github.com/qizhanchan/qui/media/internal/mp4x"
)

// captureSink records everything written to it.
type captureSink struct {
	mu  sync.Mutex
	pcm []byte
}

func (c *captureSink) Write(p []byte) (int, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.pcm) > 1<<20 {
		return 0, nil
	}
	c.pcm = append(c.pcm, p...)
	return len(p), nil
}
func (c *captureSink) Buffered() time.Duration { return 0 }
func (c *captureSink) Flush() {
	c.mu.Lock()
	c.pcm = nil
	c.mu.Unlock()
}
func (c *captureSink) SetRate(float32)         {}
func (c *captureSink) Play() error             { return nil }
func (c *captureSink) Pause() error            { return nil }
func (c *captureSink) SetVolume(float32)       {}
func (c *captureSink) HostTime() time.Duration { return 0 }
func (c *captureSink) Close() error            { return nil }

func (c *captureSink) samples() []int16 {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]int16, len(c.pcm)/2)
	for i := range out {
		out[i] = int16(binary.LittleEndian.Uint16(c.pcm[2*i:]))
	}
	return out
}

func openTestAudio(t *testing.T) (*golangAudio, *captureSink) {
	t.Helper()
	r, err := mp4x.Open(e2eFile)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { r.Close() })
	cs := &captureSink{}
	a, err := newGolangAudioSink(r, r.AudioTrack(), func(audioout.Format) (audioout.Sink, error) { return cs, nil })
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { a.Close() })
	return a, cs
}

func waitSamples(cs *captureSink, n int) []int16 {
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if s := cs.samples(); len(s) >= n {
			return s
		}
		time.Sleep(5 * time.Millisecond)
	}
	return cs.samples()
}

// TestGolangAudioMatchesFFmpeg checks the renderer's PCM against FFmpeg's
// decode of the same MP4 (edit-list priming removed), from the start and
// after a seek.
func TestGolangAudioMatchesFFmpeg(t *testing.T) {
	raw, err := os.ReadFile("testdata/bframes_aac_160x90.ref.s16")
	if err != nil {
		t.Fatal(err)
	}
	ref := make([]int16, len(raw)/2)
	for i := range ref {
		ref[i] = int16(binary.LittleEndian.Uint16(raw[2*i:]))
	}
	a, cs := openTestAudio(t)
	if a.info.SampleRate != 44100 || a.info.Channels != 2 {
		t.Fatalf("format %d Hz x %d", a.info.SampleRate, a.info.Channels)
	}
	got := waitSamples(cs, len(ref))
	compareS16(t, "start", got, ref)

	// Seek to 2048 frames: the output must continue the reference there.
	_ = a.SeekTo(time.Duration(2048) * time.Second / 44100)
	got = waitSamples(cs, len(ref)/2)
	compareS16(t, "seek", got, ref[2048*2:])
}

func compareS16(t *testing.T, name string, got, want []int16) {
	t.Helper()
	n := min(len(got), len(want))
	if n < len(want) {
		t.Fatalf("%s: only %d of %d samples", name, len(got), len(want))
	}
	bad := 0
	for i := 0; i < n; i++ {
		if d := int(got[i]) - int(want[i]); d > 1 || d < -1 {
			if bad < 3 {
				t.Errorf("%s: sample %d = %d, ffmpeg %d", name, i, got[i], want[i])
			}
			bad++
		}
	}
	if bad > 0 {
		t.Errorf("%s: %d of %d samples differ by more than 1 LSB", name, bad, n)
	}
}
