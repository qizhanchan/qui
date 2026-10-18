//go:build cgo

package media

import (
	"encoding/binary"
	"math"
	"sync"
	"time"

	"github.com/qizhanchan/qui/media/internal/aac"
	"github.com/qizhanchan/qui/media/internal/audioout"
	"github.com/qizhanchan/qui/media/internal/mp4x"
)

// golangAudio is the pure-Go backend's AudioRenderer: AAC-LC access units
// from the MP4 are decoded on a producer goroutine (internal/aac) and fed
// to the platform sink (internal/audioout). The sink's sample clock drives
// HostTime, which VideoView uses as the master clock.
type golangAudio struct {
	r    *mp4x.Reader
	t    *mp4x.Track
	info *AudioTrackInfo
	sink audioout.Sink
	rate int // output sample rate
	chs  int

	mu      sync.Mutex
	dec     *aac.Decoder
	base    time.Duration // content time of the sink's zero
	next    int           // next access unit to decode
	trim    int           // frames still to drop (seek / edit-list priming)
	pending []byte        // decoded PCM not yet accepted by the sink
	eof     bool
	playing bool
	ended   bool
	onEnded func() // replaces any previous registration, like the interface says
	// onErr reports streaming decode failures (see errorSink); errReported
	// latches so a damaged stretch does not spam one callback per frame.
	onErr       func(error)
	errReported bool
	closed      bool
	// After the last sample has been handed to the device the clock keeps
	// running on wall time, so a video slightly longer than its audio
	// still reaches its end.
	drained   bool
	drainPos  time.Duration
	drainWall time.Time
	drainAcc  time.Duration
	wake      chan struct{}
	done      chan struct{}

	buf  []byte
	f32  []float32
	pcmB []byte
}

func golangAudioAvailable() bool { return audioout.Available() }

func newGolangAudio(r *mp4x.Reader, t *mp4x.Track) (*golangAudio, error) {
	return newGolangAudioSink(r, t, audioout.Open)
}

// newGolangAudioSink is newGolangAudio with an injectable sink (tests).
func newGolangAudioSink(r *mp4x.Reader, t *mp4x.Track, open func(audioout.Format) (audioout.Sink, error)) (*golangAudio, error) {
	asc, err := r.AudioSpecificConfig()
	if err != nil {
		return nil, err
	}
	cfg, err := aac.ParseASC(asc)
	if err != nil {
		return nil, err
	}
	dec, err := aac.NewDecoder(cfg)
	if err != nil {
		return nil, ErrCodecUnsupported
	}
	if t.SampleCount() == 0 {
		return nil, ErrNoTrack
	}
	a := &golangAudio{
		r: r, t: t, dec: dec, rate: dec.SampleRate(), chs: dec.Channels(),
		wake: make(chan struct{}, 1), done: make(chan struct{}),
	}
	sink, err := open(audioout.Format{SampleRate: a.rate, Channels: a.chs})
	if err != nil {
		return nil, err
	}
	a.sink = sink
	dur := t.Duration
	if n := t.SampleCount(); n > 0 {
		if end := r.PTS(t, n-1) + r.SampleDuration(t, n-1); end > 0 && (dur == 0 || end < dur+time.Second) {
			dur = end
		}
	}
	a.info = &AudioTrackInfo{Codec: "aac", SampleRate: a.rate, Channels: a.chs, Duration: dur}
	a.seekLocked(0)
	go a.produce()
	return a, nil
}

// seekLocked positions decoding so that content time t is the next frame
// sent to the sink. Caller holds a.mu.
func (a *golangAudio) seekLocked(t time.Duration) {
	idx := a.r.SampleAtTime(a.t, t)
	n := a.t.SampleCount()
	a.dec.Reset()
	a.pending = a.pending[:0]
	a.base = t
	a.eof = idx >= n
	a.ended = false
	a.drained = false
	a.errReported = false
	if a.eof {
		a.next = n
		return
	}
	// Decode one access unit before idx (discarded) so the overlap-add of
	// the first audible frame is complete.
	start := max(idx-1, 0)
	a.next = start
	pts := a.r.PTS(a.t, start)
	a.trim = int(math.Round((t - pts).Seconds() * float64(a.rate)))
	if a.trim < 0 {
		// Content starts after t (leading silence): pad instead.
		pad := -a.trim * 2 * a.chs
		a.pending = append(a.pending, make([]byte, pad)...)
		a.trim = 0
	}
}

func (a *golangAudio) poke() {
	select {
	case a.wake <- struct{}{}:
	default:
	}
}

