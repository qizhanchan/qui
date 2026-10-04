//go:build darwin && cgo

package media

/*
#cgo CFLAGS: -x objective-c -fobjc-arc -DGL_SILENCE_DEPRECATION -DCOREVIDEO_SILENCE_GL_DEPRECATION
#cgo LDFLAGS: -framework Foundation -framework AVFoundation -framework AudioToolbox -framework CoreMedia -framework CoreVideo -framework VideoToolbox -framework OpenGL -framework IOSurface

#include <stdint.h>
#include <stdlib.h>

// AVAudioPlayer wrapper. Returns a non-zero handle on success, 0 on
// failure. errOut receives a strdup'd error string (caller frees) on
// failure, or NULL on success.
uintptr_t quiAudioOpen(const char* path, char** errOut);

// Track info. Duration is in seconds; rate sample rate is Hz; channels
// is 1 / 2 / etc. SampleRate / Channels may be 0 if unknown.
double  quiAudioDuration(uintptr_t handle);
int     quiAudioSampleRate(uintptr_t handle);
int     quiAudioChannels(uintptr_t handle);
int     quiAudioCodecID(uintptr_t handle);  // 0 = unknown, 1 = aac, 2 = mp3, 3 = lpcm/wav

// Transport. No-op on freed / invalid handles.
void    quiAudioPlay(uintptr_t handle);
void    quiAudioPause(uintptr_t handle);
void    quiAudioStop(uintptr_t handle);          // halts + rewinds to 0
void    quiAudioSeekTo(uintptr_t handle, double seconds);
double  quiAudioPosition(uintptr_t handle);      // current time in seconds
void    quiAudioSetVolume(uintptr_t handle, float v);  // 0..1
void    quiAudioSetRate(uintptr_t handle, float r);    // 1.0 = normal

void    quiAudioClose(uintptr_t handle);

// --- Video (Phase 2) ------------------------------------------------
//
// AVAssetReader-backed playback decoder. Returns 0 on failure with
// errOut populated. quiVideoStart kicks off the background decode
// loop; thereafter quiVideoDecodeOutput (defined in Go via //export)
// is called once per frame with the CVPixelBufferRef + PTS in ns.
// Open registers the Go-side handle BEFORE Start so race-free.

uintptr_t quiVideoOpen(const char* path, char** errOut);
int       quiVideoWidth(uintptr_t handle);
int       quiVideoHeight(uintptr_t handle);
double    quiVideoFPS(uintptr_t handle);
double    quiVideoDuration(uintptr_t handle);
int       quiVideoCodecID(uintptr_t handle);    // 1 h264, 2 hevc, 0 unknown
void      quiVideoStart(uintptr_t handle);
void      quiVideoSeek(uintptr_t handle, double seconds, int precise);
// Backpressure: Go calls this after consuming each frame from the
// pending channel so the ObjC decode loop can advance one slot.
void      quiVideoFrameConsumed(uintptr_t handle);
void      quiVideoClose(uintptr_t handle);

// Scrub session — independent AVAssetReader + texture cache so it
// can decode at arbitrary timestamps without disturbing playback.
uintptr_t quiVideoOpenScrub(uintptr_t srcHandle, char** errOut);
// Synchronously decodes the first frame whose PTS >= seconds. On
// success returns 1, writes a CFRetained CVPixelBufferRef into
// outPixBuf and the PTS (ns) into outPTSNs. Returns 0 on EOF /
// failure (outPixBuf left NULL).
int       quiVideoScrubDecodeAt(uintptr_t scrubHandle, double seconds,
                                void** outPixBuf, int64_t* outPTSNs);
void      quiVideoCloseScrub(uintptr_t scrubHandle);

// Main-thread (GL context current) helpers used inside QueueGLDraw.
// Lazily creates a process-wide CVOpenGLTextureCache on first call —
// the cache is tied to the GL context, and a qui app has exactly
// one context. Returns 1 on success: writes a retained
// CVOpenGLTextureRef into outCVTex and its GL texture name into
// outTexName.
int       quiVideoBindTexture(void* pixBuf, void** outCVTex, uint32_t* outTexName);
void      quiVideoCacheFlush(void);
void      quiVideoReleaseGLTex(void* cvTex);
void      quiVideoReleasePixBuf(void* pixBuf);

// --- Video-source audio (Phase 3) -----------------------------------
//
// AVAudioFile + AVAudioEngine + AVAudioPlayerNode driver for the
// audio track inside an mp4. Separate from the audio-only
// AVAudioPlayer path (mp3/m4a) because the same renderer needs to
// be the master clock for VideoView's A/V sync.

// quiVideoAudioOpen opens path's audio track. Returns 0 if the file
// has no audio track or audio cannot be decoded by AVAudioFile.
uintptr_t quiVideoAudioOpen(const char* path, char** errOut);
double    quiVideoAudioDuration(uintptr_t handle);
double    quiVideoAudioSampleRate(uintptr_t handle);
int       quiVideoAudioChannels(uintptr_t handle);

void      quiVideoAudioPlay(uintptr_t handle);
void      quiVideoAudioPause(uintptr_t handle);
void      quiVideoAudioStop(uintptr_t handle);
void      quiVideoAudioSeekTo(uintptr_t handle, double seconds);
double    quiVideoAudioHostTime(uintptr_t handle);
void      quiVideoAudioSetVolume(uintptr_t handle, float v);
void      quiVideoAudioSetRate(uintptr_t handle, float r);
void      quiVideoAudioClose(uintptr_t handle);
*/
import "C"

