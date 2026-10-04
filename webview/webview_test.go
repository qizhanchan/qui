package webview

import (
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/qizhanchan/qui"
)

// fakeBackend stands in for the real platform backend during tests.
// Records every interaction so tests can assert on call ordering,
// coordinate translation, etc. Safe for concurrent use.
type fakeBackend struct {
	mu        sync.Mutex
	tickCount int32
	pages     []*fakePage
	newErr    error
}

func (b *fakeBackend) Capabilities() Capabilities {
	return Capabilities{WebGL: true, WebRTC: true, Video: true, JSBridge: true}
}

func (b *fakeBackend) NewPage(cfg PageConfig) (Page, error) {
	if b.newErr != nil {
		return nil, b.newErr
	}
	p := &fakePage{cfg: cfg, handlers: map[string]MessageHandler{}}
	b.mu.Lock()
	b.pages = append(b.pages, p)
	b.mu.Unlock()
	return p, nil
}

func (b *fakeBackend) Tick() {
	atomic.AddInt32(&b.tickCount, 1)
}

func (b *fakeBackend) TickCount() int { return int(atomic.LoadInt32(&b.tickCount)) }

// fakePage records every call. Backend tests can replay scripted
// frames via setFrame; widget tests can assert on Inject* calls.
type fakePage struct {
	mu             sync.Mutex
	cfg            PageConfig
	url            string
	title          string
	loadState      LoadState
	caret          qui.Rect
	destroyed      bool
	mouseMoves     []mouseMove
	mouseButtons   []mouseButton
	scrolls        []scroll
	keys           []keyInject
	chars          []charInject
	devToolsOpen   int
	focusCalls     []bool
	preedits       []preedit
	commits        []string
	resizes        []resizeCall
	loadURLs       []string
	handlers       map[string]MessageHandler
	navListener    func(NavigationEvent)
	loadListener   func(LoadEvent)
	titleListener  func(string)
	cursorListener func(CursorKind)
	consoleListen  func(int, string)

	// scripted frame state — tests call setFrame to enqueue.
	pendingTex    uint32
	pendingW      int
	pendingH      int
	pendingHasNew bool
	pendingDirty  qui.Rect
	frameReleases int

	// Phase C scripted EvaluateJS state.
	evalScripts []string
	evalValue   JSValue
	evalErr     error
}

type mouseMove struct{ X, Y float32 }
type mouseButton struct {
	X, Y       float32
	Btn        qui.MouseButton
	Down       bool
	ClickCount int
}
type scroll struct {
	X, Y, DX, DY float32
}
type keyInject struct {
	Key      qui.Key
	ScanCode int
	Down     bool
}
type charInject struct {
	R rune
}
type preedit struct {
	Text   string
	Cursor int
}
type resizeCall struct {
	W, H  int
	Scale float32
}

func (p *fakePage) checkClosed() error {
	if p.destroyed {
		return ErrClosed
	}
	return nil
}

func (p *fakePage) LoadURL(url string) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if err := p.checkClosed(); err != nil {
		return err
	}
	p.url = url
	p.loadURLs = append(p.loadURLs, url)
	return nil
}
func (p *fakePage) LoadHTML(string, string) error { return p.checkClosed() }
func (p *fakePage) Reload() error                 { return p.checkClosed() }
func (p *fakePage) StopLoad() error               { return p.checkClosed() }
func (p *fakePage) GoBack() error                 { return p.checkClosed() }
func (p *fakePage) GoForward() error              { return p.checkClosed() }

func (p *fakePage) Resize(w, h int, scale float32) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if err := p.checkClosed(); err != nil {
		return err
	}
	p.resizes = append(p.resizes, resizeCall{W: w, H: h, Scale: scale})
	return nil
}

func (p *fakePage) AcquireFrame() (uint32, int, int, qui.Rect, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.pendingTex, p.pendingW, p.pendingH, p.pendingDirty, p.pendingHasNew
}

func (p *fakePage) ReleaseFrame() {
	p.mu.Lock()
	p.frameReleases++
	p.pendingHasNew = false
	p.mu.Unlock()
}

