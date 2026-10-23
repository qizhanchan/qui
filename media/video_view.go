package media

import (
	"context"
	"errors"
	"io"
	"log"
	"os"
	"sync"
	"sync/atomic"
	"time"

	"github.com/qizhanchan/qui"
)

// VideoView-side debug flag (independent of backend_darwin.go's;
// kept here so video_view.go compiles on non-darwin too). Enable
// via QUI_MEDIA_DEBUG=1 to trace the pump → promote → draw chain.
var vvDebug = os.Getenv("QUI_MEDIA_DEBUG") != ""

func vvDebugf(format string, args ...interface{}) {
	if vvDebug {
		log.Printf("[media] "+format, args...)
	}
}

var (
	vvCountMu sync.Mutex
	vvCount   = map[string]int{}
)

func vvSample(tag string, every int) bool {
	if !vvDebug {
		return false
	}
	vvCountMu.Lock()
	defer vvCountMu.Unlock()
	vvCount[tag]++
	return vvCount[tag]%every == 0
}

// framebufferDrawer is implemented by platform-specific VideoFrame
// types that own their full GL draw call. The darwin backend
// implements this so it can sample GL_TEXTURE_RECTANGLE (what
// CVOpenGLTextureCache hands back for IOSurface-backed buffers),
// which GLRenderer.DrawTexture's sampler2D shader cannot read.
//
// DrawIntoFramebuffer is called inside a GPUCanvas.QueueGLDraw
// closure with the GL context current. It returns false if it
// couldn't bind or draw the frame (e.g. texture cache create
// failed), letting the caller log the miss.
type framebufferDrawer interface {
	DrawIntoFramebuffer(state qui.GLState, src, physDst qui.Rect, flipY bool) bool
}

// cacheFlusher is invoked after each frame draw on platforms that
// keep an internal texture cache and need a periodic flush to
// reclaim IOSurface entries. The hook is platform-injected via
// flushTextureCache; unused on platforms without a cache.
var flushTextureCache func()

// VideoView is the video-playback widget. Embeds qui.BaseWidget and
// composites decoded frames onto the framebuffer via the same
// GPUCanvas.QueueGLDraw + ActiveGLRenderer().DrawTexture path that
// scene3d.Viewport uses.
//
// Lifecycle:
//
//	v := media.NewVideoView()
//	if err := v.Open("/path/to/video.mp4"); err != nil { ... }
//	v.Play()
//	defer v.Destroy()
//
// Threading: Open / Play / Pause / Stop / Seek and the frame-level
// API are safe to call from any goroutine — methods take an internal
// mutex. Draw and Tick are called by the framework on the main
// goroutine. The pump goroutine runs in the background, decoding
// frames into a channel that Tick drains.
type VideoView struct {
	qui.BaseWidget

	mu        sync.Mutex
	src       Source
	track     *VideoTrackInfo
	clock     Clock
	state     State
	lastErr   error // terminal failure that put us in StateError
	destroyed bool
	fitMode   FitMode
	loop      bool
	rate      float32
	volume    float32

	pumpCancel context.CancelFunc
	pumpDone   chan struct{}

	// pumpFrame is a single-slot rendezvous between the pump
	// goroutine and Tick. Pump fills it (waiting on pumpReady when
	// full); Tick promotes-or-drops and signals pumpReady to let
	// pump produce the next one. Guarded by pumpMu, not v.mu, so
	// the locking discipline stays simple — never take pumpMu while
	// holding v.mu (the only correct order is pumpMu first if both
	// are needed, but in practice they're disjoint).
	pumpMu    sync.Mutex
	pumpReady *sync.Cond // signaled by Tick when pumpFrame becomes nil
	pumpFrame VideoFrame

	// pumpWake wakes a pump parked at end of stream so it retries the
	// source after a seek. The pure-Go backend reports io.EOF once its ring
	// closes (the darwin reader blocks inside NextVideoFrame instead), so
	// without this the pump goroutine would finish at the end of the file
	// and replay / loop / seek-after-end would decode into a ring nobody
	// drains. invalidatePump signals it on every seek; capacity 1 coalesces
	// a seek's two bumps.
	pumpWake chan struct{}

	// resumeSeek / resumePTS record that a frame step moved the clock and
	// the displayed picture through the scrub session, leaving the playback
	// decoder at its old position. While resumeSeek is set, Tick must not
	// promote pump frames: they belong to the pre-step position and would
	// overwrite the stepped picture (most visibly after several forward
	// steps, which outrun the pump). Play repositions the decoder with one
	// precise seek before resuming, and every explicit seek clears the flag.
	// Guarded by mu.
	resumeSeek bool
	resumePTS  time.Duration

	// seekGen is bumped on every seek (SeekTo / Stop / loop-restart /
	// replay-from-end). The pump goroutine snapshots it before each
	// NextVideoFrame call and re-checks at park time; a mismatch
	// means a seek happened during the receive-then-park window, so
	// the frame may be pre-seek (popped from s.pending before the
	// backend drain) and is discarded.
	//
	// Without this, a pre-seek frame whose PTS is in the past
	// relative to the new clock would never promote — backward seeks
	// would stall the video pipeline for as long as it takes the
	// clock to catch up to the stale frame's PTS (effectively
	// forever for a backward seek of more than a few frames).
	seekGen atomic.Uint64

	currentFrame VideoFrame
	frameSink    func(VideoFrame)

	// retained is set once Draw has registered a retained GL draw (see
	// qui.RetainedGLCanvas). From then on a new frame costs a GL-only
	// redraw and the picture survives repaints of the rest of the window
	// without the view repainting itself. glDst is the logical
	// destination rect that draw uses. UI thread.
	retained bool
	glSrc    qui.Rect
	glDst    qui.Rect

	scrub ScrubDecoder // lazy

	onReady      func()
	onTimeUpdate func(time.Duration)
	onEnded      func()
	onError      func(error)
}

