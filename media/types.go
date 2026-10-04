package media

import "time"

// State describes a player or view's playback state.
type State int

const (
	// StateIdle — no source open yet, or source was closed.
	StateIdle State = iota
	// StateLoading — Open in progress; tracks not yet enumerated.
	StateLoading
	// StatePlaying — clock is advancing.
	StatePlaying
	// StatePaused — clock is held; Play resumes from the same spot.
	StatePaused
	// StateEnded — playback reached the end of the source. Seek or
	// Play (which auto-rewinds when looping is on) returns to playing.
	StateEnded
	// StateError — irrecoverable error during open or decode. The
	// underlying object should be closed and re-opened.
	StateError
)

// String returns a short label suitable for logs / debug UI.
func (s State) String() string {
	switch s {
	case StateIdle:
		return "idle"
	case StateLoading:
		return "loading"
	case StatePlaying:
		return "playing"
	case StatePaused:
		return "paused"
	case StateEnded:
		return "ended"
	case StateError:
		return "error"
	default:
		return "unknown"
	}
}

// FitMode controls how VideoView places the decoded frame within
// the widget's bounds when their aspect ratios differ.
type FitMode int

const (
	// FitContain — letterbox / pillarbox. Whole frame is visible;
	// excess widget area is filled with the style background.
	FitContain FitMode = iota
	// FitCover — fill the widget completely; crop overflow on the
	// longer axis. Aspect preserved.
	FitCover
	// FitStretch — scale to widget bounds, ignoring aspect.
	FitStretch
)

// VideoTrackInfo describes the video stream inside a source.
type VideoTrackInfo struct {
	// Codec is a short identifier like "h264" or "hevc".
	Codec string
	// Width / Height are the encoded pixel dimensions before any
	// display rotation or pixel-aspect-ratio correction.
	Width  int
	Height int
	// FPS is the nominal frame rate reported by the container.
	// Variable-FPS sources report an average.
	FPS float32
	// Duration is the total stream length.
	Duration time.Duration
	// PixelFormat is the format the decoded VideoFrame's texture is
	// guaranteed to be in. v1 always reports "bgra".
	PixelFormat string
}

// AudioTrackInfo describes the audio stream inside a source.
type AudioTrackInfo struct {
	// Codec is a short identifier like "aac", "mp3", "lpcm".
	Codec string
	// SampleRate is samples per second per channel (e.g. 44100, 48000).
	SampleRate int
	// Channels — 1 = mono, 2 = stereo, etc.
	Channels int
	// Duration is the total stream length.
	Duration time.Duration
}

// VideoFrame is a single decoded frame ready for GL compositing.
// Frames come from a small pool inside the backend; callers must
// Release them when finished, or hold them past the next decode
// by calling Retain.
//
// The texture handle is valid only between Retain and the matching
// Release. After Release the frame may be recycled into another
// frame's storage.
type VideoFrame interface {
	// PTS is the frame's presentation timestamp inside the source.
	PTS() time.Duration
	// Width / Height are the pixel dimensions of the decoded frame
	// (which equal the encoded dimensions today; future support for
	// pixel-aspect-ratio correction would change this).
	Width() int
	Height() int
	// GLTexture returns a GL_TEXTURE_2D RGBA / BGRA texture ID
	// already uploaded to the GPU. Valid only while the frame is
	// retained.
	GLTexture() uint32
	// Retain increments the frame's reference count. Use when
	// holding a frame past the next decode call. Must be paired
	// with Release.
	Retain()
	// Release returns the frame to its pool. Use exactly once per
	// implicit reference (frames returned from CurrentFrame /
	// DecodeFrameAt come pre-retained), and once per explicit Retain.
	Release()
}
