package htmlmedia

import (
	"errors"
	"math"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/qizhanchan/qui"
	"github.com/qizhanchan/qui/htmlcss"
	"github.com/qizhanchan/qui/media"
)

// Network states (HTMLMediaElement.networkState).
const (
	NetworkEmpty    = 0
	NetworkIdle     = 1
	NetworkLoading  = 2
	NetworkNoSource = 3
)

// Ready states (HTMLMediaElement.readyState).
const (
	HaveNothing     = 0
	HaveMetadata    = 1
	HaveCurrentData = 2
	HaveFutureData  = 3
	HaveEnoughData  = 4
)

// MediaError codes (HTMLMediaElement.error.code).
const (
	ErrCodeAborted        = 1
	ErrCodeNetwork        = 2
	ErrCodeDecode         = 3
	ErrCodeSrcUnsupported = 4
)

// MediaError is HTMLMediaElement.error.
type MediaError struct {
	Code    int
	Message string
}

// Play rejections, named after the DOMException play()'s promise rejects
// with.
var (
	ErrNotAllowed   = errors.New("NotAllowedError: play() failed because the user didn't interact with the document first")
	ErrNotSupported = errors.New("NotSupportedError: The element has no supported sources.")
	ErrAborted      = errors.New("AbortError: The play() request was interrupted by a new load request.")
)

// Loader is implemented by an htmlcss ResourceLoader (Options.Resources)
// that can open media resources. src is the attribute value as written;
// the loader resolves it against the document URL. OpenMedia blocks until
// the resource's headers are in (it runs on a background goroutine) and
// returns a source that keeps downloading as it is read.
type Loader interface {
	OpenMedia(src string) (media.ByteSource, error)
}

// AutoplayPolicy decides whether el may start playing with sound without a
// user gesture. A muted element may always autoplay (Chrome's policy).
// The default allows nothing unmuted; a browser installs its own (sticky
// user activation of the document).
var AutoplayPolicy = func(el *htmlcss.El) bool { return false }

// Element is the state of one <video> or <audio>.
type Element struct {
	el    *htmlcss.El
	video bool
	w     *mediaWidget
	win   *qui.Window // runs tasks while the widget is detached (Ensure)

	networkState int
	readyState   int
	paused       bool
	ended        bool
	seeking      bool
	showPoster   bool
	err          *MediaError
	currentSrc   string
	duration     float64
	pos          float64 // current time while not playing (seconds)
	startPos     float64 // seek requested before metadata arrived
	volume       float64
	muted        bool
	rate         float64
	defaultRate  float64
	vw, vh       int

	src    media.Source
	vv     *media.VideoView
	ap     *media.AudioPlayer
	bs     media.ByteSource
	mse    MediaSourceHandle // attached MediaSource (MSE), if any
	mseDur float64           // its duration (seconds; +Inf = unbounded)
	gen    int               // bumped by every load: stale loads drop their result
	sel    int               // index of the <source> candidate being tried
	fired  bool              // autoplay already considered for this load

	pendingPlay []func(error)
	lastTick    time.Time
	lastUpdate  time.Time
	listeners   []func(string)
	evq         []string
	evScheduled bool

	jobMu   sync.Mutex
	pending []func() // jobs posted before the element had a window
}

var (
	registry   = map[*htmlcss.El]*Element{}
	registryMu sync.Mutex
)

// For returns the media element state of el, or nil when el is not a
// <video> / <audio> (or its widget is not built yet; FlushPendingStyles
// builds it).
func For(el *htmlcss.El) *Element {
	registryMu.Lock()
	defer registryMu.Unlock()
	return registry[el]
}

// Ensure returns el's media element state, creating it when el is a
// <video> / <audio> whose widget is not built yet — an element a script
// created and never inserted (new Audio()) still plays. win is where its
// tasks run until it is attached. Returns nil for other tags.
func Ensure(el *htmlcss.El, win *qui.Window) *Element {
	if m := For(el); m != nil {
		if m.win == nil {
			m.win = win
		}
		return m
	}
	tag := el.Tag()
	if tag != "video" && tag != "audio" {
		return nil
	}
	m := newElement(el, tag == "video")
	m.win = win
	m.w = newMediaWidget(m)
	m.Load()
	return m
}