func (p *fakePage) setFrame(tex uint32, w, h int, dirty qui.Rect) {
	p.mu.Lock()
	p.pendingTex = tex
	p.pendingW = w
	p.pendingH = h
	p.pendingDirty = dirty
	p.pendingHasNew = true
	p.mu.Unlock()
}

func (p *fakePage) InjectMouseMove(x, y float32, _ qui.Modifiers) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if err := p.checkClosed(); err != nil {
		return err
	}
	p.mouseMoves = append(p.mouseMoves, mouseMove{X: x, Y: y})
	return nil
}

func (p *fakePage) InjectMouseButton(x, y float32, btn qui.MouseButton, down bool, cc int, _ qui.Modifiers) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if err := p.checkClosed(); err != nil {
		return err
	}
	p.mouseButtons = append(p.mouseButtons, mouseButton{X: x, Y: y, Btn: btn, Down: down, ClickCount: cc})
	return nil
}

func (p *fakePage) InjectScroll(x, y, dx, dy float32, _ qui.Modifiers) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if err := p.checkClosed(); err != nil {
		return err
	}
	p.scrolls = append(p.scrolls, scroll{X: x, Y: y, DX: dx, DY: dy})
	return nil
}

func (p *fakePage) InjectKey(key qui.Key, sc int, down bool, _ qui.Modifiers) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if err := p.checkClosed(); err != nil {
		return err
	}
	p.keys = append(p.keys, keyInject{Key: key, ScanCode: sc, Down: down})
	return nil
}

func (p *fakePage) InjectChar(r rune, _ qui.Modifiers) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if err := p.checkClosed(); err != nil {
		return err
	}
	p.chars = append(p.chars, charInject{R: r})
	return nil
}

func (p *fakePage) OpenDevTools() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if err := p.checkClosed(); err != nil {
		return err
	}
	p.devToolsOpen++
	return nil
}

func (p *fakePage) InjectFocus(focused bool) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if err := p.checkClosed(); err != nil {
		return err
	}
	p.focusCalls = append(p.focusCalls, focused)
	return nil
}

func (p *fakePage) InjectIMEPreedit(text string, cursor int) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if err := p.checkClosed(); err != nil {
		return err
	}
	p.preedits = append(p.preedits, preedit{Text: text, Cursor: cursor})
	return nil
}

func (p *fakePage) InjectIMECommit(text string) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if err := p.checkClosed(); err != nil {
		return err
	}
	p.commits = append(p.commits, text)
	return nil
}

func (p *fakePage) CaretRect() qui.Rect {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.caret
}

func (p *fakePage) EvaluateJS(script string) (JSValue, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if err := p.checkClosed(); err != nil {
		return JSValue{}, err
	}
	p.evalScripts = append(p.evalScripts, script)
	if p.evalErr != nil {
		return JSValue{}, p.evalErr
	}
	if p.evalValue.Kind == JSKindNull && p.evalValue.String == "" && p.evalValue.Number == 0 {
		// Default for tests that don't override: a "ok" string lets a
		// caller assert SOMETHING came back without the fake having to
		// be configured.
		return JSValue{Kind: JSKindString, String: "ok"}, nil
	}
	return p.evalValue, nil
}

func (p *fakePage) RegisterHandler(name string, fn MessageHandler) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if err := p.checkClosed(); err != nil {
		return err
	}
	p.handlers[name] = fn
	return nil
}

func (p *fakePage) UnregisterHandler(name string) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if err := p.checkClosed(); err != nil {
		return err
	}
	delete(p.handlers, name)
	return nil
}

func (p *fakePage) SetNavigationListener(fn func(NavigationEvent)) {
	p.mu.Lock()
	p.navListener = fn
	p.mu.Unlock()
}
func (p *fakePage) SetLoadListener(fn func(LoadEvent)) {
	p.mu.Lock()
	p.loadListener = fn
	p.mu.Unlock()
}
func (p *fakePage) SetTitleListener(fn func(string)) {
	p.mu.Lock()
	p.titleListener = fn
	p.mu.Unlock()
}
func (p *fakePage) SetCursorListener(fn func(CursorKind)) {
	p.mu.Lock()
	p.cursorListener = fn
	p.mu.Unlock()
}
func (p *fakePage) SetConsoleListener(fn func(int, string)) {
	p.mu.Lock()
	p.consoleListen = fn
	p.mu.Unlock()
}

