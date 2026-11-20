package qui

import (
	"fmt"
	"image"
	"log"
	"runtime"
	"runtime/debug"
	"sync"
	"sync/atomic"
	"time"
)

// Page threads (RFC 0005): a document's DOM, style, layout, script and
// paint run on a thread of their own instead of the window's UI thread, so
// a heavy page cannot freeze the browser chrome around it.
//
// A PageThread is one OS thread (a goroutine locked to it) that drives any
// number of PageHosts — the scheduling unit Chromium calls an agent group:
// pages of one browsing-context group and site share a thread, everything
// else gets its own (RFC 0005 §8.1). Each PageHost is a full qui.Window
// running on the embedded backend (page_host_platform.go): focus, overlays,
// tooltips, the job queue, animators, requestAnimationFrame, AfterLayout and
// agent introspection all work unchanged inside it. What it does not have is
// an OS window: its frames are committed to the host, which shows them in a
// PageView on the real window's UI thread.
//
// Threading contract:
//   - Everything that touches the page Window or its widgets runs on the
//     page thread: PageHost.Do, input sent with the Send* methods, and the
//     Window's own jobs (Window.PostJob still works from any goroutine).
//   - The host side (SetViewport, Send*, frame reads, Close) is safe from
//     any goroutine and never waits for the page thread — a page stuck in a
//     long script must not stall the UI thread that is showing it.

// PageThread is one page thread. See the file comment.
type PageThread struct {
	name string
	// sync marks a thread with no goroutine of its own: the caller pumps it
	// (Pump / RunUntilIdle). Tests use it for deterministic frames.
	sync bool

	mu        sync.Mutex
	tasks     []func()
	hosts     []*PageHost
	closing   bool
	wakeCh    chan struct{}
	done      chan struct{}
	tid       uint64
	busy      atomic.Bool
	busySince atomic.Int64
}

// NewPageThread starts a page thread. name labels it in diagnostics.
func NewPageThread(name string) *PageThread {
	t := &PageThread{name: name, wakeCh: make(chan struct{}, 1), done: make(chan struct{})}
	started := make(chan struct{})
	go t.loop(started)
	<-started
	return t
}

// NewSyncPageThread returns a page thread with no goroutine: the calling
// goroutine owns it and drives it with Pump or RunUntilIdle. The caller
// should hold its OS thread (runtime.LockOSThread) for as long as it uses
// the thread, because UI-thread ownership is per OS thread.
func NewSyncPageThread(name string) *PageThread {
	return &PageThread{name: name, sync: true, wakeCh: make(chan struct{}, 1),
		done: make(chan struct{}), tid: currentUIThreadID()}
}

// Name is the label given at creation.
func (t *PageThread) Name() string { return t.name }

// OnThread reports whether the caller is running on this page thread.
func (t *PageThread) OnThread() bool { return t != nil && t.tid == currentUIThreadID() }

// Busy reports whether the thread is inside a task or frame, and since
// when — the input to hang detection (RFC 0005 §3.6).
func (t *PageThread) Busy() (bool, time.Time) {
	if !t.busy.Load() {
		return false, time.Time{}
	}
	return true, time.Unix(0, t.busySince.Load())
}

func (t *PageThread) wake() {
	select {
	case t.wakeCh <- struct{}{}:
	default:
	}
}

// post queues fn for the page thread, in order with every other task.
func (t *PageThread) post(fn func()) {
	t.mu.Lock()
	if t.closing && !t.sync {
		t.mu.Unlock()
		return
	}
	t.tasks = append(t.tasks, fn)
	t.mu.Unlock()
	t.wake()
}

// Post runs fn on the page thread, after the tasks queued before it.
func (t *PageThread) Post(fn func()) {
	if t == nil || fn == nil {
		return
	}
	t.post(fn)
}