// NewVideoView constructs an empty VideoView. Open a file before
// Play. Volume defaults to 1.0, rate to 1.0, FitMode to FitContain.
// Calls SetSelf so framework walks see the concrete type — required
// for any BaseWidget subclass.
func NewVideoView() *VideoView {
	v := &VideoView{
		BaseWidget: qui.NewBaseWidget(),
		clock:      newWallClock(),
		fitMode:    FitContain,
		rate:       1,
		volume:     1,
		state:      StateIdle,
	}
	v.pumpReady = sync.NewCond(&v.pumpMu)
	v.pumpWake = make(chan struct{}, 1)
	v.SetSelf(v)
	return v
}

// drainPumpFrame discards any parked frame and signals the pump so
// it can produce a fresh post-seek (or post-stop) frame. Caller
// MUST NOT hold v.mu — drainPumpFrame takes pumpMu only.
func (v *VideoView) drainPumpFrame() {
	v.pumpMu.Lock()
	pf := v.pumpFrame
	v.pumpFrame = nil
	v.pumpReady.Signal()
	v.pumpMu.Unlock()
	if pf != nil {
		pf.Release()
	}
}

// invalidatePump prepares the pump pipeline for a seek: bumps seekGen
// FIRST so the pump's in-flight frames (already popped from s.pending
// but not yet parked) fail their park-time gen check, then drains any
// frame already parked in pumpFrame. Callers should use seekAndInvalidate
// rather than calling this directly — a single invalidatePump only
// covers the pre-seek window.
//
// Order matters: bump must precede drain so a frame parked between
// our drain and the pump's next iteration is still caught (the pump
// will see the new gen and discard before parking).
func (v *VideoView) invalidatePump() {
	v.seekGen.Add(1)
	// Wake a pump parked at end of stream: the seek that follows may have
	// restarted the decoder (see pumpLoop).
	select {
	case v.pumpWake <- struct{}{}:
	default:
	}
	v.drainPumpFrame()
}

// seekAndInvalidate wraps src.SeekVideo with invalidatePump on BOTH
// sides. The pre-seek pass discards anything already in the pump
// pipeline; the post-seek pass closes the window between the first
// gen bump and the backend's seekDone fence — during that ~500ms the C
// decoder is still pushing pre-seek frames into s.pending and the
// pump can pop one and park it with the post-bump gen, so the park
// check passes but the parked frame is stale.
//
// On backward seeks that stale frame's PTS sits past the new clock,
// Tick never promotes it, and the pump goroutine starves (channel
// fills, decode_out DROPs every new post-seek frame). The second
// invalidatePump drops the stale frame and bumps seekGen again so any
// frame the pump is mid-park-checking discards itself; the next pump
// iteration captures the new gen and parks the first true post-seek
// frame.
//
// Cost: at most one post-seek frame is also dropped (whichever the
// pump happened to be processing when we ran the second pass). That
// frame's replacement is along right behind it.
//
// Caller must hold no lock.
func (v *VideoView) seekAndInvalidate(src Source, t time.Duration) error {
	v.invalidatePump()
	err := src.SeekVideo(t, true)
	v.invalidatePump()
	return err
}

// Open closes any previously open source and loads path. The video
// track is enumerated synchronously; playback does not start. Returns
// ErrCodecUnsupported if the file's video codec cannot be decoded,
// ErrNoTrack if there's no video track, or ErrNotSupported on
// platforms without a backend.
func (v *VideoView) Open(path string) error {
	v.mu.Lock()
	destroyed := v.destroyed
	v.mu.Unlock()
	if destroyed {
		return ErrClosed
	}
	be := activeBackend()
	if be == nil {
		return ErrNotSupported
	}
	src, err := be.OpenSource(path)
	if err != nil {
		v.fail(err)
		return err
	}
	if src.VideoTrack() == nil {
		_ = src.Close()
		return ErrNoTrack
	}

	v.mu.Lock()
	if v.destroyed {
		// Destroy raced the open: don't install a source (and a pump) into
		// a dead view.
		v.mu.Unlock()
		_ = src.Close()
		return ErrClosed
	}
	prevSrc := v.src
	prevCancel := v.pumpCancel
	prevDone := v.pumpDone
	prevScrub := v.scrub
	prevFrame := v.currentFrame

	v.src = src
	v.track = src.VideoTrack()
	v.state = StatePaused
	// The scrub session and the displayed frame belong to the source being
	// replaced. Dropping the references here is what makes a second Open
	// decode from the NEW file (a stale scrub would answer DecodeFrameAt /
	// Step* from the closed source) and stops the old picture surviving
	// into the new one.
	v.scrub = nil
	v.currentFrame = nil
	v.resumeSeek = false
	v.lastErr = nil
	// Clock selection: if the source has an audio renderer, drive
	// playback against audio host time (true A/V sync). Otherwise
	// fall back to wall-clock pacing. Either way, apply the user's
	// previously-set volume / rate / loop / onEnded to the new
	// renderer so they survive re-Open.
	if ar := src.AudioRenderer(); ar != nil {
		v.clock = newAudioClock(ar)
		ar.SetVolume(v.volume)
		ar.SetRate(v.rate)
		// End-of-stream detection lives in Tick (clockNow ≥ Duration);
		// having the renderer also fire OnEnded would double-trigger
		// loops and user callbacks. Tick is the single source of truth.
	} else {
		v.clock = newWallClock()
	}
	v.clock.SetRate(v.rate)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	v.pumpCancel = cancel
	v.pumpDone = done
	cb := v.onReady
	v.mu.Unlock()

	// Stop the previous pump, close the previous source. Order: cancel
	// pump's ctx, broadcast pumpReady so a Wait-ing pump observes the
	// cancellation, await pumpDone, then close the source which drains
	// its backend channel.
	if prevCancel != nil {
		prevCancel()
		v.pumpMu.Lock()
		v.pumpReady.Broadcast()
		pf := v.pumpFrame
		v.pumpFrame = nil
		v.pumpMu.Unlock()
		if pf != nil {
			pf.Release()
		}
		<-prevDone
	}
	if prevSrc != nil {
		_ = prevSrc.Close()
	}
	// prevSrc.Close also releases any scrub decoder it owns; the explicit
	// Close is a cheap idempotent guard for backends that leave that to the
	// caller. The old frame's own reference is independent of the source.
	if prevScrub != nil {
		_ = prevScrub.Close()
	}
	if prevFrame != nil {
		prevFrame.Release()
	}

	go v.pumpLoop(ctx, src, done)

	if cb != nil {
		cb()
	}
	return nil
}