// OnElement calls fn for every media element of eng's document as it is
// created (and at once for those that exist). A script engine subscribes
// to wire each element's events. The returned func unsubscribes.
func OnElement(eng *htmlcss.StyleEngine, fn func(*Element)) (cancel func()) {
	registryMu.Lock()
	h := &hook{fn: fn}
	hooks[eng] = append(hooks[eng], h)
	var existing []*Element
	for el, m := range registry {
		if el.Engine() == eng {
			existing = append(existing, m)
		}
	}
	registryMu.Unlock()
	for _, m := range existing {
		fn(m)
	}
	return func() {
		registryMu.Lock()
		defer registryMu.Unlock()
		hs := hooks[eng]
		for i, x := range hs {
			if x == h {
				hooks[eng] = append(hs[:i], hs[i+1:]...)
				break
			}
		}
		if len(hooks[eng]) == 0 {
			delete(hooks, eng)
		}
	}
}

type hook struct{ fn func(*Element) }

var hooks = map[*htmlcss.StyleEngine][]*hook{}

func newElement(el *htmlcss.El, video bool) *Element {
	e := &Element{
		el:           el,
		video:        video,
		paused:       true,
		showPoster:   true,
		duration:     math.NaN(),
		volume:       1,
		rate:         1,
		defaultRate:  1,
		networkState: NetworkEmpty,
	}
	_, e.muted = el.Attr("muted")
	registryMu.Lock()
	registry[el] = e
	hs := append([]*hook(nil), hooks[el.Engine()]...)
	registryMu.Unlock()
	for _, h := range hs {
		h.fn(e)
	}
	return e
}

// OnEvent registers a listener for the element's events ("play",
// "timeupdate", …), called on the UI thread in spec order. A script engine
// turns them into DOM events.
func (e *Element) OnEvent(fn func(name string)) { e.listeners = append(e.listeners, fn) }

// fire queues a media element task that dispatches name: events reach
// listeners in order, after the state change that caused them, and a
// listener that calls play() inside loadedmetadata does not reorder the
// events already queued (HTML "queue a media element task").
func (e *Element) fire(name string) {
	e.evq = append(e.evq, name)
	if e.evScheduled {
		return
	}
	e.evScheduled = true
	e.post(e.flushEvents)
}

func (e *Element) flushEvents() {
	e.evScheduled = false
	for len(e.evq) > 0 {
		name := e.evq[0]
		e.evq = e.evq[1:]
		for _, fn := range e.listeners {
			fn(name)
		}
	}
}

// IsVideo reports a <video> (rather than an <audio>).
func (e *Element) IsVideo() bool { return e.video }

// post runs fn on the UI thread: through the window's job queue, or once
// the element is attached when it has no window yet.
func (e *Element) post(fn func()) {
	win := e.w.Window()
	if win == nil {
		win = e.win
	}
	if win != nil {
		win.PostJob(fn)
		return
	}
	e.jobMu.Lock()
	e.pending = append(e.pending, fn)
	e.jobMu.Unlock()
}

// runPending queues the jobs posted while the element had no window.
func (e *Element) runPending(win *qui.Window) {
	e.jobMu.Lock()
	jobs := e.pending
	e.pending = nil
	e.jobMu.Unlock()
	for _, fn := range jobs {
		win.PostJob(fn)
	}
}

// ---- Loading ----

// candidate is one resource the resource selection algorithm may try.
type candidate struct{ src, typ string }

// candidates lists the src attribute, or else the <source> children with
// a type the element can play.
func (e *Element) candidates() []candidate {
	if src, ok := e.el.Attr("src"); ok {
		return []candidate{{src: strings.TrimSpace(src)}}
	}
	var out []candidate
	for _, k := range e.el.Kids() {
		c, ok := k.(*htmlcss.El)
		if !ok || c.Tag() != "source" {
			continue
		}
		src, ok := c.Attr("src")
		if !ok || strings.TrimSpace(src) == "" {
			continue
		}
		typ, _ := c.Attr("type")
		if typ != "" && CanPlayType(typ) == "" {
			continue
		}
		out = append(out, candidate{strings.TrimSpace(src), typ})
	}
	return out
}