import (
	"context"
	"errors"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unsafe"

	"github.com/qizhanchan/qui"
)

// mediaDebug enables verbose tracing through the decode → bind →
// draw chain. Toggle via env: QUI_MEDIA_DEBUG=1. When off, the
// debug-log helpers compile to a single bool check + nothing else.
var mediaDebug = os.Getenv("QUI_MEDIA_DEBUG") != ""

// debugCounter samples logs so the 60Hz draw path doesn't drown the
// stderr stream. Returns true once per `every` invocations per tag.
var (
	debugCountMu sync.Mutex
	debugCount   = map[string]int{}
)

func debugSample(tag string, every int) bool {
	if !mediaDebug {
		return false
	}
	debugCountMu.Lock()
	defer debugCountMu.Unlock()
	debugCount[tag]++
	return debugCount[tag]%every == 0
}

func debugf(format string, args ...interface{}) {
	if !mediaDebug {
		return
	}
	log.Printf("[media] "+format, args...)
}

// init registers the darwin backend. Runs before any user code; safe
// to assume currentBackend is set by the time NewAudioPlayer is called.
func init() {
	setBackend(darwinBackend{})
	flushTextureCache = flushFrameCache
}

// ----- Handle registry --------------------------------------------------
//
// Cgo's pointer rules forbid storing Go pointers in C structures
// across calls, so we map integer handles to Go renderer pointers
// the same way ime_darwin.go maps NSView handles to *Window. The
// integer is what crosses the language boundary; the map indirection
// stays on the Go side.

var (
	audioRegistryMu sync.RWMutex
	audioRegistry   = map[uintptr]*darwinAudioRenderer{}
)

func registerAudioRenderer(r *darwinAudioRenderer, handle uintptr) {
	audioRegistryMu.Lock()
	audioRegistry[handle] = r
	audioRegistryMu.Unlock()
}

func lookupAudioRenderer(handle uintptr) *darwinAudioRenderer {
	audioRegistryMu.RLock()
	defer audioRegistryMu.RUnlock()
	return audioRegistry[handle]
}

func unregisterAudioRenderer(handle uintptr) {
	audioRegistryMu.Lock()
	delete(audioRegistry, handle)
	audioRegistryMu.Unlock()
}