// Play starts (or resumes) playback. Returns ErrNoTrack if no source
// is open, or the terminal error recorded by OnError once the view is in
// StateError (only Open leaves that state). Decoder is always running
// ahead; only the clock pauses.
func (v *VideoView) Play() error {
	v.mu.Lock()
	if v.destroyed {
		v.mu.Unlock()
		return ErrClosed
	}
	if v.src == nil {
		v.mu.Unlock()
		return ErrNoTrack
	}
	if err := v.terminalErrLocked(); err != nil {
		v.mu.Unlock()
		return err
	}
	// If we ended naturally and Play is called, restart from 0. A frame
	// step also needs the decoder moved to the stepped position (the step
	// itself only ran the scrub session). Hoist the seek out of v.mu so
	// invalidatePump (which takes pumpMu) and SeekVideo (which may block
	// briefly on the C decoder's seekDone semaphore) don't compose locks
	// with v.mu.
	replay := v.state == StateEnded
	src := v.src
	seekAt, doSeek := time.Duration(0), false
	switch {
	case replay:
		v.clock.SeekTo(0)
		seekAt, doSeek = 0, true
		if v.currentFrame != nil {
			v.currentFrame.Release()
			v.currentFrame = nil
		}
	case v.resumeSeek:
		seekAt, doSeek = v.resumePTS, true
	}
	v.resumeSeek = false
	v.state = StatePlaying
	v.clock.Play()
	v.mu.Unlock()
	if doSeek && src != nil {
		_ = v.seekAndInvalidate(src, seekAt)
	}
	return nil
}

// Pause holds playback at the current position. Play resumes from
// the same spot.
func (v *VideoView) Pause() error {
	v.mu.Lock()
	if v.destroyed {
		v.mu.Unlock()
		return ErrClosed
	}
	if v.src == nil {
		v.mu.Unlock()
		return ErrNoTrack
	}
	v.state = StatePaused
	v.clock.Pause()
	v.mu.Unlock()
	return nil
}

// Stop halts playback and rewinds to t=0. Returns the terminal error in
// StateError.
func (v *VideoView) Stop() error {
	v.mu.Lock()
	if v.destroyed {
		v.mu.Unlock()
		return ErrClosed
	}
	if v.src == nil {
		v.mu.Unlock()
		return ErrNoTrack
	}
	if err := v.terminalErrLocked(); err != nil {
		v.mu.Unlock()
		return err
	}
	src := v.src
	v.state = StatePaused
	v.clock.SeekTo(0)
	v.clock.Pause()
	v.resumeSeek = false
	if v.currentFrame != nil {
		v.currentFrame.Release()
		v.currentFrame = nil
	}
	v.mu.Unlock()
	return v.seekAndInvalidate(src, 0)
}

// SeekTo jumps to t (clamped to [0, Duration]). Continues playing if
// previously playing. Returns the terminal error in StateError.
func (v *VideoView) SeekTo(t time.Duration) error {
	v.mu.Lock()
	if v.destroyed {
		v.mu.Unlock()
		return ErrClosed
	}
	if v.src == nil {
		v.mu.Unlock()
		return ErrNoTrack
	}
	if err := v.terminalErrLocked(); err != nil {
		v.mu.Unlock()
		return err
	}
	if t < 0 {
		t = 0
	}
	if v.track != nil && t > v.track.Duration && v.track.Duration > 0 {
		t = v.track.Duration
	}
	src := v.src
	v.resumeSeek = false
	if v.state == StateEnded {
		// Seeking off the end parks the view there: a following Play
		// resumes from t instead of replaying from 0.
		v.state = StatePaused
	}
	if v.currentFrame != nil {
		v.currentFrame.Release()
		v.currentFrame = nil
	}
	v.clock.SeekTo(t)
	v.mu.Unlock()
	return v.seekAndInvalidate(src, t)
}

// Duration returns the source's total length, or 0 if no source is
// open.
func (v *VideoView) Duration() time.Duration {
	v.mu.Lock()
	defer v.mu.Unlock()
	if v.track == nil {
		return 0
	}
	return v.track.Duration
}

// Position returns the current playback position from the clock.
func (v *VideoView) Position() time.Duration {
	v.mu.Lock()
	c := v.clock
	v.mu.Unlock()
	if c == nil {
		return 0
	}
	return c.HostTime()
}