// Load runs the media element load algorithm: abort what is in flight,
// reset the state and select a resource again (HTMLMediaElement.load()).
func (e *Element) Load() {
	e.gen++
	for _, done := range e.pendingPlay {
		done(ErrAborted)
	}
	e.pendingPlay = nil
	if e.networkState == NetworkLoading || e.networkState == NetworkIdle {
		e.fire("abort")
	}
	if e.networkState != NetworkEmpty {
		e.fire("emptied")
		e.closePlayer()
		e.networkState = NetworkEmpty
		e.readyState = HaveNothing
		if !e.paused {
			e.paused = true
		}
		if e.seeking {
			e.seeking = false
		}
		e.pos, e.startPos = 0, 0
		if !math.IsNaN(e.duration) {
			e.duration = math.NaN()
			e.fire("durationchange")
		}
		e.vw, e.vh = 0, 0
	}
	e.rate = e.defaultRate
	e.err = nil
	e.ended = false
	e.showPoster = true
	e.fired = false
	e.sel = 0
	// The resource selection algorithm awaits a stable state: listeners a
	// script adds right after setting src still hear loadstart.
	gen := e.gen
	e.post(func() {
		if gen == e.gen {
			e.selectResource()
		}
	})
}

func (e *Element) selectResource() {
	cands := e.candidates()
	if len(cands) == 0 {
		e.networkState = NetworkEmpty
		if _, hasSrc := e.el.Attr("src"); hasSrc || e.hasSourceKids() {
			e.noSource("MEDIA_ELEMENT_ERROR: Empty src attribute")
		}
		return
	}
	e.networkState = NetworkNoSource
	e.w.Invalidate()
	e.fire("loadstart")
	if e.sel >= len(cands) {
		e.noSource("MEDIA_ELEMENT_ERROR: Format error")
		return
	}
	c := cands[e.sel]
	e.currentSrc = c.src
	if h := lookupObjectURL(c.src); h != nil {
		e.attachMediaSource(h)
		return
	}
	e.networkState = NetworkLoading
	if pre, _ := e.el.Attr("preload"); strings.EqualFold(pre, "none") && !e.autoplayAttr() && len(e.pendingPlay) == 0 {
		// preload=none: wait for play() or a seek before fetching.
		e.networkState = NetworkIdle
		e.fire("suspend")
		return
	}
	e.fetch(c.src)
}

func (e *Element) hasSourceKids() bool {
	for _, k := range e.el.Kids() {
		if c, ok := k.(*htmlcss.El); ok && c.Tag() == "source" {
			return true
		}
	}
	return false
}

func (e *Element) noSource(msg string) {
	e.err = &MediaError{Code: ErrCodeSrcUnsupported, Message: msg}
	e.networkState = NetworkNoSource
	e.fire("error")
	for _, done := range e.pendingPlay {
		done(ErrNotSupported)
	}
	e.pendingPlay = nil
	e.w.Invalidate()
}

// fetch opens src on a background goroutine.
func (e *Element) fetch(src string) {
	gen := e.gen
	var loader Loader
	if eng := e.el.Engine(); eng != nil {
		loader, _ = eng.Resources().(Loader)
	}
	go func() {
		var bs media.ByteSource
		var err error
		if loader != nil {
			bs, err = loader.OpenMedia(src)
		} else {
			bs, err = openLocal(src)
		}
		var ms media.Source
		netErr := err != nil
		if err == nil {
			ms, err = media.OpenStream(bs) // closes bs on failure
		}
		e.post(func() {
			if gen != e.gen {
				if ms != nil {
					_ = ms.Close()
				}
				return
			}
			if err != nil {
				e.loadFailed(err, netErr)
				return
			}
			e.install(ms, bs)
		})
	}()
}