func (p *fakePage) URL() string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.url
}
func (p *fakePage) Title() string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.title
}
func (p *fakePage) State() LoadState {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.loadState
}

func (p *fakePage) Destroy() {
	p.mu.Lock()
	p.destroyed = true
	p.mu.Unlock()
}

// withFakeBackend installs a fakeBackend for the duration of t, then
// restores the previous backend (typically nil on test platforms).
func withFakeBackend(t *testing.T) *fakeBackend {
	t.Helper()
	prev := currentBackend
	b := &fakeBackend{}
	setBackend(b)
	t.Cleanup(func() { setBackend(prev) })
	return b
}

// withNoBackend forces currentBackend to nil for the test, so the
// "no backend registered" code path can be exercised even under
// build tags that auto-register a real backend (`-tags webview_cef`
// on darwin). Restores on cleanup.
func withNoBackend(t *testing.T) {
	t.Helper()
	prev := currentBackend
	setBackend(nil)
	t.Cleanup(func() { setBackend(prev) })
}

// -----------------------------------------------------------------------------
// Tests
// -----------------------------------------------------------------------------

func TestNewWebViewNoBackendReturnsNotSupported(t *testing.T) {
	withNoBackend(t)
	v := NewWebView(Config{InitialURL: "about:blank"})
	if err := v.Open(); !errors.Is(err, ErrNotSupported) {
		t.Errorf("Open() with no backend = %v; want ErrNotSupported", err)
	}
}

func TestOpenCreatesPage(t *testing.T) {
	b := withFakeBackend(t)
	v := NewWebView(Config{InitialURL: "https://example.com"})
	if err := v.Open(); err != nil {
		t.Fatalf("Open() error: %v", err)
	}
	if len(b.pages) != 1 {
		t.Fatalf("backend pages = %d; want 1", len(b.pages))
	}
	if got := b.pages[0].cfg.InitialURL; got != "https://example.com" {
		t.Errorf("page InitialURL = %q; want https://example.com", got)
	}
	if v.URL() != "https://example.com" {
		t.Errorf("v.URL() = %q; want https://example.com", v.URL())
	}
}

func TestOpenIdempotent(t *testing.T) {
	b := withFakeBackend(t)
	v := NewWebView(Config{InitialURL: "x"})
	if err := v.Open(); err != nil {
		t.Fatal(err)
	}
	if err := v.Open(); err != nil {
		t.Errorf("second Open should be no-op, got %v", err)
	}
	if len(b.pages) != 1 {
		t.Errorf("backend pages = %d; want 1 (second Open should NOT create)", len(b.pages))
	}
}

func TestDestroyIdempotentAndClosesPage(t *testing.T) {
	b := withFakeBackend(t)
	v := NewWebView(Config{})
	_ = v.Open()
	v.Destroy()
	if !b.pages[0].destroyed {
		t.Error("page not destroyed after Destroy()")
	}
	// Second Destroy must not panic.
	v.Destroy()
	if err := v.LoadURL("http://x"); !errors.Is(err, ErrClosed) {
		t.Errorf("LoadURL after Destroy = %v; want ErrClosed", err)
	}
}

func TestTickCallsBackendAndPromotesFrame(t *testing.T) {
	b := withFakeBackend(t)
	v := NewWebView(Config{})
	_ = v.Open()
	page := b.pages[0]
	page.setFrame(42, 800, 600, qui.Rect{W: 800, H: 600})

	// No bounds yet → Tick returns zero (no frame visible).
	v.Layout(qui.Rect{X: 0, Y: 0, W: 800, H: 600})
	dirty := v.Tick(timeNow())
	if dirty.IsEmpty() {
		t.Error("Tick should report bounds dirty when a frame is on screen")
	}
	if b.TickCount() != 1 {
		t.Errorf("Backend.Tick called %d times; want 1", b.TickCount())
	}
	if v.texHandle != 42 || v.texW != 800 || v.texH != 600 {
		t.Errorf("Tick didn't promote: tex=%d %dx%d", v.texHandle, v.texW, v.texH)
	}
}

