package qui_test

import (
	"sync"
	"testing"

	"github.com/qizhanchan/qui"
	"github.com/qizhanchan/qui/widgets"
)

func TestEventListener_SeesDispatchedEvent(t *testing.T) {
	w := qui.NewTestWindow(qui.Size{W: 200, H: 100})
	btn := widgets.NewButton("X", nil)
	btn.SetID("x")
	root := qui.NewContainer(qui.FlexLayout{})
	root.SetSelf(root)
	root.AddChild(btn)
	w.SetRoot(root)
	root.Layout(qui.Rect{W: 200, H: 100})
	btn.Layout(qui.Rect{X: 0, Y: 0, W: 100, H: 30})

	var mu sync.Mutex
	var seen []string
	tok := w.AddEventListener(func(rec qui.EventRecord) {
		mu.Lock()
		seen = append(seen, rec.Kind+":"+rec.TargetID)
		mu.Unlock()
	})
	defer w.RemoveEventListener(tok)

	_ = w.Click("#x", qui.ClickOptions{})

	mu.Lock()
	defer mu.Unlock()
	if len(seen) == 0 {
		t.Fatal("listener saw no events")
	}
	foundDown := false
	for _, s := range seen {
		if s == "MouseDown:x" {
			foundDown = true
		}
	}
	if !foundDown {
		t.Errorf("MouseDown:x missing from %v", seen)
	}
}

func TestEventListener_Remove(t *testing.T) {
	w := qui.NewTestWindow(qui.Size{W: 200, H: 100})
	btn := widgets.NewButton("X", nil)
	btn.SetID("x")
	root := qui.NewContainer(qui.FlexLayout{})
	root.SetSelf(root)
	root.AddChild(btn)
	w.SetRoot(root)
	root.Layout(qui.Rect{W: 200, H: 100})
	btn.Layout(qui.Rect{X: 0, Y: 0, W: 100, H: 30})

	count := 0
	tok := w.AddEventListener(func(rec qui.EventRecord) { count++ })
	_ = w.Click("#x", qui.ClickOptions{})
	first := count
	w.RemoveEventListener(tok)
	_ = w.Click("#x", qui.ClickOptions{})
	if count != first {
		t.Errorf("listener still firing after Remove: %d → %d", first, count)
	}
}

func TestEventListener_PanicIsolated(t *testing.T) {
	w := qui.NewTestWindow(qui.Size{W: 200, H: 100})
	btn := widgets.NewButton("X", nil)
	btn.SetID("x")
	root := qui.NewContainer(qui.FlexLayout{})
	root.SetSelf(root)
	root.AddChild(btn)
	w.SetRoot(root)
	root.Layout(qui.Rect{W: 200, H: 100})
	btn.Layout(qui.Rect{X: 0, Y: 0, W: 100, H: 30})

	w.AddEventListener(func(rec qui.EventRecord) { panic("boom") })
	survived := 0
	w.AddEventListener(func(rec qui.EventRecord) { survived++ })

	_ = w.Click("#x", qui.ClickOptions{})
	if survived == 0 {
		t.Errorf("second listener should still fire despite first panicking")
	}
}