// SetVolume sets the audio output gain in [0, 1]. Forwarded to the
// source's AudioRenderer if present; no-op for mute / video-only
// sources. The value is remembered across Open calls so re-loading
// a file preserves the user's volume preference.
func (v *VideoView) SetVolume(vol float32) {
	if vol < 0 {
		vol = 0
	}
	if vol > 1 {
		vol = 1
	}
	v.mu.Lock()
	v.volume = vol
	var ar AudioRenderer
	if v.src != nil {
		ar = v.src.AudioRenderer()
	}
	v.mu.Unlock()
	if ar != nil {
		ar.SetVolume(vol)
	}
}

// SetRate scales playback speed via the clock. 1.0 = normal.
// Decoder rate is unchanged — fast playback drops frames; slow
// playback holds the same frame longer.
func (v *VideoView) SetRate(r float32) {
	if r <= 0 {
		r = 1
	}
	v.mu.Lock()
	v.rate = r
	if v.clock != nil {
		v.clock.SetRate(r)
	}
	v.mu.Unlock()
}

// SetLoop toggles automatic restart on end-of-stream.
func (v *VideoView) SetLoop(b bool) {
	v.mu.Lock()
	v.loop = b
	v.mu.Unlock()
}

// SetFitMode controls how decoded frames are placed within the
// widget bounds. Invalidates layout because the visible content
// changes shape.
func (v *VideoView) SetFitMode(m FitMode) {
	v.mu.Lock()
	v.fitMode = m
	v.mu.Unlock()
	v.InvalidateLayout()
}

// State returns the view's current state.
func (v *VideoView) State() State {
	v.mu.Lock()
	defer v.mu.Unlock()
	return v.state
}

// VideoTrack returns the open file's video track info, or nil if no
// source is open.
func (v *VideoView) VideoTrack() *VideoTrackInfo {
	v.mu.Lock()
	defer v.mu.Unlock()
	return v.track
}

// AudioTrack returns the open file's audio track info, or nil if
// the file has no audio or no source is open. Mirrors the source's
// AudioTrack — VideoView doesn't re-enumerate.
func (v *VideoView) AudioTrack() *AudioTrackInfo {
	v.mu.Lock()
	src := v.src
	v.mu.Unlock()
	if src == nil {
		return nil
	}
	return src.AudioTrack()
}

// StepForward decodes the next frame via the scrub session, swaps
// it in, and advances the clock so subsequent Play resumes from the
// new position. Auto-pauses if playing.
func (v *VideoView) StepForward() error {
	return v.step(+1)
}

// StepBackward seeks back one frame's worth of time via the scrub
// session and swaps in the result. The scrub session decodes from
// the prior IDR so the backward step is frame-accurate within
// 1/FPS.
func (v *VideoView) StepBackward() error {
	return v.step(-1)
}

func (v *VideoView) step(direction int) error {
	v.mu.Lock()
	if v.destroyed {
		v.mu.Unlock()
		return ErrClosed
	}
	if v.src == nil {
		v.mu.Unlock()
		return ErrNoTrack
	}
	if err := v.terminalErrLocked(); err != nil {
		v.mu.Unlock()
		return err
	}
	track := v.track
	curPTS := time.Duration(0)
	if v.currentFrame != nil {
		curPTS = v.currentFrame.PTS()
	} else {
		curPTS = v.clock.HostTime()
	}
	// Pause first — frame-step semantics are explicit single-shot.
	v.state = StatePaused
	v.clock.Pause()
	v.mu.Unlock()

	if track == nil {
		return ErrNoTrack
	}
	fps := track.FPS
	if fps <= 0 {
		fps = 30
	}
	frameDur := time.Duration(float64(time.Second) / float64(fps))
	target := curPTS + time.Duration(direction)*frameDur
	if target < 0 {
		target = 0
	}
	if track.Duration > 0 && target > track.Duration {
		target = track.Duration
	}
	frame, err := v.DecodeFrameAt(target)
	if err != nil {
		return err
	}

	v.mu.Lock()
	old := v.currentFrame
	v.currentFrame = frame
	v.clock.SeekTo(frame.PTS())
	// The playback decoder is still on the old position — possibly already
	// ahead of the stepped frame (StepForward) or far past it (StepBackward).
	// Arm the decoder seek Play will perform, and stop Tick promoting the
	// pump frames that belong to the pre-step position: they would overwrite
	// the frame we just decoded.
	v.resumeSeek, v.resumePTS = true, frame.PTS()
	sink := v.frameSink
	v.mu.Unlock()
	if old != nil {
		old.Release()
	}
	if sink != nil {
		frame.Retain()
		sink(frame)
	}
	// Drop whatever the pump had parked; anything already in flight is
	// discarded by the seekGen bump at park time.
	v.invalidatePump()
	v.InvalidateRectBounds()
	return nil
}

// CurrentFrame returns the frame currently being displayed,
// pre-Retained. Callers must Release. Returns nil if no frame has
// been decoded yet.
func (v *VideoView) CurrentFrame() VideoFrame {
	v.mu.Lock()
	defer v.mu.Unlock()
	if v.currentFrame == nil {
		return nil
	}
	v.currentFrame.Retain()
	return v.currentFrame
}