// loadFailed tries the next <source>, or reports the error.
func (e *Element) loadFailed(err error, network bool) {
	if _, hasSrc := e.el.Attr("src"); !hasSrc && e.sel+1 < len(e.candidates()) {
		e.sel++
		c := e.candidates()[e.sel]
		e.currentSrc = c.src
		e.fetch(c.src)
		return
	}
	code := ErrCodeSrcUnsupported
	if network && e.readyState > HaveNothing {
		code = ErrCodeNetwork
	}
	msg := err.Error()
	if code == ErrCodeSrcUnsupported {
		msg = "MEDIA_ELEMENT_ERROR: Format error"
		if network {
			msg = "MEDIA_ELEMENT_ERROR: " + err.Error()
		}
	}
	e.err = &MediaError{Code: code, Message: msg}
	e.networkState = NetworkNoSource
	if code == ErrCodeNetwork {
		e.networkState = NetworkIdle
	}
	e.fire("error")
	for _, done := range e.pendingPlay {
		done(ErrNotSupported)
	}
	e.pendingPlay = nil
	e.w.Invalidate()
}

// install takes the opened source: metadata, then the data events, then
// autoplay.
func (e *Element) install(src media.Source, bs media.ByteSource) {
	e.src, e.bs = src, bs
	if vt := src.VideoTrack(); vt != nil && e.video {
		vv := media.NewVideoView()
		vv.SetFitMode(media.FitContain)
		if err := vv.OpenSource(src); err != nil {
			vv.Destroy()
			e.src = nil
			e.loadFailed(err, false)
			return
		}
		e.vv = vv
		e.vw, e.vh = vt.Width, vt.Height
		e.w.setVideo(vv)
		vv.OnEnded(func() { e.onEnded() })
	} else if src.AudioTrack() != nil {
		ap, err := media.NewAudioPlayer()
		if err == nil {
			err = ap.OpenSource(src)
		} else {
			_ = src.Close()
		}
		if err != nil {
			e.src = nil
			e.loadFailed(err, false)
			return
		}
		e.ap = ap
		ap.OnEnded(func() { e.post(e.onEnded) })
	} else {
		_ = src.Close()
		e.src = nil
		e.loadFailed(media.ErrNoTrack, false)
		return
	}
	e.applyVolume()
	e.applyRate()
	e.applyLoop()

	e.duration = e.playerDuration().Seconds()
	if e.mse != nil {
		e.duration = e.mseDur
	}
	e.readyState = HaveMetadata
	e.fire("durationchange")
	if e.vw > 0 {
		e.fire("resize")
	}
	e.fire("loadedmetadata")
	e.el.NaturalSizeChanged()
	if e.startPos > 0 {
		e.seekPlayer(e.startPos)
		e.pos = e.startPos
	}
	e.readyState = HaveCurrentData
	e.fire("loadeddata")
	e.readyState = HaveEnoughData
	e.fire("canplay")
	if !e.paused {
		e.startPlayer()
		e.fire("playing")
	}
	e.fire("canplaythrough")
	e.networkState = NetworkIdle
	for _, done := range e.pendingPlay {
		done(nil)
	}
	e.pendingPlay = nil
	if e.paused && e.autoplayAttr() && !e.fired {
		e.fired = true
		if e.autoplayAllowed(false) {
			e.paused = false
			e.showPoster = false
			e.fire("play")
			e.startPlayer()
			e.fire("playing")
		}
	}
	e.lastUpdate = time.Now()
	e.w.InvalidateLayout()
	e.w.Invalidate()
}

func (e *Element) autoplayAttr() bool {
	_, ok := e.el.Attr("autoplay")
	return ok
}

func (e *Element) autoplayAllowed(gesture bool) bool {
	return gesture || e.muted || e.volume == 0 || gestureActive(e.el) || AutoplayPolicy(e.el)
}

func (e *Element) closePlayer() {
	if e.vv != nil {
		e.w.setVideo(nil)
		e.vv.Destroy()
		e.vv = nil
	}
	if e.ap != nil {
		_ = e.ap.Close()
		e.ap = nil
	}
	if e.src != nil && e.vv == nil && e.ap == nil {
		// Owned by the player once installed; closed here only when no
		// player took it.
		e.src = nil
	}
	e.bs = nil
	if h := e.mse; h != nil {
		e.mse = nil
		h.Detach(e)
	}
}

// Destroy stops playback and releases the decoders (the element left the
// document for good).
func (e *Element) Destroy() {
	e.gen++
	e.closePlayer()
	registryMu.Lock()
	delete(registry, e.el)
	registryMu.Unlock()
}

