package qui

import "time"

// EventFilter sees every input event before the window dispatches it —
// before focus moves, before hover, drag and selection bookkeeping, before
// any widget's capture phase and before accelerators. Returning true
// consumes the event: nothing else sees it.
//
// This is the hook for behavior that has to sit above the widget tree: a
// command palette that grabs keys while open, a Vim-style modal layer, a
// macro recorder, a "press any key" binding field. AddEventListener only
// observes, and a capture handler on the root can't see events aimed at an
// overlay; a filter sees both.
//
// Events arrive in window-logical coordinates. A filter that swallows a
// MouseDown should usually swallow the matching MouseUp too, or a widget
// will see a release without a press.
type EventFilter func(Event) bool

type eventFilterEntry struct {
	id int64
	fn EventFilter
}

// AddEventFilter installs fn ahead of normal dispatch and returns a function
// that removes it. Filters run most-recently-added first, so a palette
// opened on top of a Vim layer gets keys before the layer does.
func (w *Window) AddEventFilter(fn EventFilter) (remove func()) {
	if w == nil || fn == nil {
		return func() {}
	}
	w.assertUIThread("Window.AddEventFilter")
	w.nextHookID++
	id := w.nextHookID
	w.eventFilters = append(w.eventFilters, eventFilterEntry{id: id, fn: fn})
	return func() {
		for i, e := range w.eventFilters {
			if e.id == id {
				w.eventFilters = append(w.eventFilters[:i:i], w.eventFilters[i+1:]...)
				return
			}
		}
	}
}

// runEventFilters reports whether a filter consumed event.
func (w *Window) runEventFilters(event Event) bool {
	if len(w.eventFilters) == 0 {
		return false
	}
	filters := append([]eventFilterEntry(nil), w.eventFilters...)
	for i := len(filters) - 1; i >= 0; i-- {
		if filters[i].fn(event) {
			return true
		}
	}
	return false
}

// EventCustom is the type of every CustomEvent.
const EventCustom EventType = 1000

// CustomEvent is an application-defined event. It travels the same
// capture → target → bubble path as built-in input, so a deeply nested
// widget can announce something ("row-renamed", "tab-close-requested") and
// any ancestor can react or stop it — without the emitter knowing who
// listens. Handlers type-switch on CustomEvent and compare Name.
type CustomEvent struct {
	baseEvent
	When time.Time
	// Name identifies the event. Use a namespaced name ("editor.save") to
	// avoid collisions between components.
	Name string
	// Detail is the payload, by convention a value or pointer type owned
	// by the emitting component.
	Detail any
	// Bubbles mirrors the DOM flag: false delivers to the target only
	// (capture still runs, as in the DOM).
	Bubbles bool
}

func (e CustomEvent) Type() EventType      { return EventCustom }
func (e CustomEvent) Timestamp() time.Time { return e.When }

// DispatchCustomEvent sends a bubbling CustomEvent named name to target and
// reports whether a handler consumed it (returned true or stopped
// propagation). Must run on the UI thread. Works on detached trees too:
// the path is target's Parent() chain.
func DispatchCustomEvent(target Widget, name string, detail any) bool {
	return DispatchEvent(target, CustomEvent{Name: name, Detail: detail, Bubbles: true})
}

// DispatchEvent runs ev through capture → target → bubble along target's
// ancestor path and reports whether it was consumed. Use it for
// CustomEvent; built-in input events should come from the platform (or
// the test helpers) so focus, hover and capture stay consistent.
func DispatchEvent(target Widget, ev CustomEvent) bool {
	if target == nil {
		return false
	}
	if ev.When.IsZero() {
		ev.When = time.Now()
	}
	st := &eventState{target: target}
	ev.baseEvent = baseEvent{shared: st}
	path := pathToRoot(target)
	for i := 0; i < len(path)-1; i++ {
		st.phase, st.currentTarget = PhaseCapture, path[i]
		if path[i].Handle(ev) || st.stopped {
			return true
		}
	}
	st.phase, st.currentTarget = PhaseTarget, target
	if target.Handle(ev) || st.stopped {
		return true
	}
	if !ev.Bubbles {
		return false
	}
	for i := len(path) - 2; i >= 0; i-- {
		st.phase, st.currentTarget = PhaseBubble, path[i]
		if path[i].Handle(ev) || st.stopped {
			return true
		}
	}
	return false
}