// DecodeFrameAt synchronously fetches the frame at-or-after pts via
// the per-source scrub decoder, which runs on its own AVAssetReader
// so it does not disturb playback. The returned frame is pre-
// Retained; callers must Release. Lazily opens the scrub decoder on
// first call.
func (v *VideoView) DecodeFrameAt(pts time.Duration) (VideoFrame, error) {
	v.mu.Lock()
	if v.destroyed {
		v.mu.Unlock()
		return nil, ErrClosed
	}
	if v.src == nil {
		v.mu.Unlock()
		return nil, ErrNoTrack
	}
	scrub := v.scrub
	src := v.src
	v.mu.Unlock()

	if scrub == nil {
		sd, err := src.OpenScrubDecoder()
		if err != nil {
			return nil, err
		}
		v.mu.Lock()
		// Re-check under lock — another caller may have raced us.
		if v.scrub == nil {
			v.scrub = sd
			scrub = sd
		} else {
			_ = sd.Close()
			scrub = v.scrub
		}
		v.mu.Unlock()
	}
	return scrub.DecodeAt(pts)
}

// SetFrameSink installs a callback fired in Tick after each
// promotion of a new currentFrame, AND after every step / scrub
// promotion. The frame passed in is pre-Retained; the sink must
// Release. Pass nil to remove.
func (v *VideoView) SetFrameSink(fn func(VideoFrame)) {
	v.mu.Lock()
	v.frameSink = fn
	v.mu.Unlock()
}

// OnReady registers a callback fired once after Open succeeds.
func (v *VideoView) OnReady(fn func()) {
	v.mu.Lock()
	v.onReady = fn
	v.mu.Unlock()
}

// OnTimeUpdate registers a callback fired each Tick with the
// current clock position. Useful for a scrubber UI.
func (v *VideoView) OnTimeUpdate(fn func(time.Duration)) {
	v.mu.Lock()
	v.onTimeUpdate = fn
	v.mu.Unlock()
}

// OnEnded registers a callback fired once when playback reaches the
// end of the stream (or every cycle if Loop is on).
func (v *VideoView) OnEnded(fn func()) {
	v.mu.Lock()
	v.onEnded = fn
	v.mu.Unlock()
}

// OnError registers a callback for asynchronous errors (decode failures
// during streaming). The view also moves to StateError, pauses its clock
// and reports the same error from the transport calls; only a successful
// Open recovers it.
func (v *VideoView) OnError(fn func(error)) {
	v.mu.Lock()
	v.onError = fn
	v.mu.Unlock()
}

// Destroy releases all decoder + GL resources. Idempotent and safe from any
// goroutine. After Destroy the view is dead: Open and the transport methods
// return ErrClosed.
func (v *VideoView) Destroy() {
	v.mu.Lock()
	v.destroyed = true
	src := v.src
	cancel := v.pumpCancel
	done := v.pumpDone
	cur := v.currentFrame
	scrub := v.scrub
	v.src = nil
	v.track = nil
	v.pumpCancel = nil
	v.pumpDone = nil
	v.currentFrame = nil
	v.scrub = nil
	v.state = StateIdle
	v.mu.Unlock()

	if cancel != nil {
		cancel()
	}
	// Wake any cond-wait inside pumpLoop so it can observe ctx.Err.
	v.pumpMu.Lock()
	v.pumpReady.Broadcast()
	pf := v.pumpFrame
	v.pumpFrame = nil
	v.pumpMu.Unlock()
	if pf != nil {
		pf.Release()
	}
	if done != nil {
		<-done
	}
	if scrub != nil {
		_ = scrub.Close()
	}
	if src != nil {
		_ = src.Close()
	}
	if cur != nil {
		cur.Release()
	}
	// Drop the retained GL draw so the renderer stops replaying a picture
	// that no longer exists. Posted rather than called directly because
	// ForgetRetainedGLDraw is UI-thread-only and Destroy is not.
	if w := v.Window(); w != nil {
		w.PostJob(func() {
			w.ForgetRetainedGLDraw(v)
			v.retained = false
		})
	}
}

// pumpLoop runs in the background, repeatedly calling NextVideoFrame
// on the source and parking each decoded frame in pumpFrame for Tick
// to consume. The single-slot design means we hold at most ONE frame
// ahead of Tick on the Go side; the backend's own ring (cap 3) is
// the actual buffer.
//
// Synchronization: pumpMu guards pumpFrame. The pump blocks on
// pumpReady.Wait until Tick consumes (sets pumpFrame=nil) or Destroy
// cancels (broadcasts pumpReady). On EOF the pump parks on pumpWake
// instead of exiting, so the seek paths (which signal it) can resume
// decoding after a replay / loop restart / seek past the end.
//
// Seek correctness: a frame popped from s.pending BEFORE the
// backend's seek-drain may be pre-seek (stale). Without detection, a
// stale frame parked in pumpFrame whose PTS sits past the new clock
// (typical of backward seeks) would never satisfy Tick's
// pf.PTS()<=clockNow test, stalling the pipeline. We snapshot
// seekGen pre-receive and re-check at park time; a mismatch means a
// seek occurred during the receive-then-park window, so the frame is
// discarded. Callers (SeekTo / Stop / loop-restart) bump seekGen via
// invalidatePump before calling src.SeekVideo.
func (v *VideoView) pumpLoop(ctx context.Context, src Source, done chan struct{}) {
	defer close(done)

	for {
		gen := v.seekGen.Load()
		frame, err := src.NextVideoFrame(ctx)
		if err != nil {
			if ctx.Err() != nil {
				vvDebugf("pumpLoop exit: %v", err)
				return
			}
			if errors.Is(err, io.EOF) {
				// End of stream. Don't exit: a seek bumps seekGen and
				// signals pumpWake, so a replay / loop restart / seek past
				// the end can resume decoding without restarting this
				// goroutine. Parking here also closes the race between
				// "EOF decided" and "seek happened" that a restart-on-seek
				// scheme has.
				vvDebugf("pumpLoop parked at EOF")
				select {
				case <-ctx.Done():
					return
				case <-v.pumpWake:
					continue
				}
			}
			vvDebugf("pumpLoop error: %v", err)
			v.failFromPump(ctx, err)
			return
		}
		if vvSample("pump_next", 30) {
			vvDebugf("pump_next pts=%v frame=%p", frame.PTS(), frame)
		}
		v.pumpMu.Lock()
		for v.pumpFrame != nil && ctx.Err() == nil {
			v.pumpReady.Wait()
		}
		if ctx.Err() != nil {
			v.pumpMu.Unlock()
			frame.Release()
			return
		}
		if v.seekGen.Load() != gen {
			// Seek happened during receive or while we waited to
			// park. Drop and retry — the next iteration will snapshot
			// the new gen and accept the first post-seek frame.
			v.pumpMu.Unlock()
			if vvSample("pump_drop_stale", 1) {
				vvDebugf("pump dropping stale frame pts=%v (seek during receive)", frame.PTS())
			}
			frame.Release()
			continue
		}
		v.pumpFrame = frame
		v.pumpMu.Unlock()
	}
}