func TestTickWithoutPageReturnsZero(t *testing.T) {
	v := NewWebView(Config{})
	r := v.Tick(timeNow())
	if !r.IsEmpty() {
		t.Errorf("Tick on un-Opened WebView returned %+v; want empty", r)
	}
}

func TestHandleMouseTranslatesCoordinates(t *testing.T) {
	b := withFakeBackend(t)
	v := NewWebView(Config{})
	_ = v.Open()
	v.Layout(qui.Rect{X: 100, Y: 50, W: 400, H: 300})

	// MouseMove at window coords (150, 80) should arrive at page as
	// widget-local (50, 30).
	ev := qui.NewMouseEvent(qui.EventMouseMove, 150, 80, qui.MouseButtonLeft, 0)
	if !v.Handle(ev) {
		t.Error("Handle(MouseMove) should consume")
	}
	page := b.pages[0]
	if got := page.mouseMoves; len(got) != 1 || got[0] != (mouseMove{X: 50, Y: 30}) {
		t.Errorf("mouseMoves = %+v; want [{50 30}]", got)
	}

	// MouseDown.
	v.Handle(qui.NewMouseEvent(qui.EventMouseDown, 200, 100, qui.MouseButtonLeft, 0))
	if got := page.mouseButtons; len(got) != 1 || got[0].X != 100 || got[0].Y != 50 || !got[0].Down {
		t.Errorf("mouseButtons = %+v; want one down at (100,50)", got)
	}
	if got := len(page.focusCalls); got != 1 || !page.focusCalls[0] {
		t.Errorf("focusCalls = %+v; want [true] on mousedown refocus", page.focusCalls)
	}

	// Scroll.
	v.Handle(qui.NewScrollEvent(200, 100, 0, -50, 0))
	if got := page.scrolls; len(got) != 1 || got[0].DY != -50 || got[0].X != 100 {
		t.Errorf("scrolls = %+v; want one at (100,50) dy=-50", got)
	}
}

func TestHandleMouseMultiClickCounts(t *testing.T) {
	b := withFakeBackend(t)
	v := NewWebView(Config{})
	_ = v.Open()
	v.Layout(qui.Rect{X: 100, Y: 50, W: 400, H: 300})

	// Three quick clicks at the same location should be forwarded as
	// clickCount 1/2/3 on both down and up events.
	for i := 0; i < 3; i++ {
		v.Handle(qui.NewMouseEvent(qui.EventMouseDown, 200, 100, qui.MouseButtonLeft, 0))
		v.Handle(qui.NewMouseEvent(qui.EventMouseUp, 200, 100, qui.MouseButtonLeft, 0))
	}

	page := b.pages[0]
	if len(page.mouseButtons) != 6 {
		t.Fatalf("mouseButtons len=%d; want 6", len(page.mouseButtons))
	}
	want := []int{1, 1, 2, 2, 3, 3}
	for i, w := range want {
		if got := page.mouseButtons[i].ClickCount; got != w {
			t.Fatalf("mouseButtons[%d].ClickCount=%d; want %d", i, got, w)
		}
	}

	// Click far enough away should reset clickCount back to 1.
	v.Handle(qui.NewMouseEvent(qui.EventMouseDown, 260, 140, qui.MouseButtonLeft, 0))
	v.Handle(qui.NewMouseEvent(qui.EventMouseUp, 260, 140, qui.MouseButtonLeft, 0))
	if got := page.mouseButtons[6].ClickCount; got != 1 {
		t.Fatalf("reset click down count=%d; want 1", got)
	}
	if got := page.mouseButtons[7].ClickCount; got != 1 {
		t.Fatalf("reset click up count=%d; want 1", got)
	}
}