// quiAudioDidEnd is invoked by the AVAudioPlayerDelegate when
// playback finishes naturally (not on Stop). Runs on the main thread
// because AVAudioPlayer dispatches delegate callbacks on the queue
// the player was created on — we create it on the main queue.
//
//export quiAudioDidEnd
func quiAudioDidEnd(handle C.uintptr_t) {
	r := lookupAudioRenderer(uintptr(handle))
	if r == nil {
		return
	}
	r.fireEnded()
}

// ----- Backend impl -----------------------------------------------------

type darwinBackend struct{}

func (darwinBackend) Capabilities() Capabilities {
	return Capabilities{
		// v1 supports the AVFoundation common decodable set for audio
		// and (in Phase 2) H.264 / HEVC for video.
		VideoCodecs: []string{"h264", "hevc"},
		AudioCodecs: []string{"aac", "mp3", "lpcm"},
		Containers:  []string{"mp4", "m4a", "mov", "mp3", "wav", "aac"},
	}
}

func (darwinBackend) OpenSource(path string) (Source, error) {
	if path == "" {
		return nil, errors.New("media: empty path")
	}

	// File-extension routing. Phase 2 only opens the video track for
	// mp4/mov/m4v; the audio track is ignored (rewired in Phase 3).
	// Other extensions go through the existing AVAudioPlayer path.
	switch strings.ToLower(filepath.Ext(path)) {
	case ".mp4", ".mov", ".m4v":
		return openDarwinVideoSource(path)
	}
	return openDarwinAudioSource(path)
}

func openDarwinAudioSource(path string) (Source, error) {
	cPath := C.CString(path)
	defer C.free(unsafe.Pointer(cPath))

	var cErr *C.char
	handle := C.quiAudioOpen(cPath, &cErr)
	if handle == 0 {
		var msg string
		if cErr != nil {
			msg = C.GoString(cErr)
			C.free(unsafe.Pointer(cErr))
		} else {
			msg = "open failed"
		}
		if strings.Contains(strings.ToLower(msg), "codec") {
			return nil, ErrCodecUnsupported
		}
		return nil, errors.New("media: " + msg)
	}

	rend := &darwinAudioRenderer{
		handle: uintptr(handle),
	}
	registerAudioRenderer(rend, uintptr(handle))

	track := &AudioTrackInfo{
		SampleRate: int(C.quiAudioSampleRate(handle)),
		Channels:   int(C.quiAudioChannels(handle)),
		Duration:   time.Duration(float64(C.quiAudioDuration(handle)) * float64(time.Second)),
		Codec:      codecIDToName(int(C.quiAudioCodecID(handle))),
	}
	rend.duration = track.Duration

	return &darwinSource{
		audio:    track,
		renderer: rend,
	}, nil
}