// ---- Playback ----

// Play is HTMLMediaElement.play(). gesture reports that a user activation
// started it. done receives the promise's outcome (nil = resolved) on the
// UI thread, possibly synchronously.
func (e *Element) Play(gesture bool, done func(error)) {
	if done == nil {
		done = func(error) {}
	}
	if e.err != nil && e.err.Code == ErrCodeSrcUnsupported {
		done(ErrNotSupported)
		return
	}
	if !e.autoplayAllowed(gesture) {
		done(ErrNotAllowed)
		return
	}
	if gesture {
		gestureSeen(e.el)
	}
	if e.networkState == NetworkEmpty {
		e.Load()
	} else if e.networkState == NetworkIdle && e.src == nil && e.err == nil {
		// preload=none: fetch now.
		e.networkState = NetworkLoading
		e.fetch(e.currentSrc)
	}
	if e.ended && !e.loop() {
		e.setTime(0)
	}
	if e.paused {
		e.paused = false
		e.showPoster = false
		e.fire("play")
		if e.readyState < HaveFutureData {
			e.fire("waiting")
		} else {
			e.startPlayer()
			e.fire("playing")
		}
	}
	e.ended = false
	if e.readyState >= HaveFutureData {
		done(nil)
	} else {
		e.pendingPlay = append(e.pendingPlay, done)
	}
	e.lastUpdate = time.Now()
	e.w.Invalidate()
}

// Pause is HTMLMediaElement.pause().
func (e *Element) Pause() {
	if e.networkState == NetworkEmpty {
		e.Load()
	}
	if e.paused {
		return
	}
	e.pos = e.CurrentTime()
	e.paused = true
	e.stopPlayer()
	e.fire("timeupdate")
	e.fire("pause")
	for _, done := range e.pendingPlay {
		done(ErrAborted)
	}
	e.pendingPlay = nil
	e.w.Invalidate()
}

func (e *Element) startPlayer() {
	switch {
	case e.vv != nil:
		_ = e.vv.Play()
	case e.ap != nil:
		_ = e.ap.Play()
	}
}

func (e *Element) stopPlayer() {
	switch {
	case e.vv != nil:
		_ = e.vv.Pause()
	case e.ap != nil:
		_ = e.ap.Pause()
	}
}

func (e *Element) playerDuration() time.Duration {
	switch {
	case e.vv != nil:
		return e.vv.Duration()
	case e.ap != nil:
		return e.ap.Duration()
	}
	return 0
}

func (e *Element) playerPosition() time.Duration {
	switch {
	case e.vv != nil:
		return e.vv.Position()
	case e.ap != nil:
		return e.ap.Position()
	}
	return 0
}

func (e *Element) seekPlayer(t float64) {
	d := time.Duration(t * float64(time.Second))
	switch {
	case e.vv != nil:
		_ = e.vv.SeekTo(d)
	case e.ap != nil:
		_ = e.ap.SeekTo(d)
	}
}

// onEnded handles the player reaching the end (UI thread).
func (e *Element) onEnded() {
	if e.loop() || e.src == nil {
		return
	}
	e.pos = e.duration
	e.ended = true
	e.fire("timeupdate")
	if !e.paused {
		e.paused = true
		e.stopPlayer()
		e.fire("pause")
	}
	e.fire("ended")
	e.w.Invalidate()
}

func (e *Element) loop() bool {
	_, ok := e.el.Attr("loop")
	return ok
}

// tick runs every frame while playing: timeupdate every 250 ms, as Chrome.
func (e *Element) tick(now time.Time) {
	if e.paused || e.src == nil {
		return
	}
	if now.Sub(e.lastUpdate) >= 250*time.Millisecond {
		e.lastUpdate = now
		e.fire("timeupdate")
	}
	if e.ap != nil && !math.IsNaN(e.duration) && e.CurrentTime() >= e.duration && !e.loop() && !e.ended {
		e.onEnded()
	}
}

// ---- Attributes and properties ----

// CurrentTime is HTMLMediaElement.currentTime in seconds.
func (e *Element) CurrentTime() float64 {
	if e.src == nil {
		if e.readyState == HaveNothing {
			return e.startPos
		}
		return e.pos
	}
	if e.paused || e.seeking {
		return e.pos
	}
	t := e.playerPosition().Seconds()
	if !math.IsNaN(e.duration) && t > e.duration {
		t = e.duration
	}
	return t
}

