package qui

import (
	"encoding/json"
	"testing"
	"time"
)

// lifecycleSession records what a session is sent and which toggles it
// was given, standing in for the agent transport.
type lifecycleSession struct {
	toggles map[string]bool
	events  []pageLifecycleEvent
}

func (s *lifecycleSession) ID() string { return "s1" }

func (s *lifecycleSession) Emit(method string, params any) {
	if ev, ok := params.(pageLifecycleEvent); ok && method == "Page.lifecycleEvent" {
		s.events = append(s.events, ev)
	}
}

func (s *lifecycleSession) SetToggle(name string, on bool) {
	if s.toggles == nil {
		s.toggles = map[string]bool{}
	}
	s.toggles[name] = on
}

func callLifecycle(t *testing.T, w *Window, sess ProtocolSession, method string, params any) (any, error) {
	t.Helper()
	cmd, ok := LookupProtocolCommand(method)
	if !ok {
		t.Fatalf("no command %s", method)
	}
	raw, _ := json.Marshal(params)
	return cmd.Call(&ProtocolContext{Window: w, Session: sess}, raw)
}

// The window loads once: DOMContentLoaded at the first layout over a root,
// load at the first painted frame. A session that enables lifecycle events
// afterwards is told both, as Chrome replays reached milestones.
func TestPageLifecycle_MilestonesAndReplay(t *testing.T) {
	win, _ := newHeadlessTestWindow(t, 1, 40, 30)
	if got := win.ReadyState(); got != "loading" {
		t.Fatalf("before any frame: readyState = %q, want loading", got)
	}
	early := &lifecycleSession{}
	if _, err := callLifecycle(t, win, early, "Page.setLifecycleEventsEnabled", map[string]any{"enabled": true}); err != nil {
		t.Fatal(err)
	}
	if !early.toggles[LifecycleEventsToggle] || len(early.events) != 0 {
		t.Fatalf("enable before load: toggles %v, events %v — want the toggle set and nothing replayed", early.toggles, early.events)
	}

	win.Step() // no root yet: nothing to lay out
	if got := win.ReadyState(); got != "loading" {
		t.Fatalf("Step without a root: readyState = %q, want loading", got)
	}
	win.SetRoot(&fillWidget{c: Color{R: 1, A: 1}})
	win.Step()
	if got := win.ReadyState(); got != "complete" {
		t.Fatalf("after a laid-out, painted frame: readyState = %q, want complete", got)
	}

	late := &lifecycleSession{}
	if _, err := callLifecycle(t, win, late, "Page.setLifecycleEventsEnabled", map[string]any{"enabled": true}); err != nil {
		t.Fatal(err)
	}
	if len(late.events) != 2 || late.events[0].Name != "DOMContentLoaded" || late.events[1].Name != "load" {
		t.Fatalf("replay = %+v, want DOMContentLoaded then load", late.events)
	}
	if late.events[0].Timestamp <= 0 || late.events[1].Timestamp < late.events[0].Timestamp {
		t.Fatalf("timestamps %v, %v: want positive and in order", late.events[0].Timestamp, late.events[1].Timestamp)
	}
	if late.events[0].FrameID != win.TargetID() {
		t.Fatalf("frameId = %q, want the window's targetId %q", late.events[0].FrameID, win.TargetID())
	}

	// Milestones fire once: later frames don't move them.
	first := lifecycleTime(&win.lifecycle.load)
	win.SetRoot(&fillWidget{c: Color{G: 1, A: 1}})
	win.Step()
	if lifecycleTime(&win.lifecycle.load) != first {
		t.Fatal("a later frame moved the load timestamp")
	}

	off := &lifecycleSession{}
	if _, err := callLifecycle(t, win, off, "Page.setLifecycleEventsEnabled", map[string]any{"enabled": false}); err != nil {
		t.Fatal(err)
	}
	if off.toggles[LifecycleEventsToggle] || len(off.events) != 0 {
		t.Fatalf("disable: toggles %v, events %v", off.toggles, off.events)
	}
}

// Agent.waitForReady is the same state for HTTP callers, which hear no
// events: it returns once the window has loaded and gone idle, and times
// out honestly on a window that never loads.
func TestAgentWaitForReady(t *testing.T) {
	never := NewTestWindow(Size{W: 100, H: 100}) // no platform: never steps
	_, err := callLifecycle(t, never, nil, "Agent.waitForReady", map[string]any{"timeoutMs": 50})
	if r := ProtocolErrorFor(err).Reason; r != "timeout" {
		t.Fatalf("never-loading window: err = %v (%s), want timeout", err, r)
	}

	win, _ := newHeadlessTestWindow(t, 1, 40, 30)
	win.SetRoot(&fillWidget{c: Color{B: 1, A: 1}})
	type result struct {
		res any
		err error
	}
	done := make(chan result, 1)
	go func() {
		res, err := callLifecycle(t, win, nil, "Agent.waitForReady", map[string]any{"timeoutMs": 3000})
		done <- result{res, err}
	}()
	deadline := time.After(5 * time.Second)
	for {
		win.Step() // this goroutine is the window's UI thread
		select {
		case r := <-done:
			if r.err != nil {
				t.Fatalf("waitForReady: %v", r.err)
			}
			if got := r.res.(agentReadyResult).ReadyState; got != "complete" {
				t.Fatalf("readyState = %q, want complete", got)
			}
			return
		case <-deadline:
			t.Fatal("waitForReady never returned")
		case <-time.After(5 * time.Millisecond):
		}
	}
}