// Tick implements qui.Tickable. Promotes the parked frame into
// currentFrame when its PTS has come due. Returns the widget bounds
// (so the window repaints) when playing or when a frame was promoted
// — otherwise an empty Rect so the idle path stays cheap.
func (v *VideoView) Tick(_ time.Time) qui.Rect {
	v.mu.Lock()
	if v.src == nil {
		v.mu.Unlock()
		return qui.Rect{}
	}
	c := v.clock
	clockNow := c.HostTime()
	sink := v.frameSink
	track := v.track
	loop := v.loop
	state := v.state
	endedCB := v.onEnded
	timeCB := v.onTimeUpdate
	resumeSeek := v.resumeSeek
	v.mu.Unlock()

	// Peek the parked frame; promote if due. Held under pumpMu so
	// pumpLoop doesn't race when refilling the slot. A pending step
	// (resumeSeek) suppresses promotion: the parked frame comes from the
	// playback decoder's old position and would replace the stepped picture.
	promoted := false
	v.pumpMu.Lock()
	if pf := v.pumpFrame; pf != nil && !resumeSeek && pf.PTS() <= clockNow {
		v.pumpFrame = nil
		v.pumpReady.Signal()
		v.pumpMu.Unlock()

		v.mu.Lock()
		old := v.currentFrame
		v.currentFrame = pf
		v.mu.Unlock()
		if old != nil {
			old.Release()
		}
		if sink != nil {
			pf.Retain()
			sink(pf)
		}
		promoted = true
		if vvSample("promote", 30) {
			vvDebugf("promote pts=%v clock=%v", pf.PTS(), clockNow)
		}
	} else {
		if pf := v.pumpFrame; pf != nil && vvSample("not_due", 60) {
			vvDebugf("not_due pumpPTS=%v clock=%v", pf.PTS(), clockNow)
		} else if v.pumpFrame == nil && vvSample("starved", 60) {
			vvDebugf("starved (no pumpFrame) clock=%v", clockNow)
		}
		v.pumpMu.Unlock()
	}

	if state == StatePlaying && track != nil && track.Duration > 0 && clockNow >= track.Duration {
		v.mu.Lock()
		if loop {
			v.clock.SeekTo(0)
			src := v.src
			if v.currentFrame != nil {
				v.currentFrame.Release()
				v.currentFrame = nil
			}
			v.mu.Unlock()
			if src != nil {
				_ = v.seekAndInvalidate(src, 0)
			}
		} else {
			v.state = StateEnded
			v.clock.Pause()
			v.mu.Unlock()
		}
		if endedCB != nil {
			endedCB()
		}
	}

	if timeCB != nil {
		timeCB(clockNow)
	}

	if v.retained {
		// The retained draw keeps the picture on screen; a new frame is a
		// GL-only redraw. Nothing is re-rasterized and the window idles
		// between frames: ask for a Tick when the parked frame comes due
		// (or soon, while the decoder has none ready yet).
		w := v.Window()
		if promoted && w != nil {
			w.RequestGLRedraw()
		}
		if state == StatePlaying && w != nil {
			wait := 5 * time.Millisecond
			v.pumpMu.Lock()
			if pf := v.pumpFrame; pf != nil {
				rate := c.Rate()
				if rate <= 0 {
					rate = 1
				}
				wait = max(time.Duration(float32(pf.PTS()-clockNow)/rate), time.Millisecond)
			}
			v.pumpMu.Unlock()
			w.RequestTickAt(time.Now().Add(min(wait, 100*time.Millisecond)))
		}
		return qui.Rect{}
	}

	// Without a retained draw (renderers that lack it) the GL framebuffer
	// is cleared every frame, so the QueueGLDraw closure must re-run every
	// frame to preserve the picture: keep the view dirty whenever there is
	// a frame on screen. (Container.Draw skips children whose bounds don't
	// intersect the dirty region, which would prevent us from even
	// queuing the closure.)
	v.mu.Lock()
	hasFrame := v.currentFrame != nil
	v.mu.Unlock()
	if state == StatePlaying || promoted || hasFrame {
		return qui.PaintBoundsInWindow(v)
	}
	return qui.Rect{}
}