// SetCurrentTime seeks (HTMLMediaElement.currentTime = t).
func (e *Element) SetCurrentTime(t float64) {
	if math.IsNaN(t) || math.IsInf(t, 0) {
		return
	}
	if e.readyState == HaveNothing {
		e.startPos = max(t, 0)
		if e.networkState == NetworkIdle && e.src == nil && e.err == nil && e.currentSrc != "" {
			e.networkState = NetworkLoading
			e.fetch(e.currentSrc)
		}
		return
	}
	e.setTime(t)
}

func (e *Element) setTime(t float64) {
	if !math.IsNaN(e.duration) {
		t = min(t, e.duration)
	}
	t = max(t, 0)
	e.showPoster = false
	e.ended = false
	e.seeking = true
	e.pos = t
	e.fire("seeking")
	e.seekPlayer(t)
	gen := e.gen
	// seeked follows on a later task, as in a browser.
	e.post(func() {
		if gen != e.gen || !e.seeking {
			return
		}
		e.seeking = false
		e.fire("timeupdate")
		e.fire("seeked")
		e.lastUpdate = time.Now()
	})
	e.w.Invalidate()
}

// FastSeek is HTMLMediaElement.fastSeek.
func (e *Element) FastSeek(t float64) { e.SetCurrentTime(t) }

// Duration is NaN until metadata is known.
func (e *Element) Duration() float64 { return e.duration }

func (e *Element) Paused() bool      { return e.paused }
func (e *Element) Ended() bool       { return e.ended }
func (e *Element) Seeking() bool     { return e.seeking }
func (e *Element) ReadyState() int   { return e.readyState }
func (e *Element) NetworkState() int { return e.networkState }

// Error is HTMLMediaElement.error (nil when none).
func (e *Element) Error() *MediaError { return e.err }

// CurrentSrc is the URL being played, as written in the markup.
func (e *Element) CurrentSrc() string { return e.currentSrc }

// VideoWidth / VideoHeight are the frame size (0 before metadata).
func (e *Element) VideoWidth() int  { return e.vw }
func (e *Element) VideoHeight() int { return e.vh }

func (e *Element) Volume() float64 { return e.volume }

// SetVolume sets the volume; values outside [0, 1] are an IndexSizeError.
func (e *Element) SetVolume(v float64) error {
	if math.IsNaN(v) || v < 0 || v > 1 {
		return errors.New("IndexSizeError: The volume provided is outside the range [0, 1].")
	}
	if v == e.volume {
		return nil
	}
	e.volume = v
	e.applyVolume()
	e.fire("volumechange")
	e.w.Invalidate()
	return nil
}

func (e *Element) Muted() bool { return e.muted }

func (e *Element) SetMuted(m bool) {
	if m == e.muted {
		return
	}
	e.muted = m
	e.applyVolume()
	e.fire("volumechange")
	if !m && !e.paused && !e.autoplayAllowed(false) {
		// Unmuting an autoplaying video without a user gesture pauses it
		// (Chrome).
		e.Pause()
	}
	e.w.Invalidate()
}

func (e *Element) applyVolume() {
	v := float32(e.volume)
	if e.muted {
		v = 0
	}
	switch {
	case e.vv != nil:
		e.vv.SetVolume(v)
	case e.ap != nil:
		e.ap.SetVolume(v)
	}
}

func (e *Element) PlaybackRate() float64        { return e.rate }
func (e *Element) DefaultPlaybackRate() float64 { return e.defaultRate }

// SetPlaybackRate sets the rate; Chrome refuses rates outside [1/16, 16]
// (0 pauses the clock without pausing the element).
func (e *Element) SetPlaybackRate(r float64) error {
	if math.IsNaN(r) || (r != 0 && (math.Abs(r) < 1.0/16 || math.Abs(r) > 16)) {
		return errors.New("NotSupportedError: The provided playback rate is not supported.")
	}
	if r == e.rate {
		return nil
	}
	e.rate = r
	e.applyRate()
	e.fire("ratechange")
	return nil
}

