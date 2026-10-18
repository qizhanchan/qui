package media

import (
	"context"
	"log"
	"os"
	"sort"
	"strings"
	"sync"
	"time"
)

// Backend abstracts the platform-specific demux + decode + audio
// output stack. The darwin backend is AVAssetReader +
// VTDecompressionSession + AVAudioEngine. A future Linux backend
// would plug in NVDEC + a Go-side MP4 demuxer + ALSA/PulseAudio
// behind this same interface.
//
// Backends are registered exactly once at process init via
// setBackend. There is no plugin / runtime swap mechanism — the
// build-tag-gated init() in backend_darwin.go (or _other.go) decides.
type Backend interface {
	// OpenSource opens path and enumerates its tracks. Returns
	// (nil, ErrCodecUnsupported) if neither video nor audio track is
	// decodable by this backend.
	OpenSource(path string) (Source, error)

	// Capabilities lets callers (and tests) check what's supported
	// without actually opening a file.
	Capabilities() Capabilities
}

// Capabilities lists what the active backend can decode.
type Capabilities struct {
	VideoCodecs []string // e.g. ["h264", "hevc"]
	AudioCodecs []string // e.g. ["aac", "mp3", "lpcm"]
	Containers  []string // e.g. ["mp4", "mov", "m4a", "mp3", "wav"]
}

// Source is an open media file. Exactly one of VideoTrack / AudioTrack
// may be nil (a silent video or an audio-only file); both nil is an
// error reported from OpenSource.
//
// Source ownership is single-threaded from the caller's perspective —
// AudioPlayer / VideoView take exclusive ownership and Close before
// dropping the reference.
type Source interface {
	// VideoTrack returns the video stream info, or nil if absent.
	VideoTrack() *VideoTrackInfo

	// AudioTrack returns the audio stream info, or nil if absent.
	AudioTrack() *AudioTrackInfo

	// NextVideoFrame blocks until the next decoded frame is ready,
	// or returns (nil, io.EOF) at end of stream, or an error.
	// The returned frame is pre-retained; caller must Release it.
	// Pass a cancelable context so the call unblocks on Close.
	NextVideoFrame(ctx context.Context) (VideoFrame, error)

	// SeekVideo positions the video decoder to the nearest IDR
	// before pts. When precise is false, frames resume from that
	// IDR (snap-to-keyframe — fast but inaccurate). When precise is
	// true, the backend decodes forward from the IDR and drops
	// frames whose PTS < pts, so the next NextVideoFrame returns a
	// frame at or after pts. Pre-seek frames already in-flight at
	// the caller's queue may still arrive; callers can drop them
	// by checking VideoFrame.PTS().
	SeekVideo(pts time.Duration, precise bool) error

	// OpenScrubDecoder returns a second, independent decode session
	// dedicated to synchronous frame fetches (DecodeFrameAt,
	// StepBackward thumbnails). Playback and scrub never interfere
	// because they run separate AVAssetReader / VTDecompressionSession
	// / texture-cache instances. Lazily allocated on first call;
	// subsequent calls return the same ScrubDecoder. Returns
	// ErrNoTrack if the source has no video.
	OpenScrubDecoder() (ScrubDecoder, error)

	// AudioRenderer returns the active audio output for this source.
	// Returns nil if AudioTrack() is nil.
	AudioRenderer() AudioRenderer

	// Close releases the underlying decoder + reader resources.
	// Safe to call from any goroutine; idempotent.
	Close() error
}

// ScrubDecoder is an isolated single-frame fetcher. Synchronous: each
// DecodeAt blocks the caller until a frame at-or-after the target
// timestamp is decoded. The returned VideoFrame is pre-Retained;
// callers must Release it. Closing the ScrubDecoder releases its own
// decoder + texture-cache; Source.Close also closes any active scrub
// decoder, so callers that pair OpenScrubDecoder with Source.Close
// do not need an explicit ScrubDecoder.Close.
type ScrubDecoder interface {
	DecodeAt(pts time.Duration) (VideoFrame, error)
	Close() error
}

