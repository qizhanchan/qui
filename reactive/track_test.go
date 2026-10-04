package reactive_test

import (
	"fmt"
	"testing"

	"github.com/qizhanchan/qui/reactive"
)

// Zero-dep Computed tracks its Gets automatically.
func TestComputedAutoTracksDeps(t *testing.T) {
	a := reactive.NewSignal(1)
	b := reactive.NewSignal(10)
	sum := reactive.Computed(func() int { return a.Get() + b.Get() })

	if sum.Get() != 11 {
		t.Fatalf("initial %d", sum.Get())
	}
	a.Set(2)
	if sum.Get() != 12 {
		t.Fatalf("after a: %d", sum.Get())
	}
	b.Set(20)
	if sum.Get() != 22 {
		t.Fatalf("after b: %d", sum.Get())
	}
}

// Dynamic dependencies: a computed behind an `if` re-collects deps per
// run — the branch not taken stops notifying.
func TestComputedDynamicDeps(t *testing.T) {
	useA := reactive.NewSignal(true)
	a := reactive.NewSignal("A0")
	b := reactive.NewSignal("B0")
	pick := reactive.Computed(func() string {
		if useA.Get() {
			return a.Get()
		}
		return b.Get()
	})

	recomputes := 0
	unsub := pick.Subscribe(func() { recomputes++ })
	defer unsub()

	if pick.Get() != "A0" {
		t.Fatalf("initial %q", pick.Get())
	}
	// While on the A branch, b changes must not notify.
	b.Set("B1")
	if recomputes != 0 {
		t.Fatalf("untaken-branch dep notified (%d)", recomputes)
	}

	useA.Set(false)
	if pick.Get() != "B1" {
		t.Fatalf("after switch %q", pick.Get())
	}

	// Now the roles flip: a must be dropped, b must be live.
	a.Set("A1")
	if pick.Get() != "B1" {
		t.Fatalf("dropped dep still recomputed: %q", pick.Get())
	}
	before := recomputes
	b.Set("B2")
	if pick.Get() != "B2" || recomputes <= before {
		t.Fatalf("live dep dead after switch: %q (recomputes %d)", pick.Get(), recomputes)
	}
}

// A computed reading another computed chains correctly.
func TestComputedChains(t *testing.T) {
	base := reactive.NewSignal(2)
	double := reactive.Computed(func() int { return base.Get() * 2 })
	label := reactive.Computed(func() string { return fmt.Sprintf("v=%d", double.Get()) })

	if label.Get() != "v=4" {
		t.Fatalf("initial %q", label.Get())
	}
	base.Set(5)
	if label.Get() != "v=10" {
		t.Fatalf("after set %q", label.Get())
	}
}

// Peek reads without registering a dependency.
func TestComputedPeekDoesNotTrack(t *testing.T) {
	tracked := reactive.NewSignal(1)
	peeked := reactive.NewSignal(100)
	c := reactive.Computed(func() int { return tracked.Get() + peeked.Peek() })

	if c.Get() != 101 {
		t.Fatalf("initial %d", c.Get())
	}
	peeked.Set(200) // must NOT recompute
	if c.Get() != 101 {
		t.Fatalf("Peek dep triggered recompute: %d", c.Get())
	}
	tracked.Set(2) // recompute picks up the NEW peeked value too
	if c.Get() != 202 {
		t.Fatalf("after tracked set: %d", c.Get())
	}
}

func TestComputedDisposeUnsubscribesTrackedDeps(t *testing.T) {
	a := reactive.NewSignal(1)
	b := reactive.NewSignal(2)
	sum := reactive.Computed(func() int { return a.Get() + b.Get() })
	if got := sum.Get(); got != 3 {
		t.Fatalf("initial %d, want 3", got)
	}

	sum.Dispose()
	a.Set(10)
	b.Set(20)
	if got := sum.Get(); got != 3 {
		t.Fatalf("disposed tracked computed updated to %d, want 3", got)
	}
}