func TestHandleKeyAndChar(t *testing.T) {
	b := withFakeBackend(t)
	v := NewWebView(Config{})
	_ = v.Open()
	v.Layout(qui.Rect{W: 400, H: 300})

	v.Handle(qui.NewKeyEvent(qui.EventKeyDown, qui.KeyEnter, 0))
	v.Handle(qui.NewKeyEvent(qui.EventKeyUp, qui.KeyEnter, 0))
	v.Handle(qui.NewCharEvent('A', 0))

	page := b.pages[0]
	if len(page.keys) != 2 || !page.keys[0].Down || page.keys[1].Down {
		t.Errorf("keys = %+v; want [down,up]", page.keys)
	}
	if len(page.chars) != 1 || page.chars[0].R != 'A' {
		t.Errorf("chars = %+v; want [{A}]", page.chars)
	}
}

func TestHandleF12OpensDevTools(t *testing.T) {
	b := withFakeBackend(t)
	v := NewWebView(Config{})
	_ = v.Open()
	v.Layout(qui.Rect{W: 400, H: 300})

	v.Handle(qui.NewKeyEvent(qui.EventKeyDown, qui.KeyF12, 0))

	page := b.pages[0]
	if page.devToolsOpen != 1 {
		t.Fatalf("devToolsOpen=%d; want 1", page.devToolsOpen)
	}
	if len(page.keys) != 0 {
		t.Fatalf("keys forwarded=%d; want 0 (F12 consumed for DevTools)", len(page.keys))
	}
}

func TestHandleMacShortcutsOpenDevTools(t *testing.T) {
	b := withFakeBackend(t)
	v := NewWebView(Config{})
	_ = v.Open()
	v.Layout(qui.Rect{W: 400, H: 300})

	v.Handle(qui.NewKeyEvent(qui.EventKeyDown, qui.KeyI, qui.ModSuper|qui.ModAlt))
	v.Handle(qui.NewKeyEvent(qui.EventKeyDown, qui.KeyI, qui.ModSuper|qui.ModShift))

	page := b.pages[0]
	if page.devToolsOpen != 2 {
		t.Fatalf("devToolsOpen=%d; want 2", page.devToolsOpen)
	}
}

func TestIMEForwarding(t *testing.T) {
	b := withFakeBackend(t)
	v := NewWebView(Config{})
	_ = v.Open()

	v.SetPreedit("你好", 1)
	v.CommitIME("你好世界")

	page := b.pages[0]
	if len(page.preedits) != 1 || page.preedits[0].Text != "你好" || page.preedits[0].Cursor != 1 {
		t.Errorf("preedits = %+v; want [{你好 1}]", page.preedits)
	}
	if len(page.commits) != 1 || page.commits[0] != "你好世界" {
		t.Errorf("commits = %+v; want [你好世界]", page.commits)
	}
}

func TestSetFocusedForwards(t *testing.T) {
	b := withFakeBackend(t)
	v := NewWebView(Config{})
	_ = v.Open()
	v.SetFocused(true)
	v.SetFocused(false)
	page := b.pages[0]
	if len(page.focusCalls) != 2 || !page.focusCalls[0] || page.focusCalls[1] {
		t.Errorf("focusCalls = %+v; want [true, false]", page.focusCalls)
	}
}

func TestCaretRectTranslatesToWindowSpace(t *testing.T) {
	b := withFakeBackend(t)
	v := NewWebView(Config{})
	_ = v.Open()
	v.Layout(qui.Rect{X: 100, Y: 50, W: 400, H: 300})

	page := b.pages[0]
	page.caret = qui.Rect{X: 20, Y: 30, W: 2, H: 16}

	got := v.CaretRect()
	want := qui.Rect{X: 120, Y: 80, W: 2, H: 16}
	if got != want {
		t.Errorf("CaretRect() = %+v; want %+v", got, want)
	}
}

func TestCaretRectFallbackToWebViewBounds(t *testing.T) {
	withFakeBackend(t)
	v := NewWebView(Config{})
	_ = v.Open()
	v.Layout(qui.Rect{X: 100, Y: 50, W: 400, H: 300})
	// Page.caret is zero — WebView should fall back to a small rect
	// anchored to the widget's top-left so the IME candidate window
	// doesn't snap to the screen origin.
	got := v.CaretRect()
	if got.IsEmpty() {
		t.Fatal("CaretRect should fall back to widget anchor when page returns empty")
	}
	if got.X != 100 || got.Y != 50 {
		t.Errorf("CaretRect fallback origin = (%v,%v); want (100,50)", got.X, got.Y)
	}
}