// AudioRenderer plays back the source's audio track. The backend
// owns its own playback thread; the methods below are control-plane
// only. HostTime exposes the audio clock for A/V sync.
type AudioRenderer interface {
	// Play starts (or resumes) playback. Idempotent.
	Play() error
	// Pause halts playback at the current position. Idempotent.
	Pause() error
	// Stop halts playback and seeks to t=0.
	Stop() error
	// SeekTo jumps to t (clamped to [0, Duration]) and continues
	// playing if previously playing.
	SeekTo(t time.Duration) error
	// SetVolume sets the linear gain in [0, 1]. Values outside the
	// range are clamped.
	SetVolume(v float32)
	// SetRate sets the playback rate. 1.0 = normal speed. Time
	// stretching is done with pitch preservation if the backend
	// supports it.
	SetRate(r float32)
	// HostTime is the renderer's authoritative playback position
	// from the audio clock. Used by VideoView as the master clock
	// for A/V sync.
	HostTime() time.Duration
	// OnEnded registers a callback that fires once when the audio
	// stream ends naturally (not on Stop). Called on the audio
	// thread — implementations should hop to the main goroutine
	// before touching widget state.
	OnEnded(fn func())
	// Close releases backend resources. Idempotent.
	Close() error
}

// errorSink is an optional extension of AudioRenderer: a backend that can
// report per-frame decode failures while streaming implements it, and
// AudioPlayer wires the handler to its public OnError surface. Backends
// without it only report open-time failures.
type errorSink interface {
	SetErrorHandler(func(error))
}

// Backends are registered by name at process init via setBackend. The one
// that is actually used is chosen on first use from the QUI_MEDIA_BACKEND
// environment variable:
//
//	native  — the platform backend (AVFoundation on macOS)
//	golang  — the cross-platform pure-Go pipeline (backend_golang.go)
//	auto    — native when registered, otherwise golang (the default)
//
// Any other non-empty value is looked up as a backend name directly, so a
// future backend can be selected without changing this file.
var (
	backendRegistry = map[string]Backend{}
	backendOnce     sync.Once
	backendChosen   Backend
)

// setBackend is the entry point platform files call from init().
// Unexported on purpose — callers select via QUI_MEDIA_BACKEND, not by
// swapping the pointer.
func setBackend(name string, b Backend) { backendRegistry[name] = b }

// backendOverride / backendHasOverride force the active backend. Installed
// by tests so they never have to mutate the process environment; a nil
// override with backendHasOverride set means "no backend".
var (
	backendOverride    Backend
	backendHasOverride bool
)

// overrideBackendForTest forces b as the active backend (nil = none) and
// returns a restore function. Test-only.
func overrideBackendForTest(b Backend) func() {
	prevB, prevH := backendOverride, backendHasOverride
	backendOverride, backendHasOverride = b, true
	return func() { backendOverride, backendHasOverride = prevB, prevH }
}

// backendWarnf reports a misconfigured QUI_MEDIA_BACKEND; a variable so
// tests can capture the diagnostic without touching the process logger.
var backendWarnf = log.Printf

// backendNames lists the registered backend names, sorted for a stable
// diagnostic.
func backendNames() []string {
	names := make([]string, 0, len(backendRegistry))
	for name := range backendRegistry {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// resolveBackend picks the active backend once, from QUI_MEDIA_BACKEND.
func resolveBackend() Backend {
	if backendHasOverride {
		return backendOverride
	}
	backendOnce.Do(func() {
		switch want := strings.TrimSpace(os.Getenv("QUI_MEDIA_BACKEND")); want {
		case "", "auto":
			if b := backendRegistry["native"]; b != nil {
				backendChosen = b
			} else {
				backendChosen = backendRegistry["golang"]
			}
		default:
			backendChosen = backendRegistry[want]
			if backendChosen == nil {
				// A typo must not look like "media is unsupported on this
				// platform": say which value matched nothing.
				backendWarnf("media: QUI_MEDIA_BACKEND=%q matches no registered backend (have: %s); media is disabled",
					want, strings.Join(backendNames(), ", "))
			}
		}
	})
	return backendChosen
}

// resetBackendForTest clears the one-shot selection so a test can change
// QUI_MEDIA_BACKEND and re-resolve.
func resetBackendForTest() {
	backendOnce = sync.Once{}
	backendChosen = nil
}

// activeBackend returns the selected backend or nil. Public package code
// consults this before calling backend methods so it can return
// ErrNotSupported gracefully.
func activeBackend() Backend { return resolveBackend() }

// Available reports whether a media backend is active for this build and
// platform. When false, NewAudioPlayer returns ErrNotSupported and
// VideoView.Open fails with it.
func Available() bool { return activeBackend() != nil }

// Supported reports what the active backend can decode. It returns the
// zero value when no backend is available (see Available).
func Supported() Capabilities {
	if b := activeBackend(); b != nil {
		return b.Capabilities()
	}
	return Capabilities{}
}