func (t *PageThread) loop(started chan struct{}) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	t.tid = currentUIThreadID()
	close(started)
	defer close(t.done)
	timer := time.NewTimer(time.Hour)
	timer.Stop()
	for {
		if !t.pumpOnce() {
			return
		}
		wait := t.nextWait(time.Now())
		if wait == pumpForever {
			<-t.wakeCh
			continue
		}
		timer.Reset(wait)
		select {
		case <-t.wakeCh:
			if !timer.Stop() {
				<-timer.C
			}
		case <-timer.C:
		}
	}
}

// pumpOnce runs the queued tasks and one frame of every host. It reports
// false once the thread has shut down.
func (t *PageThread) pumpOnce() bool {
	t.mu.Lock()
	tasks := t.tasks
	t.tasks = nil
	closing := t.closing
	t.mu.Unlock()
	t.busySince.Store(time.Now().UnixNano())
	t.busy.Store(true)
	defer t.busy.Store(false)
	for _, fn := range tasks {
		runPageTask(fn)
	}
	if closing {
		t.mu.Lock()
		hosts := t.hosts
		t.hosts = nil
		t.mu.Unlock()
		for _, h := range hosts {
			h.destroyOnThread()
		}
		return false
	}
	t.mu.Lock()
	hosts := append([]*PageHost(nil), t.hosts...)
	t.mu.Unlock()
	for _, h := range hosts {
		h.stepOnThread()
	}
	return true
}

func runPageTask(fn func()) {
	defer func() {
		if r := recover(); r != nil {
			logPagePanic("page task", r)
		}
	}()
	fn()
}

// logPagePanic reports a panic recovered on a page thread. The page is
// in-process, so the recovery is what keeps one broken page from taking the
// whole browser down (RFC 0005 P6 moves pages out of process for real).
func logPagePanic(where string, v any) {
	log.Printf("qui/page: panic in %s: %v\n%s", where, v, debug.Stack())
}

// nextWait is how long the thread may sleep: the shortest wait any of its
// windows asks for.
func (t *PageThread) nextWait(now time.Time) time.Duration {
	t.mu.Lock()
	defer t.mu.Unlock()
	if len(t.tasks) > 0 {
		return 0
	}
	wait := pumpForever
	for _, h := range t.hosts {
		if w := h.win; w != nil && w.plat != nil {
			d := w.nextFrameWait(now)
			if h.hidden.Load() && d < hiddenFrameInterval && w.PendingJobs() == 0 {
				// A hidden page animates and repaints at a crawl (RFC 0005
				// §3.7); its jobs (loads, script tasks) still run promptly.
				d = hiddenFrameInterval
			}
			if d < wait {
				wait = d
			}
		}
	}
	return wait
}

// hiddenFrameInterval paces a hidden page's frames.
const hiddenFrameInterval = 250 * time.Millisecond

// SetHidden marks the page hidden (a background tab) or shown. A hidden
// page keeps running its jobs but frames at a few per second. Any
// goroutine.
func (h *PageHost) SetHidden(hidden bool) {
	if h == nil || h.hidden.Swap(hidden) == hidden {
		return
	}
	h.pw.visible.Store(!hidden)
	h.thread.wake()
}

// Pump runs one iteration of a sync thread: queued tasks, then a frame of
// every host. No-op on a threaded PageThread.
func (t *PageThread) Pump() {
	if t == nil || !t.sync {
		return
	}
	t.pumpOnce()
}

// RunUntilIdle pumps a sync thread until no host has work due now (no
// queued task, no dirty frame, no pending job), or until limit iterations.
// Timers and animations that are due later do not keep it running. It
// returns the iterations it ran.
func (t *PageThread) RunUntilIdle(limit int) int {
	if t == nil || !t.sync {
		return 0
	}
	if limit <= 0 {
		limit = 100
	}
	for i := 0; i < limit; i++ {
		t.pumpOnce()
		if t.nextWait(time.Now()) > frameInterval {
			return i + 1
		}
	}
	return limit
}

// Close destroys every host on the thread and stops it. It waits for the
// thread to finish unless it is called on the thread itself.
func (t *PageThread) Close() {
	if t == nil {
		return
	}
	t.mu.Lock()
	if t.closing {
		t.mu.Unlock()
		return
	}
	t.closing = true
	t.mu.Unlock()
	t.wake()
	if t.sync {
		t.pumpOnce()
		close(t.done)
		return
	}
	if !t.OnThread() {
		<-t.done
	}
}