// Measure returns the natural size of the video track, scaled to
// fit available with aspect preserved. Defaults to 320×180 if no
// track is open or available has no slack.
func (v *VideoView) Measure(available qui.Size) qui.Size {
	v.mu.Lock()
	t := v.track
	v.mu.Unlock()
	if t == nil || t.Width <= 0 || t.Height <= 0 {
		w := available.W
		if w <= 0 {
			w = 320
		}
		h := available.H
		if h <= 0 {
			h = 180
		}
		return qui.Size{W: w, H: h}
	}
	tw := float32(t.Width)
	th := float32(t.Height)
	aspect := tw / th
	w := available.W
	h := available.H
	if w <= 0 && h <= 0 {
		return qui.Size{W: tw, H: th}
	}
	if w <= 0 {
		return qui.Size{W: h * aspect, H: h}
	}
	if h <= 0 {
		return qui.Size{W: w, H: w / aspect}
	}
	// Both axes constrained: fit inside, preserve aspect.
	if w/aspect <= h {
		return qui.Size{W: w, H: w / aspect}
	}
	return qui.Size{W: h * aspect, H: h}
}

// HitTest returns self so mouse events route to the view (custom
// frame-step UI may bind to clicks).
func (v *VideoView) HitTest(p qui.Point) qui.Widget {
	if v.Bounds().Contains(p) {
		return v
	}
	return nil
}

// Draw composites the current frame onto the framebuffer via
// GPUCanvas. Mirrors scene3d.Viewport's pattern: type-assert to
// GPUCanvas, punch a transparent hole, queue a GL closure that
// binds the texture and blits it via ActiveGLRenderer.DrawTexture.
//
// CPU-canvas fallback (noop renderer in tests): paints the style
// background — no GL, no frame.
func (v *VideoView) Draw(canvas qui.Canvas) {
	gpu, ok := canvas.(qui.GPUCanvas)
	if !ok {
		if vvSample("draw_nogpu", 60) {
			vvDebugf("Draw: canvas is NOT GPUCanvas (CPU fallback)")
		}
		bg := v.Style().Background
		if bg == (qui.Color{}) {
			bg = qui.Color{R: 0.10, G: 0.10, B: 0.12, A: 1}
		}
		canvas.FillRect(v.Bounds(), bg)
		return
	}

	v.mu.Lock()
	frame := v.currentFrame
	fit := v.fitMode
	track := v.track
	v.mu.Unlock()

	if vvSample("draw", 30) {
		framePTS := "nil"
		if frame != nil {
			framePTS = frame.PTS().String()
		}
		vvDebugf("Draw frame=%v bounds=%v track=%v", framePTS, v.Bounds(), track != nil)
	}

	bounds := v.Bounds()
	// Letterbox fill (or full background under FitStretch) before
	// punching a hole for the texture. Under FitCover the texture
	// fills the whole rect, but bg keeps a clean border when the
	// frame hasn't arrived yet.
	bg := v.Style().Background
	if bg == (qui.Color{}) {
		bg = qui.Color{R: 0, G: 0, B: 0, A: 1}
	}
	canvas.FillRect(bounds, bg)

	if frame == nil || track == nil {
		return
	}

	dstRect := fitDstRect(fit, bounds, float32(track.Width), float32(track.Height))
	// FitCover's rect is larger than the widget. Crop by sampling: take
	// the visible part of dst and the matching source rect, so nothing
	// spills past the bounds (the GL draw has no scissor of its own).
	src, dst, ok := cropToVisible(dstRect, bounds, float32(track.Width), float32(track.Height))
	if !ok {
		return
	}
	// Hole-punch only the area the texture will cover, so the
	// letterbox background remains opaque around it. Use the full
	// bounds for the hole (not just dstRect) so any future under-
	// the-frame UI works the same way.
	canvas.FillRect(bounds, qui.Color{R: 0, G: 0, B: 0, A: 0})

	if rk, ok := canvas.(qui.RetainedGLCanvas); ok {
		v.glSrc, v.glDst = src, dst
		rk.QueueRetainedGLDraw(v, canvas.CurrentMatrix().TransformRect(bounds), v.glDraw)
		v.retained = true
		return
	}
	gpu.QueueGLDraw(func(state qui.GLState) { drawVideoFrame(state, frame, src, dst) })
}

// glDraw is the retained GL draw: it shows whatever frame is current when
// the frame is produced, so a RequestGLRedraw picks up a new one.
func (v *VideoView) glDraw(state qui.GLState) {
	v.mu.Lock()
	frame, src, dst := v.currentFrame, v.glSrc, v.glDst
	// Hold a reference across the draw: Destroy / SeekTo may release the
	// current frame from another goroutine while its texture is in use.
	if frame != nil {
		frame.Retain()
	}
	v.mu.Unlock()
	if frame != nil {
		drawVideoFrame(state, frame, src, dst)
		frame.Release()
	}
}