func TestLayoutResizeOnlyOnSizeChange(t *testing.T) {
	b := withFakeBackend(t)
	v := NewWebView(Config{})
	_ = v.Open()
	page := b.pages[0]

	v.Layout(qui.Rect{X: 0, Y: 0, W: 800, H: 600})
	v.Layout(qui.Rect{X: 100, Y: 100, W: 800, H: 600}) // same size, different origin
	v.Layout(qui.Rect{X: 0, Y: 0, W: 1024, H: 768})    // size change

	if len(page.resizes) != 2 {
		t.Errorf("page.resizes = %d; want 2 (initial + size change)", len(page.resizes))
	}
	if page.resizes[1].W != 1024 || page.resizes[1].H != 768 {
		t.Errorf("page.resizes[1] = %+v; want {1024 768 1}", page.resizes[1])
	}
}

func TestHandlerBufferedBeforeOpen(t *testing.T) {
	b := withFakeBackend(t)
	v := NewWebView(Config{})
	called := false
	_ = v.RegisterHandler("ping", func(_ []byte) ([]byte, error) {
		called = true
		return []byte("pong"), nil
	})
	// Register before Open succeeds (buffered).
	_ = v.Open()
	page := b.pages[0]
	if _, ok := page.handlers["ping"]; !ok {
		t.Error("buffered handler not rewired after Open")
	}
	// Sanity: actually call it.
	reply, err := page.handlers["ping"]([]byte("{}"))
	if err != nil {
		t.Fatal(err)
	}
	if !called || string(reply) != "pong" {
		t.Errorf("handler not called as expected: called=%v reply=%q", called, reply)
	}
}

func TestUnregisterHandler(t *testing.T) {
	b := withFakeBackend(t)
	v := NewWebView(Config{})
	_ = v.Open()
	_ = v.RegisterHandler("a", func(_ []byte) ([]byte, error) { return nil, nil })
	_ = v.UnregisterHandler("a")
	page := b.pages[0]
	if _, ok := page.handlers["a"]; ok {
		t.Error("UnregisterHandler did not remove from page")
	}
}

func TestEvaluateJSWithoutPageReturnsError(t *testing.T) {
	withNoBackend(t)
	v := NewWebView(Config{})
	if _, err := v.EvaluateJS("1+1"); !errors.Is(err, ErrNotSupported) {
		t.Errorf("EvaluateJS without backend = %v; want ErrNotSupported", err)
	}
}

func TestEvaluateJSRoundtripsThroughPage(t *testing.T) {
	b := withFakeBackend(t)
	v := NewWebView(Config{})
	if err := v.Open(); err != nil {
		t.Fatal(err)
	}
	page := b.pages[0]
	page.mu.Lock()
	page.evalValue = JSValue{Kind: JSKindNumber, Number: 42}
	page.mu.Unlock()

	got, err := v.EvaluateJS("21 * 2")
	if err != nil {
		t.Fatalf("EvaluateJS error: %v", err)
	}
	if got.Kind != JSKindNumber || got.Number != 42 {
		t.Errorf("EvaluateJS = %+v; want {Kind:Number, Number:42}", got)
	}
	if len(page.evalScripts) != 1 || page.evalScripts[0] != "21 * 2" {
		t.Errorf("page.evalScripts = %+v; want [21 * 2]", page.evalScripts)
	}
}

func TestEvaluateJSPropagatesError(t *testing.T) {
	b := withFakeBackend(t)
	v := NewWebView(Config{})
	_ = v.Open()
	page := b.pages[0]
	page.mu.Lock()
	page.evalErr = errors.New("oops")
	page.mu.Unlock()

	if _, err := v.EvaluateJS("throw 1"); err == nil || err.Error() != "oops" {
		t.Errorf("EvaluateJS error = %v; want \"oops\"", err)
	}
}

