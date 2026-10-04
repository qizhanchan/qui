package qui

import (
	"runtime"
	"strings"
	"testing"
)

func lockUIThreadForTest(t *testing.T) {
	t.Helper()
	runtime.LockOSThread()
	t.Cleanup(runtime.UnlockOSThread)
}

func panicFromGoroutine(fn func()) any {
	result := make(chan any, 1)
	go func() {
		defer func() { result <- recover() }()
		fn()
	}()
	return <-result
}

func requireUIThreadViolation(t *testing.T, recovered any, operation string) {
	t.Helper()
	violation, ok := recovered.(UIThreadViolation)
	if !ok {
		t.Fatalf("panic = %#v, want UIThreadViolation", recovered)
	}
	if violation.Operation != operation {
		t.Fatalf("operation = %q, want %q", violation.Operation, operation)
	}
	if violation.Owner == 0 || violation.Current == 0 || violation.Owner == violation.Current {
		t.Fatalf("invalid thread identities: owner=%d current=%d", violation.Owner, violation.Current)
	}
	if !strings.Contains(violation.Error(), "use Window.PostJob") {
		t.Fatalf("violation message does not explain the supported bridge: %q", violation.Error())
	}
}

func TestUIThreadChecksIdentifyOwner(t *testing.T) {
	lockUIThreadForTest(t)
	w := NewTestWindow(Size{W: 100, H: 80})
	w.EnableUIThreadChecks()
	if !w.UIThreadChecksEnabled() || !w.IsUIThread() {
		t.Fatal("calling goroutine should own the enabled test window")
	}

	isOwner := make(chan bool, 1)
	go func() { isOwner <- w.IsUIThread() }()
	if <-isOwner {
		t.Fatal("background goroutine unexpectedly reported UI ownership")
	}
}

func TestUIThreadChecksRejectWindowMutation(t *testing.T) {
	lockUIThreadForTest(t)
	w := NewTestWindow(Size{W: 100, H: 80})
	w.EnableUIThreadChecks()
	w.SetRoot(NewContainer(nil))

	recovered := panicFromGoroutine(func() { w.SetRoot(NewContainer(nil)) })
	requireUIThreadViolation(t, recovered, "Window.SetRoot")
	if w.Root() == nil {
		t.Fatal("rejected mutation changed the root")
	}
}

func TestUIThreadChecksRejectAttachedWidgetAndContainerMutation(t *testing.T) {
	lockUIThreadForTest(t)
	w := NewTestWindow(Size{W: 100, H: 80})
	w.EnableUIThreadChecks()
	child := NewBaseWidget()
	root := NewContainer(nil, &child)
	w.SetRoot(root)

	recovered := panicFromGoroutine(func() { child.SetEnabled(false) })
	requireUIThreadViolation(t, recovered, "BaseWidget.SetEnabled")
	if !child.Enabled() {
		t.Fatal("rejected widget mutation changed enabled state")
	}

	recovered = panicFromGoroutine(func() { root.AddChild(&BaseWidget{}) })
	requireUIThreadViolation(t, recovered, "Container.AddChild")
	if got := root.ChildCount(); got != 1 {
		t.Fatalf("rejected child mutation changed count to %d", got)
	}
}

func TestUIThreadChecksAllowPostJobBridge(t *testing.T) {
	lockUIThreadForTest(t)
	w := NewTestWindow(Size{W: 100, H: 80})
	w.EnableUIThreadChecks()
	root := NewContainer(nil)
	w.SetRoot(root)
	child := NewBaseWidget()

	posted := make(chan struct{})
	go func() {
		w.PostJob(func() { root.AddChild(&child) })
		close(posted)
	}()
	<-posted

	if got := w.DrainJobsForTest(); got != 1 {
		t.Fatalf("drained %d jobs, want 1", got)
	}
	if root.ChildCount() != 1 || child.Parent() != root || child.Window() != w {
		t.Fatal("PostJob mutation did not execute with normal tree wiring")
	}
}

func TestUIThreadChecksAllowDetachedConstruction(t *testing.T) {
	done := make(chan *BaseWidget, 1)
	go func() {
		widget := NewBaseWidget()
		widget.SetID("worker-built")
		widget.SetFixedSize(40, 20)
		widget.SetStyle(DefaultStyle())
		done <- &widget
	}()

	widget := <-done
	if widget.ID() != "worker-built" || widget.MinSize() != (Size{W: 40, H: 20}) {
		t.Fatal("detached widget construction did not retain state")
	}
}
