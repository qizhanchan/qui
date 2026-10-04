package widgets

import (
	"runtime"
	"testing"
	"time"

	. "github.com/qizhanchan/qui"
)

func typeaheadSelect(items []string) *Select {
	sel := NewSelect(nil, items, nil)
	sel.SetFocused(true)
	return sel
}

// typeAt feeds one character with an explicit timestamp, so the buffer
// timeout is exercised deterministically.
func typeAt(sel *Select, r rune, at time.Time) {
	ev := NewCharEvent(r, 0)
	ev.When = at
	sel.Handle(ev)
}

// Typing letters jumps to the matching option, and consecutive letters
// accumulate into a prefix.
func TestSelectTypeaheadPrefix(t *testing.T) {
	sel := typeaheadSelect([]string{"France", "Georgia", "Germany", "Greece"})
	t0 := time.Unix(2000, 0)

	typeAt(sel, 'g', t0)
	if got := sel.SelectedValue(); got != "Georgia" {
		t.Fatalf("after 'g': %q, want Georgia", got)
	}
	typeAt(sel, 'e', t0.Add(100*time.Millisecond))
	if got := sel.SelectedValue(); got != "Georgia" {
		t.Fatalf("after 'ge': %q, want Georgia", got)
	}
	typeAt(sel, 'r', t0.Add(200*time.Millisecond))
	if got := sel.SelectedValue(); got != "Germany" {
		t.Errorf("after 'ger': %q, want Germany", got)
	}
}

// The same letter repeated cycles through the options starting with it.
func TestSelectTypeaheadRepeatCycles(t *testing.T) {
	sel := typeaheadSelect([]string{"France", "Georgia", "Germany", "Greece"})
	t0 := time.Unix(2000, 0)

	typeAt(sel, 'g', t0)
	typeAt(sel, 'g', t0.Add(100*time.Millisecond))
	if got := sel.SelectedValue(); got != "Germany" {
		t.Fatalf("second 'g': %q, want Germany", got)
	}
	typeAt(sel, 'g', t0.Add(200*time.Millisecond))
	if got := sel.SelectedValue(); got != "Greece" {
		t.Fatalf("third 'g': %q, want Greece", got)
	}
	// Wraps back around.
	typeAt(sel, 'g', t0.Add(300*time.Millisecond))
	if got := sel.SelectedValue(); got != "Georgia" {
		t.Errorf("fourth 'g': %q, want Georgia (wrap)", got)
	}
}

// A pause longer than the reset window starts a new search instead of
// extending the old prefix.
func TestSelectTypeaheadBufferExpires(t *testing.T) {
	sel := typeaheadSelect([]string{"Alpha", "Beta", "Gamma"})
	t0 := time.Unix(2000, 0)

	typeAt(sel, 'b', t0)
	if got := sel.SelectedValue(); got != "Beta" {
		t.Fatalf("after 'b': %q, want Beta", got)
	}
	// "a" right after would search "ba" (no match → falls back to "a");
	// after the timeout it is unambiguously a fresh "a" search.
	typeAt(sel, 'a', t0.Add(2*time.Second))
	if got := sel.SelectedValue(); got != "Alpha" {
		t.Errorf("after the buffer expired, 'a': %q, want Alpha", got)
	}
}

// A prefix that matches nothing recovers by searching the last letter alone
// rather than going dead until the timeout.
func TestSelectTypeaheadNoMatchRestarts(t *testing.T) {
	sel := typeaheadSelect([]string{"Alpha", "Beta"})
	t0 := time.Unix(2000, 0)
	typeAt(sel, 'z', t0) // matches nothing, selection unchanged
	if sel.SelectedIdx != -1 {
		t.Fatalf("unmatched letter changed the selection to %d", sel.SelectedIdx)
	}
	typeAt(sel, 'b', t0.Add(50*time.Millisecond)) // "zb" → no match → "b"
	if got := sel.SelectedValue(); got != "Beta" {
		t.Errorf("recovery search: %q, want Beta", got)
	}
}

// Type-ahead never lands on a disabled option (an <option disabled> or a
// synthesized optgroup heading).
func TestSelectTypeaheadSkipsDisabled(t *testing.T) {
	sel := typeaheadSelect([]string{"Bad", "Better"})
	sel.SetItemDisabled([]bool{true, false})
	typeAt(sel, 'b', time.Unix(2000, 0))
	if got := sel.SelectedValue(); got != "Better" {
		t.Errorf("type-ahead selected %q, want Better (Bad is disabled)", got)
	}
}

// Modified keystrokes stay available as application shortcuts.
func TestSelectTypeaheadIgnoresCommandChords(t *testing.T) {
	sel := typeaheadSelect([]string{"Alpha"})
	mods := ModControl
	if runtime.GOOS == "darwin" {
		mods = ModSuper
	}
	ev := NewCharEvent('a', mods)
	ev.When = time.Unix(2000, 0)
	if sel.Handle(ev) {
		t.Error("a Cmd/Ctrl-modified character must not be consumed as type-ahead")
	}
	if sel.SelectedIdx != -1 {
		t.Error("modified keystroke changed the selection")
	}
}