// PageHostConfig describes a page to create.
type PageHostConfig struct {
	Title string
	// Width and Height are the initial viewport in logical points; Scale is
	// the device pixel ratio (0 means 1). PageView keeps them current.
	Width, Height int
	Scale         float32
	// Setup runs on the page thread right after the page Window is created,
	// before its first frame: build the widget tree here.
	Setup func(w *Window)
}

// PageHost is one page: a qui.Window on the embedded backend, driven by a
// PageThread. See the file comment for the threading contract.
type PageHost struct {
	thread *PageThread
	pw     *embeddedWindow
	// win is written once on the page thread; host-side code must not
	// touch the Window, only post work to it.
	win *Window

	closed  atomic.Bool
	crashed atomic.Bool
	hidden  atomic.Bool

	// Commit store: the last presented frame, physical pixels. presentCPU
	// copies the damaged area in; the host side copies out of it under the
	// same lock (PageView keeps its own front buffer).
	frameMu sync.Mutex
	store   *image.RGBA
	pending image.Rectangle
	seq     uint64
	// layers are the scroll layers of the last commit (page_scroll_layer.go);
	// layersSig detects a change worth a commit of its own.
	layers    []ScrollLayerFrame
	layersSig string
	taken     uint64 // seq of the last TakeFrame
	meta      PageMeta
	// committedThisStep: presentCPU ran during the current Step.
	committedThisStep bool
	// Retained overlay draws (page_host_retained.go): the page thread's
	// sink state, and what the last commits hand the host.
	retained      retainedState
	draws         []PageRetainedDraw
	drawsSeq      uint64
	redrawPending bool
	forgetPending []any

	listenerMu sync.Mutex
	onFrame    func()
	onCursor   func(CursorShape)
	onTitle    func(string)
	onCrash    func()
}

// NewPageHost creates a page on t. It returns at once: the page Window is
// built on the page thread, after anything already queued there, and
// cfg.Setup runs then. Calls made on the host before that are queued
// behind it.
func (t *PageThread) NewPageHost(cfg PageHostConfig) *PageHost {
	h := &PageHost{thread: t}
	h.pw = newEmbeddedWindow(h, cfg.Width, cfg.Height, cfg.Scale)
	t.post(func() { h.createOnThread(cfg) })
	return h
}

func (h *PageHost) createOnThread(cfg PageHostConfig) {
	if h.closed.Load() {
		return
	}
	w, h2, _, _, _ := h.pw.geometry()
	win, err := newWindowFromConfig(&embeddedApp{host: h}, platformWindowConfig{Title: cfg.Title, Width: w, Height: h2})
	if err != nil {
		log.Printf("qui/page: create page window: %v", err)
		return
	}
	// The page Window belongs to this thread, checks or no checks: OnUIThread
	// must answer for the page thread, not the process main thread.
	win.uiThread.owner = currentUIThreadID()
	h.win = win
	h.thread.mu.Lock()
	h.thread.hosts = append(h.thread.hosts, h)
	h.thread.mu.Unlock()
	if cfg.Setup != nil {
		cfg.Setup(win)
	}
}

// Thread is the PageThread driving h.
func (h *PageHost) Thread() *PageThread { return h.thread }

// Window is the page Window. Page thread only — from anywhere else use Do.
func (h *PageHost) Window() *Window { return h.win }

// Do runs fn with the page Window on the page thread, in order with input
// and every other task posted to the host.
func (h *PageHost) Do(fn func(w *Window)) {
	if h == nil || fn == nil {
		return
	}
	h.thread.post(func() {
		if h.win != nil && !h.closed.Load() {
			fn(h.win)
		}
	})
}

