package qui

import "testing"

func TestWindowIsActive(t *testing.T) {
	w := NewTestWindow(Size{W: 200, H: 100})
	if !w.IsActive() {
		t.Fatal("a new window should start active")
	}
	var got []bool
	w.OnActivate(func(a bool) { got = append(got, a) })
	w.SetActiveForTest(false)
	if w.IsActive() {
		t.Fatal("IsActive after resigning key")
	}
	w.SetActiveForTest(true)
	if !w.IsActive() {
		t.Fatal("not active after becoming key again")
	}
	if len(got) != 2 || got[0] || !got[1] {
		t.Fatalf("OnActivate calls = %v, want [false true]", got)
	}
	var nilWin *Window
	if nilWin.IsActive() {
		t.Fatal("nil window reports active")
	}
}
