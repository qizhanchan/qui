// Package audio is a low-latency software mixer for interactive audio such
// as games: any number of voices pull float32 stereo frames from a Source,
// get a gain and a constant-power pan, and are summed through a soft
// limiter into the platform output (CoreAudio on macOS).
//
//	m, err := audio.NewMixer(audio.Options{})
//	v := m.Play(audio.NewBuffer(samples, true))
//	v.SetGain(0.5)
//	defer m.Close()
//
// When no output device is available (CGO disabled, headless CI), NewMixer
// still succeeds in silent mode so callers need no special casing; Mix
// renders the same signal offline for tests.
package audio

import (
	"encoding/binary"
	"math"
	"sync"
	"sync/atomic"
	"time"

	"github.com/qizhanchan/qui/media/internal/audioout"
)

// Source produces interleaved stereo float32 frames. Read fills dst (an
// even number of samples) and returns how many samples it wrote; fewer
// than len(dst) ends the voice. Sources are called from the mixer
// goroutine only.
type Source interface {
	Read(dst []float32) int
}

// SourceFunc adapts a function to Source.
type SourceFunc func(dst []float32) int

func (f SourceFunc) Read(dst []float32) int { return f(dst) }

// Options configures a mixer. Zero values pick game-friendly defaults.
type Options struct {
	SampleRate int           // default 48000
	Latency    time.Duration // queued audio target, default 40 ms
}

// Mixer sums voices into one stereo stream.
type Mixer struct {
	rate    int
	latency time.Duration

	mu     sync.Mutex
	voices []*Voice
	master float32
	buf    []float32 // mix scratch
	tmp    []float32 // per-voice scratch

	sink   audioout.Sink
	done   chan struct{}
	closed atomic.Bool
	wg     sync.WaitGroup
}

// NewMixer opens the default output. Without a device the mixer runs
// silently (Silent reports true) and voices still advance in real time.
func NewMixer(opts Options) (*Mixer, error) {
	m := newMixer(opts)
	sink, err := audioout.Open(audioout.Format{SampleRate: m.rate, Channels: 2, BufferFrames: 512})
	if err == nil {
		m.sink = sink
		if err := sink.Play(); err != nil {
			sink.Close()
			m.sink = nil
		}
	}
	m.wg.Add(1)
	go m.run()
	return m, nil
}

// NewOfflineMixer is a mixer without an output thread; call Mix to render.
func NewOfflineMixer(opts Options) *Mixer { return newMixer(opts) }

func newMixer(opts Options) *Mixer {
	m := &Mixer{rate: opts.SampleRate, latency: opts.Latency, master: 1, done: make(chan struct{})}
	if m.rate <= 0 {
		m.rate = 48000
	}
	if m.latency <= 0 {
		m.latency = 40 * time.Millisecond
	}
	return m
}

// SampleRate is the output rate in Hz.
func (m *Mixer) SampleRate() int { return m.rate }

// Silent reports whether no output device is attached.
func (m *Mixer) Silent() bool { return m.sink == nil }

// SetMasterGain scales the whole mix (linear, clamped to [0, 4]).
func (m *Mixer) SetMasterGain(g float32) {
	m.mu.Lock()
	m.master = max(0, min(4, g))
	m.mu.Unlock()
}

// Play starts src as a new voice at unit gain, centred.
func (m *Mixer) Play(src Source) *Voice {
	v := &Voice{src: src}
	v.gain.Store(math.Float32bits(1))
	v.pan.Store(math.Float32bits(0))
	if m.closed.Load() {
		v.stopped.Store(true)
		return v
	}
	m.mu.Lock()
	m.voices = append(m.voices, v)
	m.mu.Unlock()
	return v
}

// Voices is the number of live voices.
func (m *Mixer) Voices() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.voices)
}