// SetViewport sets the page's logical size and device pixel ratio. Safe
// from any goroutine; the page lays out at the new size on its next frame.
func (h *PageHost) SetViewport(width, height int, scale float32) {
	if h == nil || !h.pw.setGeometry(width, height, scale) {
		return
	}
	h.thread.post(func() {
		if hd := h.handler(); hd != nil {
			w, ht, fw, fh, _ := h.pw.geometry()
			hd.onResize(w, ht)
			hd.onFramebufferResize(fw, fh)
		}
	})
}

// Viewport reports the size and scale last given to SetViewport.
func (h *PageHost) Viewport() (width, height int, scale float32) {
	w, ht, _, _, s := h.pw.geometry()
	return w, ht, s
}

func (h *PageHost) handler() platformHandler {
	if h.win == nil || h.closed.Load() {
		return nil
	}
	return h.pw.handler
}

// sendInput queues a platform event for the page thread.
func (h *PageHost) sendInput(fn func(hd platformHandler)) {
	if h == nil || h.closed.Load() {
		return
	}
	h.thread.post(func() {
		if hd := h.handler(); hd != nil {
			fn(hd)
		}
	})
}

// SendMouseMove delivers a pointer move at page-logical (x, y).
func (h *PageHost) SendMouseMove(x, y float64, mods Modifiers) {
	h.sendInput(func(hd platformHandler) {
		h.pw.cursorX, h.pw.cursorY, h.pw.mods = x, y, mods
		hd.onMouseMove(x, y, mods)
	})
}

// SendMouseButton delivers a press or release at page-logical (x, y).
func (h *PageHost) SendMouseButton(btn MouseButton, down bool, x, y float64, mods Modifiers) {
	h.sendInput(func(hd platformHandler) {
		if h.pw.cursorX != x || h.pw.cursorY != y {
			h.pw.cursorX, h.pw.cursorY = x, y
			hd.onMouseMove(x, y, mods)
		}
		h.pw.mods = mods
		hd.onMouseButton(btn, down, mods)
	})
}

// SendScroll delivers a wheel or trackpad scroll at page-logical (x, y);
// dx, dy are logical points as the host window received them.
func (h *PageHost) SendScroll(x, y, dx, dy float64, mods Modifiers, phase GesturePhase, precise bool) {
	h.sendInput(func(hd platformHandler) {
		h.pw.cursorX, h.pw.cursorY, h.pw.mods = x, y, mods
		hd.onScroll(x, y, dx, dy, mods, phase, precise)
	})
}

// SendGesture delivers a pinch / rotate / smart-magnify step.
func (h *PageHost) SendGesture(kind EventType, phase GesturePhase, dScale, dRotation float32, mods Modifiers) {
	h.sendInput(func(hd platformHandler) { hd.onGesture(kind, phase, dScale, dRotation, mods) })
}

// SendKey delivers a key press or release.
func (h *PageHost) SendKey(k Key, scancode int, down, repeat bool, mods Modifiers) {
	h.sendInput(func(hd platformHandler) {
		h.pw.mods = mods
		hd.onKey(k, scancode, down, repeat, mods)
	})
}

// SendChar delivers typed text, one rune.
func (h *PageHost) SendChar(r rune) {
	h.sendInput(func(hd platformHandler) { hd.onChar(r) })
}

// SendFocus tells the page whether it has keyboard focus.
func (h *PageHost) SendFocus(focused bool) {
	h.sendInput(func(hd platformHandler) { hd.onFocus(focused) })
}

// SendPreedit and SendCommitText forward input-method composition.
func (h *PageHost) SendPreedit(text string, cursor int) {
	h.sendInput(func(hd platformHandler) { hd.onPreedit(text, cursor) })
}

func (h *PageHost) SendCommitText(text string) {
	h.sendInput(func(hd platformHandler) { hd.onCommitText(text) })
}

// SendFileDrop delivers files dropped at page-logical (x, y).
func (h *PageHost) SendFileDrop(paths []string, x, y float64) {
	h.sendInput(func(hd platformHandler) { hd.onFileDrop(paths, x, y) })
}