func openDarwinVideoSource(path string) (Source, error) {
	cPath := C.CString(path)
	defer C.free(unsafe.Pointer(cPath))

	var cErr *C.char
	handle := C.quiVideoOpen(cPath, &cErr)
	if handle == 0 {
		var msg string
		if cErr != nil {
			msg = C.GoString(cErr)
			C.free(unsafe.Pointer(cErr))
		} else {
			msg = "video open failed"
		}
		if strings.Contains(strings.ToLower(msg), "codec") {
			return nil, ErrCodecUnsupported
		}
		return nil, errors.New("media: " + msg)
	}

	vs := &darwinVideoSource{
		handle:  uintptr(handle),
		pending: make(chan pendingFrame, videoQueueCap),
		closeCh: make(chan struct{}),
	}
	vs.track = &VideoTrackInfo{
		Width:       int(C.quiVideoWidth(handle)),
		Height:      int(C.quiVideoHeight(handle)),
		FPS:         float32(C.quiVideoFPS(handle)),
		Duration:    time.Duration(float64(C.quiVideoDuration(handle)) * float64(time.Second)),
		Codec:       videoCodecIDToName(int(C.quiVideoCodecID(handle))),
		PixelFormat: "bgra",
	}
	registerVideoSource(vs, uintptr(handle))

	// Try to open the audio track via AVAudioFile + AVAudioEngine.
	// Failure (no audio track, unsupported codec) is non-fatal —
	// VideoView will fall back to wall-clock pacing when AudioRenderer
	// returns nil. We log so the user can tell why audio is missing.
	var cAudioErr *C.char
	audioHandle := C.quiVideoAudioOpen(cPath, &cAudioErr)
	if audioHandle == 0 {
		if cAudioErr != nil {
			debugf("video audio open skipped: %s", C.GoString(cAudioErr))
			C.free(unsafe.Pointer(cAudioErr))
		}
	} else {
		rend := &darwinVideoAudioRenderer{
			handle:   uintptr(audioHandle),
			duration: time.Duration(float64(C.quiVideoAudioDuration(audioHandle)) * float64(time.Second)),
		}
		registerVideoAudioRenderer(rend, uintptr(audioHandle))
		vs.audioRenderer = rend
		vs.audioTrack = &AudioTrackInfo{
			SampleRate: int(C.quiVideoAudioSampleRate(audioHandle)),
			Channels:   int(C.quiVideoAudioChannels(audioHandle)),
			Duration:   rend.duration,
			Codec:      "pcm", // AVAudioFile decodes to PCM regardless of source codec
		}
		debugf("video audio opened: %dHz x%d ch, %v",
			vs.audioTrack.SampleRate, vs.audioTrack.Channels, vs.audioTrack.Duration)
	}

	C.quiVideoStart(handle)
	return vs, nil
}

func codecIDToName(id int) string {
	switch id {
	case 1:
		return "aac"
	case 2:
		return "mp3"
	case 3:
		return "lpcm"
	default:
		return "unknown"
	}
}

// ----- Source impl ------------------------------------------------------

type darwinSource struct {
	audio    *AudioTrackInfo
	renderer *darwinAudioRenderer

	closeOnce sync.Once
	closeErr  error
}

func (s *darwinSource) VideoTrack() *VideoTrackInfo { return nil }
func (s *darwinSource) AudioTrack() *AudioTrackInfo { return s.audio }
func (s *darwinSource) AudioRenderer() AudioRenderer {
	if s.renderer == nil {
		return nil
	}
	return s.renderer
}

func (s *darwinSource) NextVideoFrame(ctx context.Context) (VideoFrame, error) {
	// Phase 1: audio-only; video pipeline arrives in Phase 2.
	return nil, ErrNoTrack
}

func (s *darwinSource) SeekVideo(time.Duration, bool) error {
	return ErrNoTrack
}

func (s *darwinSource) OpenScrubDecoder() (ScrubDecoder, error) {
	return nil, ErrNoTrack
}

func (s *darwinSource) Close() error {
	s.closeOnce.Do(func() {
		if s.renderer != nil {
			s.closeErr = s.renderer.Close()
		}
	})
	return s.closeErr
}

// ----- AudioRenderer impl -----------------------------------------------

type darwinAudioRenderer struct {
	handle   uintptr
	duration time.Duration

	mu      sync.Mutex
	onEnded func()
	closed  bool
}

func (r *darwinAudioRenderer) Play() error {
	if r.isClosed() {
		return ErrClosed
	}
	C.quiAudioPlay(C.uintptr_t(r.handle))
	return nil
}

func (r *darwinAudioRenderer) Pause() error {
	if r.isClosed() {
		return ErrClosed
	}
	C.quiAudioPause(C.uintptr_t(r.handle))
	return nil
}

func (r *darwinAudioRenderer) Stop() error {
	if r.isClosed() {
		return ErrClosed
	}
	C.quiAudioStop(C.uintptr_t(r.handle))
	return nil
}

