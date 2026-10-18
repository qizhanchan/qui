// Package media provides audio + video playback for qui without
// pulling in ffmpeg.
//
// # Surfaces
//
//   - AudioPlayer — standalone service for audio-only files (mp3, m4a,
//     wav, aac). Not a widget; not attached to a Window. Suitable for UI
//     sound effects or background music.
//
//   - VideoView — a widget that opens mp4 / m4v / mov, demuxes and decodes
//     them itself, and exposes frame-level controls (StepForward /
//     StepBackward / DecodeFrameAt / SetFrameSink) so callers can build
//     scrubbers, thumbnail strips, or custom processing pipelines on top
//     of it.
//
//   - FrameView — a widget that displays frames pushed from outside — a
//     decoder on its own thread, a remote-desktop stream, a camera. Takes
//     CVPixelBuffers (macOS, zero copy) or RGBA images (portable), with a
//     one-slot latest-wins mailbox and GL-only redraws while frames
//     arrive.
//
// # Backends
//
// All platform-specific demux / decode / audio output is hidden behind a
// single internal Backend interface (see backend.go). The backend is chosen
// once per process from QUI_MEDIA_BACKEND:
//
//	native  — the platform stack: AVAssetReader (demux) +
//	          VTDecompressionSession (video) + AVAudioEngine (audio) on
//	          macOS. Registered as "native" by backend_darwin.go.
//	golang  — the cross-platform pure-Go pipeline (backend_golang.go): the
//	          self-written H.264 / HEVC / AV1 / AAC decoders, an ISO-BMFF
//	          demuxer (internal/mp4x) and GPU YUV→RGB, with only audio
//	          *output* behind a thin cgo seam (internal/audioout).
//	auto    — native when one is registered, otherwise golang (the default).
//
// An unrecognized value resolves to no backend and logs a warning listing
// the registered names; on a platform with no backend registered, every entry
// point returns ErrNotSupported. Call Available / Supported to check before
// trying to open a file. (Like the rest of qui, the package needs cgo for the
// GL bindings, so there is no cgo-less build.)
//
// # Threading and GL context
//
// Decode callbacks arrive on background threads. They never touch GL or
// widget state directly — decoded frames reach the UI through the pump
// (VideoView) or the frame mailbox (FrameView), and the main goroutine is
// woken with qui.WakeEventLoop. GL calls (texture allocation, upload,
// compositing) run on the UI thread inside Tick / Draw / retained GL draws.
//
// The GPU paths share process-wide GL programs, textures and (on macOS) the
// CVOpenGLTextureCache. All media widgets in a process must therefore render
// through the same GL context: windows opened with NewSharedWindow share
// their context; a second unshared window is not supported for video.
package media

import "errors"

// ErrNotSupported is returned by AudioPlayer and VideoView when no
// media backend is available: an unrecognized QUI_MEDIA_BACKEND, or a
// platform with no backend registered.
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