// OnFrame registers fn to run after each commit. It runs on the page
// thread and must not block: post to the host window and return.
func (h *PageHost) OnFrame(fn func()) {
	h.listenerMu.Lock()
	h.onFrame = fn
	h.listenerMu.Unlock()
}

// OnCursor registers fn to run when the page's pointer shape changes (page
// thread; must not block).
func (h *PageHost) OnCursor(fn func(CursorShape)) {
	h.listenerMu.Lock()
	h.onCursor = fn
	h.listenerMu.Unlock()
}

// OnTitle registers fn to run when the page Window's title changes (page
// thread; must not block).
func (h *PageHost) OnTitle(fn func(string)) {
	h.listenerMu.Lock()
	h.onTitle = fn
	h.listenerMu.Unlock()
}

// Cursor is the pointer shape the page last asked for.
func (h *PageHost) Cursor() CursorShape { return CursorShape(h.pw.cursorShape.Load()) }

func (h *PageHost) noteCursor(shape CursorShape) {
	h.listenerMu.Lock()
	fn := h.onCursor
	h.listenerMu.Unlock()
	if fn != nil {
		fn(shape)
	}
}

func (h *PageHost) noteTitle(title string) {
	h.listenerMu.Lock()
	fn := h.onTitle
	h.listenerMu.Unlock()
	if fn != nil {
		fn(title)
	}
}

// commitPixels is presentCPU: copy the damaged area of the finished frame
// into the store and tell the host. Page thread.
func (h *PageHost) commitPixels(img *image.RGBA, damage image.Rectangle) {
	if img == nil {
		return
	}
	b := img.Bounds()
	h.frameMu.Lock()
	if h.retained.redraw && h.store != nil && h.store.Bounds() == b {
		// A redraw-only frame: the pixels did not change, the host replays
		// the retained draws.
		h.takeRetainedLocked()
		h.publishLayersLocked()
		h.seq++
		h.committedThisStep = true
		h.frameMu.Unlock()
		h.notifyFrame()
		return
	}
	if h.store == nil || h.store.Bounds() != b {
		h.store = image.NewRGBA(b)
		damage = b
	}
	if damage.Empty() {
		damage = b
	}
	damage = damage.Intersect(b)
	copyRGBARect(h.store, img, damage)
	h.pending = h.pending.Union(damage)
	h.publishLayersLocked()
	h.takeRetainedLocked()
	h.seq++
	h.committedThisStep = true
	h.frameMu.Unlock()
	h.notifyFrame()
}

// publishLayersLocked snapshots the page's scroll layers into the commit.
// Page thread, frameMu held.
func (h *PageHost) publishLayersLocked() {
	if h.win == nil {
		return
	}
	h.layers = h.win.collectScrollLayers()
	h.layersSig = scrollLayersSig(h.layers)
	h.meta = h.win.pageMeta()
}

// PageMeta is page state the host acts on without asking the page.
type PageMeta struct {
	// IMEActive: the page's focused widget takes input-method composition;
	// Caret is its caret, page-logical, for the candidate window.
	IMEActive bool
	Caret     Rect
}

// pageMeta snapshots w's PageMeta. Page thread.
func (w *Window) pageMeta() PageMeta {
	var m PageMeta
	if _, ok := imeClientOf(w.focused); ok {
		m.IMEActive = true
		m.Caret = w.CaretRect()
	}
	return m
}

func (h *PageHost) notifyFrame() {
	h.listenerMu.Lock()
	fn := h.onFrame
	h.listenerMu.Unlock()
	if fn != nil {
		fn()
	}
}

// commitLayersIfChanged commits scroll layers on their own after a Step
// that painted nothing — tiles a raster worker finished, a scroller that
// appeared — so the host can scroll into them. Page thread.
func (h *PageHost) commitLayersIfChanged() {
	if h.win == nil {
		return
	}
	st := &h.retained
	layers := h.win.collectScrollLayers()
	sig := scrollLayersSig(layers)
	h.frameMu.Lock()
	meta := h.win.pageMeta()
	if h.store == nil || sig == h.layersSig && meta == h.meta && !st.changed && !st.redraw && len(st.forget) == 0 {
		h.frameMu.Unlock()
		return
	}
	h.layers, h.layersSig, h.meta = layers, sig, meta
	h.takeRetainedLocked()
	h.seq++
	h.frameMu.Unlock()
	h.notifyFrame()
}

