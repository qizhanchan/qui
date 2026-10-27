package qui_test

import (
	"log/slog"
	"sync"
	"testing"

	"github.com/qizhanchan/qui"
	"github.com/qizhanchan/qui/widgets"
)

// recordingSink collects every protocol event.
type recordingSink struct {
	mu     sync.Mutex
	events []string
	params []any
}

func (s *recordingSink) WantsEvents(*qui.Window, string) bool { return true }

func (s *recordingSink) HandleEvent(_ *qui.Window, method string, params any) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.events = append(s.events, method)
	s.params = append(s.params, params)
}

func (s *recordingSink) has(method string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, m := range s.events {
		if m == method {
			return true
		}
	}
	return false
}

func installSink(t *testing.T) *recordingSink {
	s := &recordingSink{}
	t.Cleanup(qui.AddProtocolEventSink(s))
	return s
}

func TestProtocolEvents_LogHandler(t *testing.T) {
	sink := installSink(t)
	logger := slog.New(qui.LogHandler(nil)).With("user", "ada")
	logger.Warn("saved", "rows", 3)
	if !sink.has("Log.entryAdded") {
		t.Fatal("no Log.entryAdded")
	}
}

type panicky struct{ qui.BaseWidget }

func (p *panicky) Handle(qui.Event) bool { panic("boom in handler") }

func (p *panicky) HitTest(qui.Point) qui.Widget { return p }

func TestProtocolEvents_PanicSurvivedAndReported(t *testing.T) {
	sink := installSink(t)
	w := qui.NewTestWindow(qui.Size{W: 100, H: 100})
	p := &panicky{BaseWidget: qui.NewBaseWidget()}
	w.SetRoot(p)
	p.Layout(qui.Rect{W: 100, H: 100})

	if _, err := call(t, w, "Debug.setFlags", map[string]any{"survivePanics": true}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { call(t, w, "Debug.setFlags", map[string]any{"survivePanics": false}) })
	w.DispatchTestEvent(qui.NewMouseEvent(qui.EventMouseDown, 10, 10, qui.MouseButtonLeft, 0))
	if !sink.has("Runtime.exceptionThrown") {
		t.Fatalf("events = %v", sink.events)
	}
	// Without the flag the panic still propagates (after being reported).
	call(t, w, "Debug.setFlags", map[string]any{"survivePanics": false})
	defer func() {
		if recover() == nil {
			t.Fatal("panic was swallowed without survivePanics")
		}
	}()
	w.DispatchTestEvent(qui.NewMouseEvent(qui.EventMouseDown, 10, 10, qui.MouseButtonLeft, 0))
}

func TestProtocolEvents_InspectModePicksWithoutClicking(t *testing.T) {
	sink := installSink(t)
	w := qui.NewTestWindow(qui.Size{W: 200, H: 100})
	clicked := 0
	btn := widgets.NewButton("Go", func() { clicked++ })
	c := qui.NewContainer(qui.FlexLayout{}, btn)
	c.SetSelf(c)
	w.SetRoot(c)
	c.Measure(qui.Size{W: 200, H: 100})
	c.Layout(qui.Rect{W: 200, H: 100})
	b := btn.Bounds()

	if _, err := call(t, w, "Overlay.setInspectMode", map[string]any{"mode": "searchForNode"}); err != nil {
		t.Fatal(err)
	}
	x, y := b.X+b.W/2, b.Y+b.H/2
	w.DispatchTestEvent(qui.NewMouseEvent(qui.EventMouseDown, x, y, qui.MouseButtonLeft, 0))
	w.DispatchTestEvent(qui.NewMouseEvent(qui.EventMouseUp, x, y, qui.MouseButtonLeft, 0))
	if clicked != 0 {
		t.Fatal("the picking click reached the app")
	}
	if !sink.has("Overlay.inspectNodeRequested") {
		t.Fatalf("events = %v", sink.events)
	}
	// The mode ended with the pick: the next click is the app's.
	w.DispatchTestEvent(qui.NewMouseEvent(qui.EventMouseDown, x, y, qui.MouseButtonLeft, 0))
	w.DispatchTestEvent(qui.NewMouseEvent(qui.EventMouseUp, x, y, qui.MouseButtonLeft, 0))
	if clicked != 1 {
		t.Fatalf("clicked = %d after inspect mode ended", clicked)
	}
}

func TestProtocol_FlagsAndAnimationClock(t *testing.T) {
	w := qui.NewTestWindow(qui.Size{W: 100, H: 100})
	flags, err := call(t, w, "Debug.setFlags", map[string]any{"layout": true})
	if err != nil || flags["layout"] != true {
		t.Fatalf("setFlags = %v, %v", flags, err)
	}
	call(t, w, "Debug.setFlags", map[string]any{"layout": false})
	if _, err := call(t, w, "Debug.setFlags", map[string]any{"bogus": true}); reason(err) != "invalid_params" {
		t.Fatalf("unknown flag: %v", err)
	}

	st, err := call(t, w, "Animation.setPaused", map[string]any{"paused": true})
	if err != nil || st["paused"] != true {
		t.Fatalf("setPaused = %v, %v", st, err)
	}
	if _, err := call(t, w, "Animation.advance", map[string]any{"ms": 250}); err != nil {
		t.Fatal(err)
	}
	st, _ = call(t, w, "Animation.setPaused", map[string]any{"paused": false})
	if st["paused"] != false || st["playbackRate"] != float64(1) {
		t.Fatalf("resume = %v", st)
	}
}