func (r *darwinAudioRenderer) SeekTo(t time.Duration) error {
	if r.isClosed() {
		return ErrClosed
	}
	if t < 0 {
		t = 0
	}
	if t > r.duration && r.duration > 0 {
		t = r.duration
	}
	C.quiAudioSeekTo(C.uintptr_t(r.handle), C.double(t.Seconds()))
	return nil
}

func (r *darwinAudioRenderer) SetVolume(v float32) {
	if r.isClosed() {
		return
	}
	if v < 0 {
		v = 0
	}
	if v > 1 {
		v = 1
	}
	C.quiAudioSetVolume(C.uintptr_t(r.handle), C.float(v))
}

func (r *darwinAudioRenderer) SetRate(rate float32) {
	if r.isClosed() {
		return
	}
	if rate <= 0 {
		rate = 1
	}
	C.quiAudioSetRate(C.uintptr_t(r.handle), C.float(rate))
}

func (r *darwinAudioRenderer) HostTime() time.Duration {
	if r.isClosed() {
		return 0
	}
	seconds := float64(C.quiAudioPosition(C.uintptr_t(r.handle)))
	return time.Duration(seconds * float64(time.Second))
}

func (r *darwinAudioRenderer) OnEnded(fn func()) {
	r.mu.Lock()
	r.onEnded = fn
	r.mu.Unlock()
}

func (r *darwinAudioRenderer) Close() error {
	r.mu.Lock()
	if r.closed {
		r.mu.Unlock()
		return nil
	}
	r.closed = true
	r.mu.Unlock()
	C.quiAudioClose(C.uintptr_t(r.handle))
	unregisterAudioRenderer(r.handle)
	return nil
}

func (r *darwinAudioRenderer) fireEnded() {
	r.mu.Lock()
	fn := r.onEnded
	r.mu.Unlock()
	if fn != nil {
		fn()
	}
}

func (r *darwinAudioRenderer) isClosed() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.closed
}

// ----- Video source registry --------------------------------------------
//
// Mirrors the audio registry above. The integer handle threads the
// cgo boundary; the decode callback (quiVideoDecodeOutput) uses it to
// find the right pending channel without ever holding a Go pointer
// in C memory.

var (
	videoRegistryMu sync.RWMutex
	videoRegistry   = map[uintptr]*darwinVideoSource{}
)

func registerVideoSource(s *darwinVideoSource, handle uintptr) {
	videoRegistryMu.Lock()
	videoRegistry[handle] = s
	videoRegistryMu.Unlock()
}

func lookupVideoSource(handle uintptr) *darwinVideoSource {
	videoRegistryMu.RLock()
	defer videoRegistryMu.RUnlock()
	return videoRegistry[handle]
}

func unregisterVideoSource(handle uintptr) {
	videoRegistryMu.Lock()
	delete(videoRegistry, handle)
	videoRegistryMu.Unlock()
}

// videoQueueCap caps the pending-frame ring. 3 frames covers a
// ~50ms-ahead buffer at 60fps — enough for the main thread to absorb
// a single dropped Step without starving, without piling up
// IOSurface memory if the consumer falls behind.
const videoQueueCap = 3

type pendingFrame struct {
	pixBuf unsafe.Pointer // CVPixelBufferRef, pre-CFRetained on ObjC side
	pts    time.Duration
}

