// Package audioout is the single cgo island of the media stack: a thin audio
// output sink over CoreAudio (macOS), WASAPI (Windows) or ALSA (Linux).
//
// Everything above it — decoders, the render pump, A/V sync — is pure Go. When
// built with CGO_ENABLED=0 (or on a platform without a sink) Open returns
// ErrNotSupported, so the rest of the stack stays buildable and testable.
package audioout

import (
	"errors"
	"time"
)

// ErrNotSupported is returned when no audio sink is available (CGO disabled,
// or an unsupported platform).
var ErrNotSupported = errors.New("audioout: not supported on this build/platform")

// Format describes the PCM stream fed to a Sink. Samples are interleaved
// 16-bit little-endian signed integers.
type Format struct {
	SampleRate int // e.g. 44100, 48000
	Channels   int // 1 = mono, 2 = stereo
}

// Sink is a running audio output. It owns its own playback thread; the methods
// below are control-plane only and are safe to call from any goroutine.
type Sink interface {
	// Write queues interleaved PCM for playback. It returns the number of
	// bytes accepted (which may be less than len(pcm) when the buffer is
	// full) and an error.
	Write(pcm []byte) (int, error)
	// Play starts (or resumes) playback. Idempotent.
	Play() error
	// Pause halts playback at the current position. Idempotent.
	Pause() error
	// SetVolume sets the linear gain in [0, 1]; values outside are clamped.
	SetVolume(v float32)
	// HostTime is the sink's authoritative playback position from the audio
	// clock. Used as the master clock for A/V sync.
	HostTime() time.Duration
	// Close stops playback and releases the device. Idempotent.
	Close() error
}

// Open returns a Sink for the given format, or ErrNotSupported when no sink is
// available. The concrete implementation is selected by build tags.
func Open(format Format) (Sink, error) { return open(format) }

// Available reports whether an audio sink can be opened on this build.
func Available() bool { return available() }