func scrollLayersSig(ls []ScrollLayerFrame) string {
	var b []byte
	for _, l := range ls {
		b = fmt.Appendf(b, "%p|%v|%v|%v|%v|%d|%v|%d|%d;", l.Key, l.Viewport, l.Offset, l.Max, l.Composited,
			len(l.Tiles), l.Origin, l.Applied, len(l.Covers))
	}
	return string(b)
}

// SetScrollLayerOffset tells the page that the host scrolled the layer key
// (a ScrollLayerFrame.Key) to the logical offsets (x, y). seq orders the
// requests; commits report the last one applied (ScrollLayerFrame.Applied).
func (h *PageHost) SetScrollLayerOffset(key any, x, y float32, seq uint64) {
	if h == nil || h.closed.Load() {
		return
	}
	h.thread.post(func() {
		if w := h.win; w != nil && !h.closed.Load() {
			w.applyScrollLayerOffset(key, x, y, seq)
		}
	})
}

// PageFrameUpdate is what TakeFrame hands the host.
type PageFrameUpdate struct {
	// Image is the host's copy of the frame (the buffer passed in, or a
	// new one when the size changed); Changed the device pixels that
	// differ from what it held before.
	Image   *image.RGBA
	Changed image.Rectangle
	// Seq numbers commits; Layers are the scroll layers of the latest.
	Seq    uint64
	Layers []ScrollLayerFrame
	// Draws are the committed overlay draws (DrawsSeq changes with them);
	// Redraw asks the host to replay them; Forget lists keys whose host
	// textures can go (page_host_retained.go).
	Draws    []PageRetainedDraw
	DrawsSeq uint64
	Redraw   bool
	Forget   []any
	Meta     PageMeta
}

// TakeFrame copies what changed since the last TakeFrame into dst (which
// it reallocates when the frame size changed) and returns it with the
// latest scroll layers. ok is false when nothing was committed since the
// last call. Safe from any goroutine; holds the commit lock only for the
// copy.
func (h *PageHost) TakeFrame(dst *image.RGBA) (u PageFrameUpdate, ok bool) {
	h.frameMu.Lock()
	defer h.frameMu.Unlock()
	u = PageFrameUpdate{Image: dst, Seq: h.seq, Layers: h.layers, Draws: h.draws, DrawsSeq: h.drawsSeq, Meta: h.meta}
	if h.store == nil || h.seq == h.taken {
		return u, false
	}
	h.taken = h.seq
	u.Redraw, h.redrawPending = h.redrawPending, false
	u.Forget, h.forgetPending = h.forgetPending, nil
	b := h.store.Bounds()
	if dst == nil || dst.Bounds() != b {
		dst = image.NewRGBA(b)
		MarkImageVolatile(dst)
		h.pending = b
	}
	u.Image = dst
	if !h.pending.Empty() {
		u.Changed = h.pending
		copyRGBARect(dst, h.store, u.Changed)
		h.pending = image.Rectangle{}
	}
	return u, true
}

// FrameSeq is the number of frames committed so far.
func (h *PageHost) FrameSeq() uint64 {
	h.frameMu.Lock()
	defer h.frameMu.Unlock()
	return h.seq
}

// Snapshot returns a copy of the last committed frame (nil before the
// first). Safe from any goroutine.
func (h *PageHost) Snapshot() *image.RGBA {
	h.frameMu.Lock()
	defer h.frameMu.Unlock()
	if h.store == nil {
		return nil
	}
	out := image.NewRGBA(h.store.Bounds())
	copy(out.Pix, h.store.Pix)
	return out
}