// quiVideoDecodeOutput is the //export callback the ObjC decode loop
// invokes once per decoded frame. Runs on a background dispatch
// queue — must not block on Go-side mutexes the main thread might
// hold, and must not touch widget / GL state.
//
// The ObjC side gates each push on a slot semaphore counted by
// quiVideoFrameConsumed, so the channel never overflows under
// normal operation. The default branch only fires after Close races
// with a stray decode callback; we CFRelease defensively to avoid
// leaking IOSurfaces in that window.
//
//export quiVideoDecodeOutput
func quiVideoDecodeOutput(handle C.uintptr_t, pixBuf unsafe.Pointer, ptsNanos C.int64_t) {
	s := lookupVideoSource(uintptr(handle))
	if s == nil {
		C.quiVideoReleasePixBuf(pixBuf)
		return
	}
	pf := pendingFrame{pixBuf: pixBuf, pts: time.Duration(ptsNanos)}
	if debugSample("decode_out", 30) {
		debugf("decode_out h=%d pts=%v pixBuf=%p", uintptr(handle), pf.pts, pixBuf)
	}
	select {
	case s.pending <- pf:
		// Wake the main loop so Window.Step runs even when there are no
		// input events. Goes through the engine rather than the windowing
		// library directly: a subpackage must not depend on which platform
		// backend is in use.
		qui.WakeEventLoop()
	default:
		// Should not happen with semaphore backpressure. Drop
		// defensively to keep IOSurfaces from leaking.
		debugf("decode_out DROP h=%d pts=%v (channel full)", uintptr(handle), pf.pts)
		C.quiVideoReleasePixBuf(pixBuf)
	}
}

func videoCodecIDToName(id int) string {
	switch id {
	case 1:
		return "h264"
	case 2:
		return "hevc"
	default:
		return "unknown"
	}
}

// ----- darwinVideoSource ------------------------------------------------

type darwinVideoSource struct {
	handle  uintptr
	track   *VideoTrackInfo
	pending chan pendingFrame
	closeCh chan struct{}

	scrubMu sync.Mutex
	scrub   *darwinScrubDecoder

	audioTrack    *AudioTrackInfo
	audioRenderer *darwinVideoAudioRenderer

	closed atomic.Bool
}

func (s *darwinVideoSource) VideoTrack() *VideoTrackInfo { return s.track }
func (s *darwinVideoSource) AudioTrack() *AudioTrackInfo { return s.audioTrack }
func (s *darwinVideoSource) AudioRenderer() AudioRenderer {
	if s.audioRenderer == nil {
		return nil
	}
	return s.audioRenderer
}

