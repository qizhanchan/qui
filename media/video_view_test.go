package media

import (
	"context"
	"errors"
	"io"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// ----- Fake backend -----------------------------------------------------

// fakeBackend serves a deterministic stream of decoded frames so we
// can exercise VideoView's clock, pump, state machine, and frame-sink
// without any cgo / GLFW dependency. Used by every test in this file
// via swapBackend.
type fakeBackend struct {
	caps       Capabilities
	openCount  atomic.Int32
	openError  error
	makeSource func() *fakeSource
}

func (b *fakeBackend) Capabilities() Capabilities { return b.caps }
func (b *fakeBackend) OpenSource(path string) (Source, error) {
	b.openCount.Add(1)
	if b.openError != nil {
		return nil, b.openError
	}
	if b.makeSource != nil {
		return b.makeSource(), nil
	}
	return newFakeSource(), nil
}

type fakeFrame struct {
	pts  time.Duration
	w, h int
	refs atomic.Int32
}

func (f *fakeFrame) PTS() time.Duration { return f.pts }
func (f *fakeFrame) Width() int         { return f.w }
func (f *fakeFrame) Height() int        { return f.h }
func (f *fakeFrame) GLTexture() uint32  { return 0 }
func (f *fakeFrame) Retain()            { f.refs.Add(1) }
func (f *fakeFrame) Release()           { f.refs.Add(-1) }

type fakeSource struct {
	track  *VideoTrackInfo
	mu     sync.Mutex
	pos    time.Duration // next PTS to emit
	step   time.Duration // PTS increment per frame
	end    time.Duration // emit io.EOF once pos > end
	closed atomic.Bool

	// Observability for lifecycle tests (VideoView re-Open / frame step).
	scrubOpens atomic.Int32
	seekCalls  atomic.Int32
	lastSeekNs atomic.Int64
}

func newFakeSource() *fakeSource {
	return &fakeSource{
		track: &VideoTrackInfo{
			Width:       64,
			Height:      48,
			FPS:         60,
			Duration:    time.Second,
			Codec:       "h264",
			PixelFormat: "bgra",
		},
		step: time.Second / 60,
		end:  time.Second,
	}
}

func (s *fakeSource) VideoTrack() *VideoTrackInfo  { return s.track }
func (s *fakeSource) AudioTrack() *AudioTrackInfo  { return nil }
func (s *fakeSource) AudioRenderer() AudioRenderer { return nil }

func (s *fakeSource) NextVideoFrame(ctx context.Context) (VideoFrame, error) {
	if s.closed.Load() {
		return nil, io.EOF
	}
	s.mu.Lock()
	pos := s.pos
	s.pos += s.step
	s.mu.Unlock()
	if pos > s.end {
		// Park forever waiting for ctx so the pump goroutine can be
		// cancelled cleanly when the test ends.
		<-ctx.Done()
		return nil, ctx.Err()
	}
	f := &fakeFrame{pts: pos, w: s.track.Width, h: s.track.Height}
	f.refs.Store(1)
	return f, nil
}

func (s *fakeSource) SeekVideo(pts time.Duration, _ bool) error {
	s.mu.Lock()
	s.pos = pts
	s.mu.Unlock()
	s.seekCalls.Add(1)
	s.lastSeekNs.Store(int64(pts))
	return nil
}

func (s *fakeSource) OpenScrubDecoder() (ScrubDecoder, error) {
	s.scrubOpens.Add(1)
	return &fakeScrubDecoder{src: s}, nil
}

func (s *fakeSource) Close() error {
	s.closed.Store(true)
	return nil
}

type fakeScrubDecoder struct {
	src    *fakeSource
	closed atomic.Bool
}

func (d *fakeScrubDecoder) DecodeAt(pts time.Duration) (VideoFrame, error) {
	if d.closed.Load() {
		return nil, ErrClosed
	}
	f := &fakeFrame{pts: pts, w: d.src.track.Width, h: d.src.track.Height}
	f.refs.Store(1)
	return f, nil
}

func (d *fakeScrubDecoder) Close() error {
	d.closed.Store(true)
	return nil
}

// swapBackend installs a backend for the duration of a test and
// returns a restore function. The package-private override hook is
// reachable because the test lives in `package media`.
func swapBackend(t *testing.T, b Backend) func() {
	t.Helper()
	return overrideBackendForTest(b)
}

// virtualClock pins the wall clock used by wallClock so Tick-driven
// state machines step deterministically without real sleeps. The mutex is
// required: the pump goroutine also reads the clock (a decode failure pauses
// it), so advancing the virtual time from the test goroutine races with it
// otherwise.
func virtualClock(t *testing.T) func(d time.Duration) {
	t.Helper()
	original := nowFunc
	var mu sync.Mutex
	cur := time.Unix(0, 0)
	nowFunc = func() time.Time {
		mu.Lock()
		defer mu.Unlock()
		return cur
	}
	t.Cleanup(func() { nowFunc = original })
	return func(d time.Duration) {
		mu.Lock()
		cur = cur.Add(d)
		mu.Unlock()
	}
}

// ----- Tests -----------------------------------------------------------

func TestVideoViewOpenRejectsNoVideoTrack(t *testing.T) {
	restore := swapBackend(t, &fakeBackend{
		makeSource: func() *fakeSource {
			s := newFakeSource()
			s.track = nil // no video track
			return s
		},
	})
	defer restore()

	v := NewVideoView()
	defer v.Destroy()
	err := v.Open("ignored")
	if !errors.Is(err, ErrNoTrack) {
		t.Errorf("Open without video track = %v, want ErrNoTrack", err)
	}
}

func TestVideoViewOpenSetsTrack(t *testing.T) {
	restore := swapBackend(t, &fakeBackend{})
	defer restore()

	v := NewVideoView()
	defer v.Destroy()
	if err := v.Open("ignored"); err != nil {
		t.Fatalf("Open: %v", err)
	}
	tr := v.VideoTrack()
	if tr == nil {
		t.Fatal("VideoTrack nil after Open")
	}
	if tr.Width != 64 || tr.Height != 48 {
		t.Errorf("track size = %dx%d, want 64x48", tr.Width, tr.Height)
	}
	if got := v.State(); got != StatePaused {
		t.Errorf("post-Open State = %v, want StatePaused", got)
	}
}

func TestVideoViewPlayPauseTransitions(t *testing.T) {
	virtualClock(t)
	restore := swapBackend(t, &fakeBackend{})
	defer restore()

	v := NewVideoView()
	defer v.Destroy()
	if err := v.Open("ignored"); err != nil {
		t.Fatalf("Open: %v", err)
	}

	if err := v.Play(); err != nil {
		t.Errorf("Play: %v", err)
	}
	if got := v.State(); got != StatePlaying {
		t.Errorf("Play State = %v, want StatePlaying", got)
	}
	if err := v.Pause(); err != nil {
		t.Errorf("Pause: %v", err)
	}
	if got := v.State(); got != StatePaused {
		t.Errorf("Pause State = %v, want StatePaused", got)
	}
}

func TestVideoViewPositionFollowsClock(t *testing.T) {
	tick := virtualClock(t)
	restore := swapBackend(t, &fakeBackend{})
	defer restore()

	v := NewVideoView()
	defer v.Destroy()
	if err := v.Open("ignored"); err != nil {
		t.Fatalf("Open: %v", err)
	}
	v.Play()
	tick(250 * time.Millisecond)
	if got := v.Position(); got != 250*time.Millisecond {
		t.Errorf("Position after 250ms wall = %v, want 250ms", got)
	}
	v.SeekTo(750 * time.Millisecond)
	if got := v.Position(); got != 750*time.Millisecond {
		t.Errorf("Position after SeekTo(750ms) = %v, want 750ms", got)
	}
	// Past-duration seek clamps to track Duration (1s in the fake).
	v.SeekTo(2 * time.Second)
	if got := v.Position(); got != time.Second {
		t.Errorf("Position after SeekTo(2s past dur) = %v, want 1s clamped", got)
	}
}

// Drives the Tick loop until either fn returns true or the deadline
// expires. Each iteration advances the virtual clock by step.
func driveTicks(v *VideoView, advance func(time.Duration), step time.Duration, deadline time.Duration, fn func() bool) bool {
	elapsed := time.Duration(0)
	for elapsed < deadline {
		advance(step)
		v.Tick(time.Time{})
		if fn() {
			return true
		}
		elapsed += step
		// Yield so the pump goroutine can wake and park a frame.
		time.Sleep(time.Millisecond)
	}
	return false
}

func TestVideoViewFrameSinkInvokedPerPromotion(t *testing.T) {
	tick := virtualClock(t)
	restore := swapBackend(t, &fakeBackend{})
	defer restore()

	var sinkCount atomic.Int32
	v := NewVideoView()
	defer v.Destroy()
	v.SetFrameSink(func(f VideoFrame) {
		sinkCount.Add(1)
		f.Release()
	})
	if err := v.Open("ignored"); err != nil {
		t.Fatalf("Open: %v", err)
	}
	v.Play()
	ok := driveTicks(v, tick, 20*time.Millisecond, 2*time.Second, func() bool {
		return sinkCount.Load() >= 5
	})
	if !ok {
		t.Errorf("expected >=5 sink invocations, got %d", sinkCount.Load())
	}
}

func TestVideoViewOnEndedFires(t *testing.T) {
	tick := virtualClock(t)
	restore := swapBackend(t, &fakeBackend{})
	defer restore()

	ended := make(chan struct{}, 1)
	v := NewVideoView()
	defer v.Destroy()
	v.OnEnded(func() {
		select {
		case ended <- struct{}{}:
		default:
		}
	})
	if err := v.Open("ignored"); err != nil {
		t.Fatalf("Open: %v", err)
	}
	v.Play()
	// Track duration is 1s; advance well past so the EOF branch fires.
	ok := driveTicks(v, tick, 50*time.Millisecond, 3*time.Second, func() bool {
		select {
		case <-ended:
			return true
		default:
			return false
		}
	})
	if !ok {
		t.Error("OnEnded never fired after clock passed Duration")
	}
	if got := v.State(); got != StateEnded {
		t.Errorf("post-end State = %v, want StateEnded", got)
	}
}

func TestVideoViewOnTimeUpdateMonotonic(t *testing.T) {
	tick := virtualClock(t)
	restore := swapBackend(t, &fakeBackend{})
	defer restore()

	var lastPos atomic.Int64
	var monotonic atomic.Bool
	monotonic.Store(true)
	v := NewVideoView()
	defer v.Destroy()
	v.OnTimeUpdate(func(p time.Duration) {
		prev := time.Duration(lastPos.Load())
		if p < prev {
			monotonic.Store(false)
		}
		lastPos.Store(int64(p))
	})
	if err := v.Open("ignored"); err != nil {
		t.Fatalf("Open: %v", err)
	}
	v.Play()
	driveTicks(v, tick, 16*time.Millisecond, 500*time.Millisecond, func() bool { return false })
	if !monotonic.Load() {
		t.Error("OnTimeUpdate position regressed at some Tick")
	}
}

func TestVideoViewDecodeFrameAtUsesScrub(t *testing.T) {
	virtualClock(t)
	restore := swapBackend(t, &fakeBackend{})
	defer restore()

	v := NewVideoView()
	defer v.Destroy()
	if err := v.Open("ignored"); err != nil {
		t.Fatalf("Open: %v", err)
	}
	f, err := v.DecodeFrameAt(500 * time.Millisecond)
	if err != nil {
		t.Fatalf("DecodeFrameAt: %v", err)
	}
	if f == nil {
		t.Fatal("DecodeFrameAt returned nil frame")
	}
	if f.PTS() != 500*time.Millisecond {
		t.Errorf("scrub frame PTS = %v, want 500ms", f.PTS())
	}
	f.Release()
}

func TestVideoViewStopRewinds(t *testing.T) {
	tick := virtualClock(t)
	restore := swapBackend(t, &fakeBackend{})
	defer restore()

	v := NewVideoView()
	defer v.Destroy()
	if err := v.Open("ignored"); err != nil {
		t.Fatalf("Open: %v", err)
	}
	v.Play()
	tick(200 * time.Millisecond)
	if got := v.Position(); got != 200*time.Millisecond {
		t.Errorf("pre-Stop Position = %v, want 200ms", got)
	}
	if err := v.Stop(); err != nil {
		t.Errorf("Stop: %v", err)
	}
	if got := v.Position(); got != 0 {
		t.Errorf("post-Stop Position = %v, want 0", got)
	}
	if got := v.State(); got != StatePaused {
		t.Errorf("post-Stop State = %v, want StatePaused", got)
	}
}

func TestVideoViewMethodsWithoutSource(t *testing.T) {
	restore := swapBackend(t, &fakeBackend{})
	defer restore()
	v := NewVideoView()
	defer v.Destroy()
	if err := v.Play(); err != ErrNoTrack {
		t.Errorf("Play without source = %v, want ErrNoTrack", err)
	}
	if err := v.Pause(); err != ErrNoTrack {
		t.Errorf("Pause without source = %v, want ErrNoTrack", err)
	}
	if err := v.SeekTo(0); err != ErrNoTrack {
		t.Errorf("SeekTo without source = %v, want ErrNoTrack", err)
	}
	if got := v.Duration(); got != 0 {
		t.Errorf("Duration without source = %v, want 0", got)
	}
}

func TestVideoViewNotSupportedWithoutBackend(t *testing.T) {
	restore := swapBackend(t, nil)
	defer restore()
	v := NewVideoView()
	defer v.Destroy()
	err := v.Open("any")
	if !errors.Is(err, ErrNotSupported) {
		t.Errorf("Open without backend = %v, want ErrNotSupported", err)
	}
}

// ----- Lifecycle regressions -------------------------------------------

// sourcesBackend records every fakeSource it hands to Open so a test can
// inspect the one a later call must have bound to. tune, when non-nil, runs
// before the source is returned (i.e. before any pump goroutine sees it).
func sourcesBackend(sources *[]*fakeSource, tune func(*fakeSource)) *fakeBackend {
	b := &fakeBackend{}
	b.makeSource = func() *fakeSource {
		s := newFakeSource()
		if tune != nil {
			tune(s)
		}
		*sources = append(*sources, s)
		return s
	}
	return b
}

// hasCurrentFrame reports whether a frame is on screen without retaining it.
func hasCurrentFrame(v *VideoView) bool {
	v.mu.Lock()
	defer v.mu.Unlock()
	return v.currentFrame != nil
}

// parkedPTS reports the PTS of the frame the pump is holding, if any.
func parkedPTS(v *VideoView) (time.Duration, bool) {
	v.pumpMu.Lock()
	defer v.pumpMu.Unlock()
	if v.pumpFrame == nil {
		return 0, false
	}
	return v.pumpFrame.PTS(), true
}

func waitForParked(t *testing.T, v *VideoView) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if _, ok := parkedPTS(v); ok {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("pump never parked a frame")
}

// Open closes the previous source; the scrub session and the displayed
// frame belong to that source, so re-opening must rebind both. Regression:
// a stale scrub answered DecodeFrameAt/Step* from the closed source.
func TestVideoViewReopenRebindsScrub(t *testing.T) {
	var sources []*fakeSource
	restore := swapBackend(t, sourcesBackend(&sources, nil))
	defer restore()

	v := NewVideoView()
	defer v.Destroy()
	if err := v.Open("a"); err != nil {
		t.Fatalf("Open a: %v", err)
	}
	f, err := v.DecodeFrameAt(0)
	if err != nil {
		t.Fatalf("DecodeFrameAt on a: %v", err)
	}
	f.Release()
	if got := sources[0].scrubOpens.Load(); got != 1 {
		t.Fatalf("source a opened %d scrub decoders, want 1", got)
	}

	if err := v.Open("b"); err != nil {
		t.Fatalf("Open b: %v", err)
	}
	f, err = v.DecodeFrameAt(100 * time.Millisecond)
	if err != nil {
		t.Fatalf("DecodeFrameAt after re-Open: %v (stale scrub from the closed source?)", err)
	}
	f.Release()
	if got := sources[1].scrubOpens.Load(); got != 1 {
		t.Errorf("source b opened %d scrub decoders, want 1 (reused source a's closed scrub)", got)
	}
	if got := sources[0].scrubOpens.Load(); got != 1 {
		t.Errorf("source a opened %d scrub decoders, want 1 (stale scrub reused)", got)
	}
}

// Re-opening must release the frame shown by the previous source instead of
// leaving it on screen (it would otherwise survive until the new file
// promotes its first frame, i.e. forever while paused).
func TestVideoViewReopenDropsOldFrame(t *testing.T) {
	tick := virtualClock(t)
	var sources []*fakeSource
	restore := swapBackend(t, sourcesBackend(&sources, nil))
	defer restore()

	v := NewVideoView()
	defer v.Destroy()
	if err := v.Open("a"); err != nil {
		t.Fatalf("Open a: %v", err)
	}
	v.Play()
	if !driveTicks(v, tick, 20*time.Millisecond, 2*time.Second, func() bool {
		return hasCurrentFrame(v)
	}) {
		t.Fatal("no frame promoted before re-Open")
	}
	old := v.CurrentFrame() // pre-retained; our reference
	if old == nil {
		t.Fatal("CurrentFrame returned nil")
	}

	if err := v.Open("b"); err != nil {
		old.Release()
		t.Fatalf("Open b: %v", err)
	}
	if got := v.CurrentFrame(); got != nil {
		got.Release()
		old.Release()
		t.Fatalf("currentFrame from the closed source still installed after re-Open (pts=%v)", got.PTS())
	}
	// The view's reference must be gone; only ours is left.
	if got := old.(*fakeFrame).refs.Load(); got != 1 {
		t.Errorf("old frame refs = %d, want 1 (the view leaked %d)", got, got-1)
	}
	old.Release()
}

// A frame step moves the clock and the picture through the scrub session,
// but the playback decoder stays where it was. Tick must not promote the
// pump's pre-step frames over the stepped picture — after several forward
// steps the pump is behind the clock and would drag the picture back.
func TestVideoViewStepForwardNotOverriddenByPump(t *testing.T) {
	tick := virtualClock(t)
	var sources []*fakeSource
	restore := swapBackend(t, sourcesBackend(&sources, nil))
	defer restore()

	v := NewVideoView()
	defer v.Destroy()
	if err := v.Open("a"); err != nil {
		t.Fatalf("Open: %v", err)
	}
	v.Play()
	if !driveTicks(v, tick, 20*time.Millisecond, 2*time.Second, func() bool {
		return hasCurrentFrame(v)
	}) {
		t.Fatal("no frame promoted")
	}
	waitForParked(t, v) // make the override possible at all

	want := time.Duration(0)
	for i := 0; i < 20; i++ {
		if err := v.StepForward(); err != nil {
			t.Fatalf("StepForward %d: %v", i, err)
		}
		want = v.Position()
	}
	v.Tick(time.Time{}) // no clock advance: only a stale promotion could change the picture
	f := v.CurrentFrame()
	if f == nil {
		t.Fatal("CurrentFrame nil after step")
	}
	got := f.PTS()
	f.Release()
	if got != want {
		t.Errorf("after 20 forward steps + Tick the picture is at %v, want the stepped %v (pump override)", got, want)
	}
}

// Stepping backward leaves the playback decoder ahead of the clock. Play
// must reposition it at the stepped frame; otherwise the pump stays parked
// on a future frame and playback freezes until the clock catches up.
func TestVideoViewStepBackwardReseeksOnPlay(t *testing.T) {
	tick := virtualClock(t)
	var sources []*fakeSource
	restore := swapBackend(t, sourcesBackend(&sources, func(s *fakeSource) {
		// A longer source so several backward steps stay inside the track.
		s.track.Duration = 10 * time.Second
		s.end = 10 * time.Second
	}))
	defer restore()

	v := NewVideoView()
	defer v.Destroy()
	if err := v.Open("a"); err != nil {
		t.Fatalf("Open: %v", err)
	}
	src := sources[0]

	v.Play()
	if !driveTicks(v, tick, 20*time.Millisecond, 5*time.Second, func() bool {
		return v.Position() >= 2*time.Second
	}) {
		t.Fatal("clock never reached 2s")
	}
	// Let the pump settle on one parked frame (the clock is frozen here, so
	// the pump refills its single slot and blocks).
	waitForParked(t, v)

	for i := 0; i < 20; i++ {
		if err := v.StepBackward(); err != nil {
			t.Fatalf("StepBackward %d: %v", i, err)
		}
	}
	want := v.Position()
	if want >= 2*time.Second {
		t.Fatalf("stepped position %v did not move back from 2s", want)
	}
	if got := src.seekCalls.Load(); got != 0 {
		t.Fatalf("stepping seeked the playback decoder %d times; the seek belongs to Play", got)
	}
	// While paused the stepped picture must survive a Tick.
	v.Tick(time.Time{})
	if f := v.CurrentFrame(); f == nil || f.PTS() != want {
		if f != nil {
			f.Release()
		}
		t.Fatalf("picture after backward step + Tick is not the stepped frame")
	} else {
		f.Release()
	}

	if err := v.Play(); err != nil {
		t.Fatalf("Play: %v", err)
	}
	if got := src.seekCalls.Load(); got != 1 {
		t.Errorf("Play after step seeked the decoder %d times, want 1", got)
	}
	if got := time.Duration(src.lastSeekNs.Load()); got != want {
		t.Errorf("Play seeked to %v, want the stepped %v", got, want)
	}
	// And the pipeline must actually resume from there.
	ok := driveTicks(v, tick, 20*time.Millisecond, time.Second, func() bool {
		f := v.CurrentFrame()
		if f == nil || f.PTS() <= want {
			return false
		}
		advanced := f.PTS() <= want+200*time.Millisecond
		f.Release()
		return advanced
	})
	if !ok {
		t.Error("playback did not resume near the stepped position (pump still parked on a pre-step frame)")
	}
}

// ----- End-of-stream pump lifecycle ------------------------------------

// eofSource reports io.EOF once its frames are exhausted, the way the
// pure-Go backend does (the darwin reader blocks in NextVideoFrame until
// the next seek instead). VideoView's pump therefore hits the end-of-stream
// path rather than blocking in the receive.
type eofSource struct {
	track  *VideoTrackInfo
	frames int
	mu     sync.Mutex
	next   int
	closed bool
	eofs   atomic.Int32 // NextVideoFrame calls that returned io.EOF
}

func (s *eofSource) VideoTrack() *VideoTrackInfo  { return s.track }
func (s *eofSource) AudioTrack() *AudioTrackInfo  { return nil }
func (s *eofSource) AudioRenderer() AudioRenderer { return nil }

func (s *eofSource) NextVideoFrame(context.Context) (VideoFrame, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed || s.next >= s.frames {
		s.eofs.Add(1)
		return nil, io.EOF
	}
	fps := s.track.FPS
	f := &fakeFrame{pts: time.Duration(float64(s.next) / float64(fps) * float64(time.Second)), w: 64, h: 48}
	f.refs.Store(1)
	s.next++
	return f, nil
}

func (s *eofSource) SeekVideo(pts time.Duration, _ bool) error {
	s.mu.Lock()
	s.next = int(pts.Seconds() * float64(s.track.FPS))
	s.mu.Unlock()
	return nil
}

func (s *eofSource) OpenScrubDecoder() (ScrubDecoder, error) { return nil, ErrNoTrack }

func (s *eofSource) Close() error {
	s.mu.Lock()
	s.closed = true
	s.mu.Unlock()
	return nil
}

type eofBackend struct{ src *eofSource }

func (eofBackend) Capabilities() Capabilities          { return Capabilities{} }
func (b eofBackend) OpenSource(string) (Source, error) { return b.src, nil }

// A backend that reports EOF ends the decoder's stream; replay (and loop /
// seek-after-end) must revive the pump. Regression: the pump goroutine used
// to return at EOF, so the restarted decoder filled a ring nobody drained
// and the picture never came back.
func TestVideoViewReplayAfterEndResumesPump(t *testing.T) {
	tick := virtualClock(t)
	src := &eofSource{
		track:  &VideoTrackInfo{Width: 64, Height: 48, FPS: 30, Duration: time.Second},
		frames: 5,
	}
	restore := swapBackend(t, eofBackend{src: src})
	defer restore()

	v := NewVideoView()
	defer v.Destroy()
	if err := v.Open("ignored"); err != nil {
		t.Fatalf("Open: %v", err)
	}
	v.Play()
	if !driveTicks(v, tick, 20*time.Millisecond, 3*time.Second, func() bool {
		return v.State() == StateEnded
	}) {
		t.Fatalf("never reached StateEnded (state=%v)", v.State())
	}
	if src.eofs.Load() == 0 {
		t.Fatal("source never reported EOF; test does not cover the end-of-stream path")
	}

	if err := v.Play(); err != nil {
		t.Fatalf("replay Play: %v", err)
	}
	if !driveTicks(v, tick, 20*time.Millisecond, time.Second, func() bool {
		return hasCurrentFrame(v)
	}) {
		t.Error("replay after end-of-stream produced no frames (pump goroutine not restarted)")
	}
}

// ----- Decode failure --------------------------------------------------

// failingSource yields a few frames and then a terminal decode error.
type failingSource struct {
	track *VideoTrackInfo
	err   error
	after int
	mu    sync.Mutex
	n     int
}

func (s *failingSource) VideoTrack() *VideoTrackInfo  { return s.track }
func (s *failingSource) AudioTrack() *AudioTrackInfo  { return nil }
func (s *failingSource) AudioRenderer() AudioRenderer { return nil }

func (s *failingSource) NextVideoFrame(context.Context) (VideoFrame, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.n >= s.after {
		return nil, s.err
	}
	fps := s.track.FPS
	f := &fakeFrame{pts: time.Duration(float64(s.n) / float64(fps) * float64(time.Second)), w: 64, h: 48}
	f.refs.Store(1)
	s.n++
	return f, nil
}

func (s *failingSource) SeekVideo(time.Duration, bool) error { return nil }
func (s *failingSource) OpenScrubDecoder() (ScrubDecoder, error) {
	return nil, ErrNoTrack
}
func (s *failingSource) Close() error { return nil }

type failingBackend struct{ src *failingSource }

func (failingBackend) Capabilities() Capabilities          { return Capabilities{} }
func (b failingBackend) OpenSource(string) (Source, error) { return b.src, nil }

// A decode error is terminal: the view must stop claiming to play, freeze its
// clock, surface the error, and report it from transport calls instead of
// pretending a dead decoder can resume.
func TestVideoViewDecodeErrorConvergesToStateError(t *testing.T) {
	tick := virtualClock(t)
	boom := errors.New("decode exploded")
	src := &failingSource{
		track: &VideoTrackInfo{Width: 64, Height: 48, FPS: 60, Duration: time.Second},
		err:   boom,
		after: 2,
	}
	restore := swapBackend(t, failingBackend{src: src})
	defer restore()

	v := NewVideoView()
	defer v.Destroy()
	errc := make(chan error, 1)
	v.OnError(func(err error) {
		select {
		case errc <- err:
		default:
		}
	})
	if err := v.Open("ignored"); err != nil {
		t.Fatalf("Open: %v", err)
	}
	v.Play()
	var gotErr error
	if !driveTicks(v, tick, 20*time.Millisecond, 2*time.Second, func() bool {
		select {
		case gotErr = <-errc:
			return true
		default:
			return false
		}
	}) {
		t.Fatal("OnError never fired for a decode failure")
	}
	if !errors.Is(gotErr, boom) {
		t.Fatalf("OnError got %v, want %v", gotErr, boom)
	}
	if got := v.State(); got != StateError {
		t.Errorf("state after decode error = %v, want StateError", got)
	}
	for name, fn := range map[string]func() error{
		"Play":         v.Play,
		"SeekTo":       func() error { return v.SeekTo(0) },
		"Stop":         v.Stop,
		"StepForward":  v.StepForward,
		"StepBackward": v.StepBackward,
	} {
		if err := fn(); !errors.Is(err, boom) {
			t.Errorf("%s after decode error = %v, want %v", name, err, boom)
		}
	}

	// The clock must be frozen: a decode error must not keep advancing
	// Position (the audio renderer would too).
	pos := v.Position()
	tick(200 * time.Millisecond)
	v.Tick(time.Time{})
	if got := v.Position(); got != pos {
		t.Errorf("Position moved from %v to %v after a decode error", pos, got)
	}
}

// Destroy is terminal: the view is dead, not just emptied.
func TestVideoViewDestroyedIsClosed(t *testing.T) {
	restore := swapBackend(t, &fakeBackend{})
	defer restore()

	v := NewVideoView()
	if err := v.Open("a"); err != nil {
		t.Fatalf("Open: %v", err)
	}
	v.Destroy()
	v.Destroy() // idempotent

	for name, fn := range map[string]func() error{
		"Open":         func() error { return v.Open("b") },
		"Play":         v.Play,
		"Pause":        v.Pause,
		"Stop":         v.Stop,
		"SeekTo":       func() error { return v.SeekTo(0) },
		"StepForward":  v.StepForward,
		"StepBackward": v.StepBackward,
	} {
		if err := fn(); !errors.Is(err, ErrClosed) {
			t.Errorf("%s after Destroy = %v, want ErrClosed", name, err)
		}
	}
	if _, err := v.DecodeFrameAt(0); !errors.Is(err, ErrClosed) {
		t.Errorf("DecodeFrameAt after Destroy = %v, want ErrClosed", err)
	}
	if got := v.State(); got != StateIdle {
		t.Errorf("state after Destroy = %v, want StateIdle", got)
	}
}