func (e *Element) SetDefaultPlaybackRate(r float64) {
	if r == e.defaultRate {
		return
	}
	e.defaultRate = r
	e.fire("ratechange")
}

func (e *Element) applyRate() {
	r := float32(max(e.rate, 0))
	switch {
	case e.vv != nil:
		e.vv.SetRate(r)
	case e.ap != nil:
		e.ap.SetRate(r)
	}
}

func (e *Element) applyLoop() {
	l := e.loop()
	switch {
	case e.vv != nil:
		e.vv.SetLoop(l)
	case e.ap != nil:
		e.ap.SetLoop(l)
	}
}

// Buffered is the downloaded time ranges (HTMLMediaElement.buffered).
func (e *Element) Buffered() []media.TimeRange {
	if bs, ok := e.src.(media.BufferedSource); ok && e.src != nil {
		return bs.Buffered()
	}
	return nil
}

// Seekable is [0, duration] once metadata is known.
func (e *Element) Seekable() []media.TimeRange {
	if e.readyState == HaveNothing || math.IsNaN(e.duration) {
		return nil
	}
	return []media.TimeRange{{Start: 0, End: e.duration}}
}

// attrChanged reacts to attribute changes.
func (e *Element) attrChanged(name string) {
	switch name {
	case "src":
		e.Load()
	case "loop":
		e.applyLoop()
	case "muted":
		// The muted attribute is the default only; it does not change the
		// muted state after parsing.
	case "controls", "poster":
		e.w.Invalidate()
	}
}

// ---- Autoplay gesture bookkeeping ----

var (
	gestureMu   sync.Mutex
	gestureDocs = map[*htmlcss.StyleEngine]bool{}
)

// gestureSeen records a user activation in el's document; later unmuted
// autoplay in it is allowed (a simplification of sticky activation).
func gestureSeen(el *htmlcss.El) {
	gestureMu.Lock()
	gestureDocs[el.Engine()] = true
	gestureMu.Unlock()
}

func gestureActive(el *htmlcss.El) bool {
	gestureMu.Lock()
	defer gestureMu.Unlock()
	return gestureDocs[el.Engine()]
}

// NoteUserActivation tells the package the user interacted with el's
// document (a click, a key press): unmuted playback is allowed there from
// now on. A browser calls it from its input handling.
func NoteUserActivation(el *htmlcss.El) { gestureSeen(el) }

// ---- Local files ----

// fileSource is a ByteSource over a local file.
type fileSource struct {
	*os.File
	size int64
}

func (f fileSource) Size() int64 { return f.size }

// openLocal opens a local path or file: URL (the fallback without a host
// loader).
func openLocal(src string) (media.ByteSource, error) {
	p := src
	if u, err := url.Parse(src); err == nil && u.Scheme == "file" {
		p = u.Path
	} else if err == nil && u.Scheme != "" && len(u.Scheme) > 1 {
		return nil, errors.New("htmlmedia: no loader for " + u.Scheme + ": URLs")
	}
	p = filepath.Clean(p)
	f, err := os.Open(p)
	if err != nil {
		return nil, err
	}
	st, err := f.Stat()
	if err != nil || st.IsDir() {
		f.Close()
		if err == nil {
			err = errors.New("htmlmedia: " + p + " is a directory")
		}
		return nil, err
	}
	return fileSource{f, st.Size()}, nil
}

// ---- canPlayType ----

