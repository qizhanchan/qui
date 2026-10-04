package media

import (
	"sync"
	"time"
)

// AudioPlayer plays an audio-only file (mp3, m4a, wav, aac). It is
// not a widget and does not need a Window — useful for UI sound
// effects, alert sounds, or background music. For video with audio,
// use VideoView instead; it manages its own audio internally.
//
// Lifecycle:
//
//	p, err := media.NewAudioPlayer()
//	if err != nil { ... }                 // returns ErrNotSupported on platforms without a backend
//	defer p.Close()
//
//	if err := p.Open("/path/to/song.mp3"); err != nil { ... }
//	p.SetVolume(0.7)
//	p.OnEnded(func() { fmt.Println("done") })
//	_ = p.Play()
//	...
//	_ = p.Pause()
//
// AudioPlayer is safe to call from any goroutine — methods serialize
// internally on a mutex. OnEnded fires on a background audio thread;
// callers that need to update widget state should hop to the main
// goroutine themselves (e.g. via Window.RunOnMain if available, or
// by setting a flag the next Tick will observe).
type AudioPlayer struct {
	mu       sync.Mutex
	src      Source
	renderer AudioRenderer
	state    State
	volume   float32
	rate     float32
	loop     bool

	onEnded func()
	onError func(error)
}

// NewAudioPlayer constructs an empty AudioPlayer. Returns
// ErrNotSupported on platforms without a media backend.
//
// The returned player has no source open yet — call Open to load
// a file before Play. Volume defaults to 1.0 and rate to 1.0.
func NewAudioPlayer() (*AudioPlayer, error) {
	if activeBackend() == nil {
		return nil, ErrNotSupported
	}
	return &AudioPlayer{
		state:  StateIdle,
		volume: 1,
		rate:   1,
	}, nil
}

// Open closes any previously open source and loads path. The file's
// audio track is enumerated synchronously; playback does not start.
// Returns ErrCodecUnsupported if the file's audio codec cannot be
// decoded, or ErrNoTrack if the file has no audio.
func (p *AudioPlayer) Open(path string) error {
	if p == nil {
		return ErrClosed
	}
	be := activeBackend()
	if be == nil {
		return ErrNotSupported
	}

	src, err := be.OpenSource(path)
	if err != nil {
		p.setState(StateError)
		p.fireError(err)
		return err
	}
	if src.AudioTrack() == nil {
		_ = src.Close()
		return ErrNoTrack
	}
	renderer := src.AudioRenderer()
	if renderer == nil {
		_ = src.Close()
		return ErrNoTrack
	}

	p.mu.Lock()
	prevSrc := p.src
	p.src = src
	p.renderer = renderer
	p.state = StatePaused
	vol, rate := p.volume, p.rate
	onEnded := p.onEnded
	p.mu.Unlock()

	renderer.SetVolume(vol)
	renderer.SetRate(rate)
	if onEnded != nil {
		renderer.OnEnded(p.handleEnded)
	} else {
		renderer.OnEnded(p.handleEnded)
	}

	if prevSrc != nil {
		_ = prevSrc.Close()
	}
	return nil
}

// Play starts (or resumes) playback. Returns ErrClosed if Close was
// called, or ErrNoTrack if no source is open.
func (p *AudioPlayer) Play() error {
	p.mu.Lock()
	r := p.renderer
	if r == nil {
		p.mu.Unlock()
		return ErrNoTrack
	}
	p.state = StatePlaying
	p.mu.Unlock()
	return r.Play()
}

// Pause holds playback at the current position. Play resumes from
// the same spot.
func (p *AudioPlayer) Pause() error {
	p.mu.Lock()
	r := p.renderer
	if r == nil {
		p.mu.Unlock()
		return ErrNoTrack
	}
	p.state = StatePaused
	p.mu.Unlock()
	return r.Pause()
}

// Stop halts playback and rewinds to t=0.
func (p *AudioPlayer) Stop() error {
	p.mu.Lock()
	r := p.renderer
	if r == nil {
		p.mu.Unlock()
		return ErrNoTrack
	}
	p.state = StatePaused
	p.mu.Unlock()
	return r.Stop()
}