func (s *darwinVideoSource) NextVideoFrame(ctx context.Context) (VideoFrame, error) {
	if s.closed.Load() {
		return nil, io.EOF
	}
	select {
	case pf, ok := <-s.pending:
		if !ok {
			return nil, io.EOF
		}
		// Tell the decoder a slot opened up so it can produce
		// another frame. Without this the decode loop would either
		// run flat-out (memory waste) or stall after filling the cap.
		C.quiVideoFrameConsumed(C.uintptr_t(s.handle))
		return newDarwinVideoFrame(pf.pixBuf, pf.pts, s.track.Width, s.track.Height), nil
	case <-s.closeCh:
		return nil, io.EOF
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func (s *darwinVideoSource) SeekVideo(pts time.Duration, precise bool) error {
	if s.closed.Load() {
		return ErrClosed
	}
	if pts < 0 {
		pts = 0
	}
	cprec := C.int(0)
	if precise {
		cprec = 1
	}
	C.quiVideoSeek(C.uintptr_t(s.handle), C.double(pts.Seconds()), cprec)
	// Drain stale pre-seek frames so the next NextVideoFrame returns
	// a post-seek result. CFRelease each so we don't leak IOSurfaces.
	// Each drained slot must signal the decoder so its semaphore
	// stays in sync with the actual channel occupancy.
	for {
		select {
		case pf := <-s.pending:
			C.quiVideoReleasePixBuf(pf.pixBuf)
			C.quiVideoFrameConsumed(C.uintptr_t(s.handle))
		default:
			return nil
		}
	}
}

func (s *darwinVideoSource) OpenScrubDecoder() (ScrubDecoder, error) {
	if s.closed.Load() {
		return nil, ErrClosed
	}
	s.scrubMu.Lock()
	defer s.scrubMu.Unlock()
	if s.scrub != nil {
		return s.scrub, nil
	}
	var cErr *C.char
	h := C.quiVideoOpenScrub(C.uintptr_t(s.handle), &cErr)
	if h == 0 {
		var msg string
		if cErr != nil {
			msg = C.GoString(cErr)
			C.free(unsafe.Pointer(cErr))
		} else {
			msg = "open scrub failed"
		}
		return nil, errors.New("media: " + msg)
	}
	sd := &darwinScrubDecoder{
		handle: uintptr(h),
		width:  s.track.Width,
		height: s.track.Height,
	}
	s.scrub = sd
	return sd, nil
}

func (s *darwinVideoSource) Close() error {
	if !s.closed.CompareAndSwap(false, true) {
		return nil
	}
	close(s.closeCh)
	// Tear down the audio renderer first so it stops calling into
	// the asset that the video side is about to close.
	if s.audioRenderer != nil {
		_ = s.audioRenderer.Close()
		s.audioRenderer = nil
	}
	// Tear down the native side so no more callbacks fire.
	C.quiVideoClose(C.uintptr_t(s.handle))
	unregisterVideoSource(s.handle)
	// Drain whatever the decoder pushed before it stopped.
	for {
		select {
		case pf := <-s.pending:
			C.quiVideoReleasePixBuf(pf.pixBuf)
		default:
			goto closeScrub
		}
	}
closeScrub:
	s.scrubMu.Lock()
	scrub := s.scrub
	s.scrub = nil
	s.scrubMu.Unlock()
	if scrub != nil {
		_ = scrub.Close()
	}
	return nil
}

// bindFramePixelBuffer creates a GL_TEXTURE_2D view of a pixel
// buffer via the process-wide CVOpenGLTextureCache. MUST be called
// from the main thread (inside QueueGLDraw) with the GL context
// current. Caller owns the returned CVOpenGLTextureRef and must
// release it via quiVideoReleaseGLTex.
func bindFramePixelBuffer(pixBuf unsafe.Pointer) (cvTex unsafe.Pointer, name uint32, ok bool) {
	var outTex unsafe.Pointer
	var outName C.uint32_t
	r := C.quiVideoBindTexture(pixBuf, &outTex, &outName)
	if r == 0 {
		debugf("bindFramePixelBuffer FAILED pixBuf=%p", pixBuf)
		return nil, 0, false
	}
	if debugSample("bind_tex", 30) {
		debugf("bind_tex OK pixBuf=%p tex=%d cvTex=%p", pixBuf, uint32(outName), outTex)
	}
	return outTex, uint32(outName), true
}

func flushFrameCache() {
	C.quiVideoCacheFlush()
}

// ----- darwinVideoAudioRenderer (Phase 3) -------------------------------
//
// AudioRenderer driving an AVAudioEngine for the audio track inside
// an mp4. Owned by darwinVideoSource; only created when the source's
// AVAudioFile-open succeeds. VideoView swaps its wallClock for an
// audioClock wrapping this renderer so video promotion runs against
// audio host time (true A/V sync).

var (
	videoAudioRegistryMu sync.RWMutex
	videoAudioRegistry   = map[uintptr]*darwinVideoAudioRenderer{}
)

func registerVideoAudioRenderer(r *darwinVideoAudioRenderer, handle uintptr) {
	videoAudioRegistryMu.Lock()
	videoAudioRegistry[handle] = r
	videoAudioRegistryMu.Unlock()
}

func lookupVideoAudioRenderer(handle uintptr) *darwinVideoAudioRenderer {
	videoAudioRegistryMu.RLock()
	defer videoAudioRegistryMu.RUnlock()
	return videoAudioRegistry[handle]
}

func unregisterVideoAudioRenderer(handle uintptr) {
	videoAudioRegistryMu.Lock()
	delete(videoAudioRegistry, handle)
	videoAudioRegistryMu.Unlock()
}

//export quiVideoAudioDidEnd
func quiVideoAudioDidEnd(handle C.uintptr_t) {
	r := lookupVideoAudioRenderer(uintptr(handle))
	if r == nil {
		return
	}
	r.fireEnded()
}

type darwinVideoAudioRenderer struct {
	handle   uintptr
	duration time.Duration

	mu      sync.Mutex
	onEnded func()
	closed  bool
}

func (r *darwinVideoAudioRenderer) Play() error {
	if r.isClosed() {
		return ErrClosed
	}
	C.quiVideoAudioPlay(C.uintptr_t(r.handle))
	return nil
}

func (r *darwinVideoAudioRenderer) Pause() error {
	if r.isClosed() {
		return ErrClosed
	}
	C.quiVideoAudioPause(C.uintptr_t(r.handle))
	return nil
}

func (r *darwinVideoAudioRenderer) Stop() error {
	if r.isClosed() {
		return ErrClosed
	}
	C.quiVideoAudioStop(C.uintptr_t(r.handle))
	return nil
}

func (r *darwinVideoAudioRenderer) SeekTo(t time.Duration) error {
	if r.isClosed() {
		return ErrClosed
	}
	if t < 0 {
		t = 0
	}
	if r.duration > 0 && t > r.duration {
		t = r.duration
	}
	C.quiVideoAudioSeekTo(C.uintptr_t(r.handle), C.double(t.Seconds()))
	return nil
}

func (r *darwinVideoAudioRenderer) SetVolume(v float32) {
	if r.isClosed() {
		return
	}
	if v < 0 {
		v = 0
	}
	if v > 1 {
		v = 1
	}
	C.quiVideoAudioSetVolume(C.uintptr_t(r.handle), C.float(v))
}

func (r *darwinVideoAudioRenderer) SetRate(rate float32) {
	if r.isClosed() {
		return
	}
	if rate <= 0 {
		rate = 1
	}
	C.quiVideoAudioSetRate(C.uintptr_t(r.handle), C.float(rate))
}

func (r *darwinVideoAudioRenderer) HostTime() time.Duration {
	if r.isClosed() {
		return 0
	}
	seconds := float64(C.quiVideoAudioHostTime(C.uintptr_t(r.handle)))
	return time.Duration(seconds * float64(time.Second))
}

func (r *darwinVideoAudioRenderer) OnEnded(fn func()) {
	r.mu.Lock()
	r.onEnded = fn
	r.mu.Unlock()
}

func (r *darwinVideoAudioRenderer) Close() error {
	r.mu.Lock()
	if r.closed {
		r.mu.Unlock()
		return nil
	}
	r.closed = true
	r.mu.Unlock()
	C.quiVideoAudioClose(C.uintptr_t(r.handle))
	unregisterVideoAudioRenderer(r.handle)
	return nil
}

func (r *darwinVideoAudioRenderer) fireEnded() {
	r.mu.Lock()
	fn := r.onEnded
	r.mu.Unlock()
	if fn != nil {
		fn()
	}
}

func (r *darwinVideoAudioRenderer) isClosed() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.closed
}

// ----- darwinScrubDecoder ----------------------------------------------

type darwinScrubDecoder struct {
	handle uintptr
	width  int
	height int
	closed atomic.Bool
}

func (d *darwinScrubDecoder) DecodeAt(pts time.Duration) (VideoFrame, error) {
	if d.closed.Load() {
		return nil, ErrClosed
	}
	if pts < 0 {
		pts = 0
	}
	var pixBuf unsafe.Pointer
	var ptsOut C.int64_t
	ok := C.quiVideoScrubDecodeAt(C.uintptr_t(d.handle), C.double(pts.Seconds()), &pixBuf, &ptsOut)
	if ok == 0 {
		return nil, io.EOF
	}
	return newDarwinVideoFrame(pixBuf, time.Duration(ptsOut), d.width, d.height), nil
}

func (d *darwinScrubDecoder) Close() error {
	if !d.closed.CompareAndSwap(false, true) {
		return nil
	}
	C.quiVideoCloseScrub(C.uintptr_t(d.handle))
	return nil
}