// CanPlayType answers HTMLMediaElement.canPlayType for a MIME type with
// optional codecs parameter: "probably", "maybe" or "".
func CanPlayType(t string) string {
	t = strings.TrimSpace(strings.ToLower(t))
	if t == "" {
		return ""
	}
	mime, params, _ := strings.Cut(t, ";")
	mime = strings.TrimSpace(mime)
	caps := media.StreamSupported()
	container := ""
	switch mime {
	case "video/mp4", "audio/mp4", "video/x-m4v", "audio/x-m4a", "audio/m4a", "video/quicktime":
		container = "mp4"
	case "video/webm", "audio/webm":
		container = "webm"
	case "video/x-matroska", "audio/x-matroska", "video/mkv", "audio/mkv":
		container = "mkv"
	case "audio/ogg", "application/ogg", "audio/opus":
		container = "ogg"
	case "audio/mpeg", "audio/mp3", "audio/mpeg3", "audio/x-mpeg", "audio/x-mp3":
		container = "mp3"
	default:
		return ""
	}
	if !contains(caps.Containers, container) {
		return ""
	}
	if container == "mp3" && !contains(caps.AudioCodecs, "mp3") {
		return ""
	}
	codecs := ""
	for _, p := range strings.Split(params, ";") {
		k, v, ok := strings.Cut(strings.TrimSpace(p), "=")
		if ok && strings.TrimSpace(k) == "codecs" {
			codecs = strings.Trim(strings.TrimSpace(v), `"'`)
		}
	}
	if codecs == "" {
		if container == "mp3" || mime == "audio/opus" && contains(caps.AudioCodecs, "opus") {
			return "probably" // the type names the codec, like Chrome
		}
		return "maybe"
	}
	for _, c := range strings.Split(codecs, ",") {
		c = strings.TrimSpace(c)
		if !codecInContainer(c, container) || !codecSupported(c, caps) {
			return ""
		}
	}
	return "probably"
}

// codecInContainer reports whether container can carry codec c (so a
// WebM never claims H.264, nor an MP4 an MP3 the backend does not
// demux from it).
func codecInContainer(c, container string) bool {
	switch container {
	case "webm":
		return strings.HasPrefix(c, "av01") || strings.HasPrefix(c, "opus") || isVP9Codec(c)
	case "ogg":
		return strings.HasPrefix(c, "opus")
	case "mp3":
		return c == "mp3" || c == "mp4a.69" || c == "mp4a.6b" || c == "mp4a.40.34"
	case "mp4":
		return c != "mp3" && c != "mp4a.69" && c != "mp4a.6b" && !isVP9Codec(c)
	}
	return true // Matroska carries anything
}

// isVP9Codec reports a VP9 codec string ("vp9" or "vp09.PP.LL.DD…").
func isVP9Codec(c string) bool { return c == "vp9" || strings.HasPrefix(c, "vp09.") }

// codecSupported maps an RFC 6381 codec string onto the backend's codec
// names.
func codecSupported(c string, caps media.Capabilities) bool {
	switch {
	case strings.HasPrefix(c, "avc1") || strings.HasPrefix(c, "avc3"):
		return contains(caps.VideoCodecs, "h264")
	case strings.HasPrefix(c, "hvc1") || strings.HasPrefix(c, "hev1"):
		return contains(caps.VideoCodecs, "hevc")
	case strings.HasPrefix(c, "av01"):
		return contains(caps.VideoCodecs, "av1")
	case c == "vp9" || strings.HasPrefix(c, "vp09.00."):
		// Profile 0 (8-bit 4:2:0) only.
		return contains(caps.VideoCodecs, "vp9")
	case c == "mp4a.40.2" || c == "mp4a.40.5" || c == "mp4a.40.29" || c == "mp4a.40" || c == "mp4a.67":
		return contains(caps.AudioCodecs, "aac")
	case strings.HasPrefix(c, "opus"):
		return contains(caps.AudioCodecs, "opus")
	case c == "mp3" || c == "mp4a.69" || c == "mp4a.6b":
		return contains(caps.AudioCodecs, "mp3")
	case c == "flac" || c == "fLaC":
		return contains(caps.AudioCodecs, "flac")
	}
	return false
}

func contains(xs []string, x string) bool {
	for _, v := range xs {
		if v == x {
			return true
		}
	}
	return false
}

// El is the element this state belongs to.
func (e *Element) El() *htmlcss.El { return e.el }

// DestroyAll stops and releases every media element of eng's document —
// the document is going away (navigation, closed tab).
func DestroyAll(eng *htmlcss.StyleEngine) {
	registryMu.Lock()
	var ms []*Element
	for el, m := range registry {
		if el.Engine() == eng {
			ms = append(ms, m)
		}
	}
	delete(hooks, eng)
	registryMu.Unlock()
	for _, m := range ms {
		m.Destroy()
	}
	gestureMu.Lock()
	delete(gestureDocs, eng)
	gestureMu.Unlock()
}