func copyRGBARect(dst, src *image.RGBA, r image.Rectangle) {
	r = r.Intersect(dst.Bounds()).Intersect(src.Bounds())
	if r.Empty() {
		return
	}
	n := r.Dx() * 4
	for y := r.Min.Y; y < r.Max.Y; y++ {
		di := dst.PixOffset(r.Min.X, y)
		si := src.PixOffset(r.Min.X, y)
		copy(dst.Pix[di:di+n], src.Pix[si:si+n])
	}
}

// stepOnThread runs one frame of the page Window.
func (h *PageHost) stepOnThread() {
	w := h.win
	if w == nil || w.plat == nil || h.closed.Load() {
		return
	}
	if w.ShouldClose() && !w.closeVetoed() {
		h.destroyOnThread()
		return
	}
	w.pollLifecycle()
	h.syncRetainedSink()
	h.frameMu.Lock()
	h.committedThisStep = false
	h.frameMu.Unlock()
	defer func() {
		if r := recover(); r != nil {
			// A frame that panicked half-way leaves the Window in no state to
			// run another: show the last good frame and stop (a "sad tab").
			logPagePanic("page frame", r)
			w.inStep = false
			h.crashed.Store(true)
			h.closed.Store(true)
			h.thread.mu.Lock()
			for i, x := range h.thread.hosts {
				if x == h {
					h.thread.hosts = append(h.thread.hosts[:i:i], h.thread.hosts[i+1:]...)
					break
				}
			}
			h.thread.mu.Unlock()
			h.listenerMu.Lock()
			fn := h.onCrash
			h.listenerMu.Unlock()
			if fn != nil {
				fn()
			}
		}
	}()
	w.Step()
	h.frameMu.Lock()
	painted := h.committedThisStep
	h.frameMu.Unlock()
	if !painted {
		h.commitLayersIfChanged()
	}
}

// Crashed reports whether the page stopped after a panic.
func (h *PageHost) Crashed() bool { return h != nil && h.crashed.Load() }

// OnCrash registers fn to run (on the page thread) if the page panics.
func (h *PageHost) OnCrash(fn func()) {
	h.listenerMu.Lock()
	h.onCrash = fn
	h.listenerMu.Unlock()
}

// Close destroys the page. Safe from any goroutine; the Window is torn
// down on the page thread.
func (h *PageHost) Close() {
	if h == nil || !h.closed.CompareAndSwap(false, true) {
		return
	}
	h.thread.post(h.destroyOnThread)
}

// Closed reports whether Close ran.
func (h *PageHost) Closed() bool { return h == nil || h.closed.Load() }

func (h *PageHost) destroyOnThread() {
	h.closed.Store(true)
	t := h.thread
	t.mu.Lock()
	for i, x := range t.hosts {
		if x == h {
			t.hosts = append(t.hosts[:i:i], t.hosts[i+1:]...)
			break
		}
	}
	t.mu.Unlock()
	if w := h.win; w != nil && w.plat != nil {
		w.Destroy()
	}
}

// FramesClose reports whether two frames match up to the antialiasing
// noise render order causes: the glyph mask cache buckets sub-pixel
// positions and rasterizes a bucket at the first requester's exact
// fraction, so pages painted in another order can differ by a level or two
// in text edges. It returns the number of pixels further apart than tol in
// any channel.
func FramesClose(a, b *image.RGBA, tol uint8) (bad int, ok bool) {
	if a == nil || b == nil || a.Bounds() != b.Bounds() {
		return -1, false
	}
	for i := 0; i+3 < len(a.Pix); i += 4 {
		for c := 0; c < 4; c++ {
			x, y := a.Pix[i+c], b.Pix[i+c]
			if x > y && x-y > tol || y > x && y-x > tol {
				bad++
				break
			}
		}
	}
	return bad, bad == 0
}

// TakeFrameLayersForTest returns the scroll layers of the latest commit
// and its sequence number, without consuming the frame.
func (h *PageHost) TakeFrameLayersForTest() ([]ScrollLayerFrame, uint64) {
	h.frameMu.Lock()
	defer h.frameMu.Unlock()
	return h.layers, h.seq
}