// Mix renders len(dst)/2 stereo frames, advancing every voice. The output
// mixer goroutine calls it; offline mixers call it directly.
func (m *Mixer) Mix(dst []float32) {
	dst = dst[:len(dst)&^1]
	clear(dst)
	m.mu.Lock()
	defer m.mu.Unlock()
	if cap(m.tmp) < len(dst) {
		m.tmp = make([]float32, len(dst))
	}
	tmp := m.tmp[:len(dst)]
	live := m.voices[:0]
	for _, v := range m.voices {
		if v.stopped.Load() {
			continue
		}
		clear(tmp)
		n := v.src.Read(tmp) &^ 1
		gain := math.Float32frombits(v.gain.Load())
		pan := math.Float32frombits(v.pan.Load())
		// Constant-power pan: −1 left … +1 right.
		angle := float64(pan+1) * math.Pi / 4
		l, r := gain*float32(math.Cos(angle))*math.Sqrt2, gain*float32(math.Sin(angle))*math.Sqrt2
		for i := 0; i < n; i += 2 {
			dst[i] += tmp[i] * l
			dst[i+1] += tmp[i+1] * r
		}
		if n < len(dst) {
			v.stopped.Store(true)
			continue
		}
		live = append(live, v)
	}
	clear(m.voices[len(live):])
	m.voices = live
	for i, s := range dst {
		dst[i] = softClip(s * m.master)
	}
}

// softClip is transparent below ±0.8 and saturates smoothly to ±1.
func softClip(x float32) float32 {
	const knee = 0.8
	switch {
	case x > knee:
		return knee + (1-knee)*float32(math.Tanh(float64((x-knee)/(1-knee))))
	case x < -knee:
		return -knee - (1-knee)*float32(math.Tanh(float64((-x-knee)/(1-knee))))
	}
	return x
}

func (m *Mixer) run() {
	defer m.wg.Done()
	const chunk = 256 // frames per write, ~5 ms at 48 kHz
	floats := make([]float32, 2*chunk)
	pcm := make([]byte, 4*chunk)
	tick := time.NewTicker(2 * time.Millisecond)
	defer tick.Stop()
	start := time.Now()
	var silentFrames int64
	for {
		select {
		case <-m.done:
			return
		case <-tick.C:
		}
		if m.sink == nil {
			// Keep voices advancing in real time without a device.
			due := int64(time.Since(start).Seconds() * float64(m.rate))
			for silentFrames+chunk <= due {
				m.Mix(floats)
				silentFrames += chunk
			}
			continue
		}
		for m.sink.Buffered() < m.latency {
			m.Mix(floats)
			for i, s := range floats {
				binary.LittleEndian.PutUint16(pcm[2*i:], uint16(int16(s*32767)))
			}
			if n, _ := m.sink.Write(pcm); n < len(pcm) {
				break
			}
		}
	}
}

// Close stops output and releases the device. Idempotent.
func (m *Mixer) Close() {
	if m.closed.Swap(true) {
		return
	}
	close(m.done)
	m.wg.Wait()
	if m.sink != nil {
		m.sink.Close()
	}
}

// Voice is one playing source.
type Voice struct {
	src     Source
	gain    atomic.Uint32
	pan     atomic.Uint32
	stopped atomic.Bool
}

// SetGain sets the linear gain (clamped to [0, 8]). Safe from any goroutine.
func (v *Voice) SetGain(g float32) { v.gain.Store(math.Float32bits(max(0, min(8, g)))) }

// SetPan sets the stereo position, −1 (left) … +1 (right).
func (v *Voice) SetPan(p float32) { v.pan.Store(math.Float32bits(max(-1, min(1, p)))) }

// Stop ends the voice at the next mix.
func (v *Voice) Stop() { v.stopped.Store(true) }

// Playing reports whether the voice is still mixed.
func (v *Voice) Playing() bool { return !v.stopped.Load() }

// Buffer plays a fixed interleaved stereo clip, optionally looping.
type Buffer struct {
	samples []float32
	pos     int
	loop    bool
}

// NewBuffer wraps interleaved stereo samples (not copied).
func NewBuffer(samples []float32, loop bool) *Buffer {
	return &Buffer{samples: samples[:len(samples)&^1], loop: loop}
}

func (b *Buffer) Read(dst []float32) int {
	n := 0
	for n < len(dst) {
		if b.pos >= len(b.samples) {
			if !b.loop || len(b.samples) == 0 {
				return n
			}
			b.pos = 0
		}
		c := copy(dst[n:], b.samples[b.pos:])
		n += c
		b.pos += c
	}
	return n
}
