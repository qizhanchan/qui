// Package media provides audio + video playback for qui without
// pulling in ffmpeg.
//
// # Backends
//
// All platform-specific demux / decode / audio output is hidden
// behind a single internal Backend interface (see backend.go). The
// macOS backend uses AVAssetReader (demux) + VTDecompressionSession
// (low-level video decode) + AVAudioEngine (audio output), composited
// onto qui's framebuffer through the GPUCanvas.QueueGLDraw +
// ActiveGLRenderer().DrawTexture path that scene3d.Viewport already
// uses. A Linux backend (NVDEC or similar) can be slotted in later
// by adding a backend_linux.go that registers itself in init();
// nothing in the public API changes.
//
// On platforms without a backend, every entry point returns
// ErrNotSupported. Code still compiles.
//
// Two public surfaces
//
//   - AudioPlayer — standalone service for playing audio-only files
//     (mp3, m4a, wav, aac). Not a widget; not attached to a Window.
//     Suitable for UI sound effects or background music.
//
//   - VideoView — a widget that plays mp4 / m4v / mov. Embeds
//     qui.BaseWidget and exposes frame-level controls
//     (StepForward / StepBackward / DecodeFrameAt / SetFrameSink) so
//     callers can build scrubbers, thumbnail strips, or custom
//     processing pipelines on top of it.
//
// # Threading
//
// Decode callbacks arrive on background dispatch queues. They never
// touch GL or widget state directly — all communication with the main
// goroutine happens through the frame queue in clock.go and a
// glfw.PostEmptyEvent wake-up. GL calls (texture allocation, texture
// cache create) run on the main goroutine inside Tick / Draw.
package media

import "errors"

// ErrNotSupported is returned by AudioPlayer and VideoView when no
// media backend is available for the current platform (e.g. on Linux
// in v1, or when the package was built with cgo disabled).
var ErrNotSupported = errors.New("media: not supported on this platform")

// ErrCodecUnsupported is returned by Open when the file's video or
// audio codec is recognized but the backend cannot decode it.
var ErrCodecUnsupported = errors.New("media: codec not supported by backend")

// ErrClosed is returned by methods on a player or view that has
// already been Closed / Destroyed.
var ErrClosed = errors.New("media: player closed")

// ErrNoTrack is returned when a track of the requested type does not
// exist in the file (e.g. asking VideoView about audio in a silent
// mp4).
var ErrNoTrack = errors.New("media: track not present")
