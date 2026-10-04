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
	return nil
}

func (s *fakeSource) OpenScrubDecoder() (ScrubDecoder, error) {
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
// returns a restore function. The package-private currentBackend is
// reachable because the test lives in `package media`.
func swapBackend(t *testing.T, b Backend) func() {
	t.Helper()
	prev := currentBackend
	currentBackend = b
	return func() { currentBackend = prev }
}

// virtualClock pins the wall clock used by wallClock so Tick-driven
// state machines step deterministically without real sleeps.
func virtualClock(t *testing.T) func(d time.Duration) {
	t.Helper()
	original := nowFunc
	t.Cleanup(func() { nowFunc = original })
	cur := time.Unix(0, 0)
	nowFunc = func() time.Time { return cur }
	return func(d time.Duration) { cur = cur.Add(d) }
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