// drawVideoFrame composites the src rect of frame (frame pixels) into
// dstRect (logical pixels) of the current framebuffer.
func drawVideoFrame(state qui.GLState, frame VideoFrame, src, dstRect qui.Rect) {
	// HiDPI: dstRect is logical pixels; the GL framebuffer is
	// physical. Scale by the ratio so the texture lands at the
	// right place on Retina displays.
	scaleX, scaleY := float32(1), float32(1)
	if state.LogicalSize.W > 0 {
		scaleX = state.FramebufferSize.W / state.LogicalSize.W
	}
	if state.LogicalSize.H > 0 {
		scaleY = state.FramebufferSize.H / state.LogicalSize.H
	}
	physDst := qui.Rect{
		X: dstRect.X * scaleX,
		Y: dstRect.Y * scaleY,
		W: dstRect.W * scaleX,
		H: dstRect.H * scaleY,
	}
	// Darwin frames know how to draw themselves — they hold a
	// GL_TEXTURE_RECTANGLE which GLRenderer.DrawTexture's
	// sampler2D shader can't sample. The rect-aware shader
	// lives in gl_rect_darwin.go.
	if fbd, ok := frame.(framebufferDrawer); ok {
		if vvSample("drawfb", 30) {
			vvDebugf("DrawIntoFramebuffer physDst=%v fbSize=%v logSize=%v texSize=%dx%d",
				physDst, state.FramebufferSize, state.LogicalSize,
				frame.Width(), frame.Height())
		}
		if !fbd.DrawIntoFramebuffer(state, src, physDst, true) {
			vvDebugf("QueueGLDraw: DrawIntoFramebuffer FAILED")
		}
		if flushTextureCache != nil {
			flushTextureCache()
		}
		return
	}
	// Fallback: 2D-texture path via the renderer's stock shader.
	// Not hit on darwin; kept for hypothetical future backends
	// that bind eagerly into GL_TEXTURE_2D.
	texName := frame.GLTexture()
	if texName == 0 {
		vvDebugf("QueueGLDraw: texName=0 and no framebufferDrawer, skipping")
		return
	}
	glr := qui.ActiveGLRenderer()
	if glr == nil {
		vvDebugf("QueueGLDraw: ActiveGLRenderer is nil")
		return
	}
	texW := int32(frame.Width())
	texH := int32(frame.Height())
	glr.DrawTexture(
		texName,
		src,
		physDst,
		texW,
		texH,
		qui.TextureOpts{FlipY: true, Tint: qui.Color{R: 1, G: 1, B: 1, A: 1}, Opacity: 1},
	)
}

// InvalidateRectBounds is a thin wrapper around the parent window's
// InvalidateRect for v.Bounds(). Used after frame-step promotions
// when Play state hasn't changed so Tick won't naturally repaint.
func (v *VideoView) InvalidateRectBounds() {
	// Walk up to the root and find the *Window via Parent chain.
	// The standard pattern is to call BaseWidget.InvalidateLayout
	// which bubbles; for paint-only invalidation we mark the bounds
	// dirty. Simplest: invalidate layout — Window's next Step will
	// re-render us.
	v.InvalidateLayout()
}

// fitDstRect computes the destination rect for a video frame inside
// widget bounds given a fit mode.
func fitDstRect(fit FitMode, bounds qui.Rect, frameW, frameH float32) qui.Rect {
	if frameW <= 0 || frameH <= 0 {
		return bounds
	}
	bw, bh := bounds.W, bounds.H
	aspect := frameW / frameH
	switch fit {
	case FitStretch:
		return bounds
	case FitCover:
		// Fill the whole bounds; crop overflow on the longer axis by
		// having dst exceed bounds. This relies on the renderer
		// clipping to bounds — DrawTexture itself doesn't clip, but
		// the surrounding 2D clip stack does (ClipCanvas). For v1,
		// approximate with fit-inside-then-scale-up.
		// We compute the rect that fully covers bounds while
		// preserving aspect, then return it centered. Pixels outside
		// bounds will be drawn but clipped by the surrounding scroll
		// / container clip stack.
		if bw/aspect < bh {
			// limit by height
			w := bh * aspect
			return qui.Rect{X: bounds.X + (bw-w)/2, Y: bounds.Y, W: w, H: bh}
		}
		h := bw / aspect
		return qui.Rect{X: bounds.X, Y: bounds.Y + (bh-h)/2, W: bw, H: h}
	case FitContain:
		fallthrough
	default:
		// Letterbox: largest rect that fits inside bounds preserving aspect.
		if bw/aspect <= bh {
			h := bw / aspect
			return qui.Rect{X: bounds.X, Y: bounds.Y + (bh-h)/2, W: bw, H: h}
		}
		w := bh * aspect
		return qui.Rect{X: bounds.X + (bw-w)/2, Y: bounds.Y, W: w, H: bh}
	}
}

// fail records a terminal error: the view moves to StateError, the clock
// stops (so Position freezes and no end-of-stream callback fires), the error
// is remembered for transport calls, and OnError is dispatched outside the
// lock. The pump goroutine has stopped by the time this returns, so only a
// successful Open leaves the state again.
func (v *VideoView) fail(err error) {
	if fn := v.setFailed(err); fn != nil {
		fn(err)
	}
}

// failFromPump is fail for the decoder goroutine. A pump whose context was
// cancelled (Open of a new source, a seek, Destroy) no longer speaks for
// the view, and OnError runs on the UI thread like the other callbacks, so
// a handler may update widgets.
func (v *VideoView) failFromPump(ctx context.Context, err error) {
	if ctx.Err() != nil {
		return
	}
	fn := v.setFailed(err)
	if fn == nil {
		return
	}
	if w := v.Window(); w != nil {
		w.PostJob(func() { fn(err) })
		return
	}
	fn(err)
}

// setFailed moves the view to StateError and returns the OnError callback.
func (v *VideoView) setFailed(err error) func(error) {
	v.mu.Lock()
	defer v.mu.Unlock()
	v.state = StateError
	v.lastErr = err
	v.resumeSeek = false
	if v.clock != nil {
		v.clock.Pause()
	}
	return v.onError
}

// terminalErrLocked returns the error that put the view in StateError, if
// any. Caller holds v.mu. Transport calls report it instead of pretending to
// play a dead decoder.
func (v *VideoView) terminalErrLocked() error {
	if v.state != StateError {
		return nil
	}
	return v.lastErr
}