// produce keeps the sink's buffer topped up.
func (a *golangAudio) produce() {
	defer close(a.done)
	const ahead = 300 * time.Millisecond
	idle := time.NewTimer(time.Hour)
	defer idle.Stop()
	for {
		a.mu.Lock()
		if a.closed {
			a.mu.Unlock()
			return
		}
		work := false
		if len(a.pending) > 0 {
			n, _ := a.sink.Write(a.pending)
			a.pending = a.pending[n:]
			work = n > 0
		}
		var decodeErr error
		if len(a.pending) == 0 && !a.eof && a.sink.Buffered() < ahead {
			decodeErr = a.decodeOneLocked()
			work = true
		}
		if a.eof && len(a.pending) == 0 && !a.drained && a.sink.Buffered() == 0 {
			a.drained = true
			a.drainPos = a.base + a.sink.HostTime()
			a.drainWall = time.Now()
			a.drainAcc = 0
		}
		// Sleep until the sink has drained to the refill threshold rather
		// than polling: each wakeup costs a goroutine handoff.
		wait := 10 * time.Millisecond
		if !a.eof && len(a.pending) == 0 {
			wait = min(max(a.sink.Buffered()-ahead, 10*time.Millisecond), 100*time.Millisecond)
		}
		var fire func()
		if a.eof && len(a.pending) == 0 && !a.ended && a.playing && a.sink.Buffered() == 0 &&
			a.base+a.sink.HostTime() >= a.info.Duration-50*time.Millisecond {
			a.ended = true
			fire = a.onEnded
		}
		var report func(error)
		if decodeErr != nil && !a.errReported {
			a.errReported = true
			report = a.onErr
		} else if decodeErr == nil {
			a.errReported = false
		}
		a.mu.Unlock()
		if fire != nil {
			fire()
		}
		if report != nil {
			report(decodeErr)
		}
		if !work {
			idle.Reset(wait)
			select {
			case <-a.wake:
			case <-idle.C: // Reset (Go 1.23+) never delivers a stale tick
			}
		}
	}
}

func (a *golangAudio) decodeOneLocked() error {
	n := a.t.SampleCount()
	if a.next >= n {
		a.eof = true
		return nil
	}
	b, err := a.r.ReadSampleInto(a.buf, a.t, a.next)
	a.next++
	if err != nil {
		// A truncated/unreadable sample ends the stream (unchanged).
		a.eof = true
		return nil
	}
	a.buf = b
	// Decode errors are concealed as silence; they are also reported to the
	// optional handler so callers can log or retry the file.
	var decErr error
	a.f32, decErr = a.dec.Decode(b, a.f32[:0])
	frames := len(a.f32) / a.chs
	skip := min(a.trim, frames)
	a.trim -= skip
	pcm := a.f32[skip*a.chs:]
	need := len(pcm) * 2
	if cap(a.pcmB) < need {
		a.pcmB = make([]byte, need)
	}
	out := a.pcmB[:need]
	for i, v := range pcm {
		x := math.Round(float64(v) * 32768)
		if x > 32767 {
			x = 32767
		} else if x < -32768 {
			x = -32768
		}
		binary.LittleEndian.PutUint16(out[2*i:], uint16(int16(x)))
	}
	a.pending = append(a.pending, out...)
	return decErr
}

func (a *golangAudio) Play() error {
	a.mu.Lock()
	if !a.playing {
		a.drainWall = time.Now()
	}
	a.playing = true
	a.mu.Unlock()
	a.poke()
	return a.sink.Play()
}

func (a *golangAudio) Pause() error {
	a.mu.Lock()
	if a.playing && a.drained {
		a.drainAcc += time.Since(a.drainWall)
	}
	a.playing = false
	a.mu.Unlock()
	return a.sink.Pause()
}

func (a *golangAudio) Stop() error {
	_ = a.Pause()
	return a.SeekTo(0)
}

func (a *golangAudio) SeekTo(t time.Duration) error {
	if t < 0 {
		t = 0
	}
	if d := a.info.Duration; d > 0 && t > d {
		t = d
	}
	a.mu.Lock()
	a.sink.Flush()
	a.seekLocked(t)
	a.mu.Unlock()
	a.poke()
	return nil
}

func (a *golangAudio) SetVolume(v float32) { a.sink.SetVolume(v) }
func (a *golangAudio) SetRate(r float32)   { a.sink.SetRate(r) }

func (a *golangAudio) HostTime() time.Duration {
	a.mu.Lock()
	base := a.base
	var virt time.Duration = -1
	if a.drained {
		virt = a.drainPos + a.drainAcc
		if a.playing {
			virt += time.Since(a.drainWall)
		}
	}
	a.mu.Unlock()
	t := base + a.sink.HostTime()
	return max(t, virt)
}

func (a *golangAudio) OnEnded(fn func()) {
	a.mu.Lock()
	a.onEnded = fn
	a.mu.Unlock()
}

// SetErrorHandler implements errorSink: the handler receives the first
// decode failure of a run (consecutive failures are coalesced).
func (a *golangAudio) SetErrorHandler(fn func(error)) {
	a.mu.Lock()
	a.onErr = fn
	a.mu.Unlock()
}

func (a *golangAudio) Close() error {
	a.mu.Lock()
	if a.closed {
		a.mu.Unlock()
		return nil
	}
	a.closed = true
	a.mu.Unlock()
	a.poke()
	<-a.done
	return a.sink.Close()
}