// SeekTo jumps to t (clamped to [0, Duration]).
func (p *AudioPlayer) SeekTo(t time.Duration) error {
	p.mu.Lock()
	r := p.renderer
	p.mu.Unlock()
	if r == nil {
		return ErrNoTrack
	}
	if t < 0 {
		t = 0
	}
	return r.SeekTo(t)
}

// Duration returns the source's total length, or 0 if no source is
// open.
func (p *AudioPlayer) Duration() time.Duration {
	p.mu.Lock()
	src := p.src
	p.mu.Unlock()
	if src == nil || src.AudioTrack() == nil {
		return 0
	}
	return src.AudioTrack().Duration
}

// Position returns the current playback position. Reads the audio
// clock directly from the backend.
func (p *AudioPlayer) Position() time.Duration {
	p.mu.Lock()
	r := p.renderer
	p.mu.Unlock()
	if r == nil {
		return 0
	}
	return r.HostTime()
}

// SetVolume sets linear gain in [0, 1].
func (p *AudioPlayer) SetVolume(v float32) {
	if v < 0 {
		v = 0
	}
	if v > 1 {
		v = 1
	}
	p.mu.Lock()
	p.volume = v
	r := p.renderer
	p.mu.Unlock()
	if r != nil {
		r.SetVolume(v)
	}
}

// SetRate sets playback speed. 1.0 = normal.
func (p *AudioPlayer) SetRate(r float32) {
	if r <= 0 {
		r = 1
	}
	p.mu.Lock()
	p.rate = r
	ar := p.renderer
	p.mu.Unlock()
	if ar != nil {
		ar.SetRate(r)
	}
}

// SetLoop toggles automatic restart on end-of-stream. Off by default.
// Loop fires the OnEnded callback every cycle.
func (p *AudioPlayer) SetLoop(b bool) {
	p.mu.Lock()
	p.loop = b
	p.mu.Unlock()
}

// State returns the player's current state.
func (p *AudioPlayer) State() State {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.state
}

// OnEnded registers a callback to fire when the audio reaches the
// natural end of the stream. Replaces any previous registration.
// Called on a background audio thread.
func (p *AudioPlayer) OnEnded(fn func()) {
	p.mu.Lock()
	p.onEnded = fn
	r := p.renderer
	p.mu.Unlock()
	if r != nil {
		r.OnEnded(p.handleEnded)
	}
}

// OnError registers a callback for asynchronous errors (decode
// failures during streaming). Replaces any previous registration.
func (p *AudioPlayer) OnError(fn func(error)) {
	p.mu.Lock()
	p.onError = fn
	p.mu.Unlock()
}

// Close releases backend resources. Idempotent. After Close, all
// other methods return ErrClosed / ErrNoTrack.
func (p *AudioPlayer) Close() error {
	p.mu.Lock()
	src := p.src
	p.src = nil
	p.renderer = nil
	p.state = StateIdle
	p.mu.Unlock()
	if src != nil {
		return src.Close()
	}
	return nil
}

// handleEnded is the audio-thread callback that drives Loop semantics
// and dispatches to the user-installed OnEnded.
func (p *AudioPlayer) handleEnded() {
	p.mu.Lock()
	loop := p.loop
	fn := p.onEnded
	r := p.renderer
	if !loop {
		p.state = StateEnded
	}
	p.mu.Unlock()

	if loop && r != nil {
		// Restart from the top. SeekTo + Play is the simplest
		// path; AVFoundation reschedules the buffer cleanly.
		_ = r.SeekTo(0)
		_ = r.Play()
	}
	if fn != nil {
		fn()
	}
}

func (p *AudioPlayer) setState(s State) {
	p.mu.Lock()
	p.state = s
	p.mu.Unlock()
}

func (p *AudioPlayer) fireError(err error) {
	p.mu.Lock()
	fn := p.onError
	p.mu.Unlock()
	if fn != nil {
		fn(err)
	}
}