func TestRegisterHandlerRoutesToPage(t *testing.T) {
	b := withFakeBackend(t)
	v := NewWebView(Config{})
	_ = v.Open()
	var received []byte
	if err := v.RegisterHandler("echo", func(p []byte) ([]byte, error) {
		received = append(received[:0], p...)
		return append([]byte(`{"got":`), append(p, ']', '}')...), nil
	}); err != nil {
		t.Fatal(err)
	}
	page := b.pages[0]
	fn, ok := page.handlers["echo"]
	if !ok {
		t.Fatal("handler not registered on page")
	}
	reply, err := fn([]byte(`"hi"`))
	if err != nil {
		t.Fatal(err)
	}
	if string(reply) == "" || string(received) != `"hi"` {
		t.Errorf("payload roundtrip failed: received=%q reply=%q", received, reply)
	}
}

// Passing nil to RegisterHandler is sugar for UnregisterHandler — both
// the widget table and the page table should reflect the removal so
// subsequent JS calls fall back to the "unknown handler" error path.
func TestRegisterHandlerNilDeletes(t *testing.T) {
	b := withFakeBackend(t)
	v := NewWebView(Config{})
	_ = v.Open()
	_ = v.RegisterHandler("a", func(_ []byte) ([]byte, error) { return nil, nil })
	if err := v.RegisterHandler("a", nil); err != nil {
		t.Fatalf("RegisterHandler(nil) error: %v", err)
	}
	page := b.pages[0]
	if _, still := page.handlers["a"]; still {
		t.Error("RegisterHandler(name, nil) didn't delete from page")
	}
}

func TestLoadEventsUpdateState(t *testing.T) {
	b := withFakeBackend(t)
	v := NewWebView(Config{InitialURL: "x"})
	_ = v.Open()
	page := b.pages[0]

	var startedURL, finishedURL string
	v.OnLoadStart(func(u string) { startedURL = u })
	v.OnLoadFinish(func(u string) { finishedURL = u })

	page.loadListener(LoadEvent{State: LoadStateLoading, URL: "https://example.com"})
	if v.State() != LoadStateLoading {
		t.Errorf("state after Loading = %v; want LoadStateLoading", v.State())
	}
	if startedURL != "https://example.com" {
		t.Errorf("onLoadStart url = %q", startedURL)
	}
	page.loadListener(LoadEvent{State: LoadStateLoaded, URL: "https://example.com"})
	if finishedURL != "https://example.com" {
		t.Errorf("onLoadFinish url = %q", finishedURL)
	}
}

func TestTitleListenerUpdatesAndFires(t *testing.T) {
	b := withFakeBackend(t)
	v := NewWebView(Config{})
	_ = v.Open()
	var titles []string
	v.OnTitleChanged(func(t string) { titles = append(titles, t) })
	page := b.pages[0]
	page.titleListener("Page One")
	page.titleListener("Page Two")
	if v.Title() != "Page Two" {
		t.Errorf("v.Title() = %q; want Page Two", v.Title())
	}
	if len(titles) != 2 {
		t.Errorf("title callbacks = %d; want 2", len(titles))
	}
}

func TestMultipleWebViewsShareBackendTick(t *testing.T) {
	b := withFakeBackend(t)
	v1 := NewWebView(Config{})
	v2 := NewWebView(Config{})
	_ = v1.Open()
	_ = v2.Open()
	v1.Layout(qui.Rect{W: 100, H: 100})
	v2.Layout(qui.Rect{W: 100, H: 100})

	// Each Tick call into v1 and v2 within one frame both call Backend.Tick.
	// We don't try to dedupe at the WebView layer (see backend.go) —
	// CefDoMessageLoopWork is documented as idempotent. Tests just
	// assert that both Ticks reach the backend.
	b.pages[0].setFrame(1, 100, 100, qui.Rect{})
	b.pages[1].setFrame(2, 100, 100, qui.Rect{})
	v1.Tick(timeNow())
	v2.Tick(timeNow())
	if got := b.TickCount(); got != 2 {
		t.Errorf("Backend.Tick called %d times across 2 WebViews; want 2", got)
	}
}

func timeNow() time.Time { return time.Now() }
