package qui

import (
	"testing"
	"time"
)

func TestDoubleTapModifier(t *testing.T) {
	win := NewTestWindow(Size{W: 200, H: 200})
	reg := NewAcceleratorRegistry()
	win.SetAcceleratorRegistry(reg)
	win.SetRoot(NewContainer(FlexLayout{Direction: Vertical}))
	shift, ctrl := 0, 0
	reg.BindDoubleTap(ModShift, func() { shift++ })
	remove := reg.BindDoubleTap(ModControl, func() { ctrl++ })

	t0 := time.Unix(1000, 0)
	at := func(ms int) time.Time { return t0.Add(time.Duration(ms) * time.Millisecond) }
	key := func(kind EventType, k Key, mods Modifiers, ms int) {
		e := NewKeyEvent(kind, k, mods)
		e.When = at(ms)
		win.DispatchTestEvent(e)
	}
	tap := func(k Key, mod Modifiers, ms int) {
		key(EventKeyDown, k, mod, ms)
		key(EventKeyUp, k, 0, ms+60)
	}

	// ⇧ ⇧ quickly: fires; left then right shift counts
	tap(KeyLeftShift, ModShift, 0)
	tap(KeyRightShift, ModShift, 200)
	if shift != 1 {
		t.Fatalf("double shift fired %d", shift)
	}
	// too slow between the taps
	tap(KeyLeftShift, ModShift, 1000)
	tap(KeyLeftShift, ModShift, 1600)
	if shift != 1 {
		t.Fatalf("slow taps fired %d", shift)
	}
	// a letter in between (typing "A" with shift) spoils it
	key(EventKeyDown, KeyLeftShift, ModShift, 3000)
	key(EventKeyDown, KeyA, ModShift, 3050)
	key(EventKeyUp, KeyLeftShift, 0, 3100)
	tap(KeyLeftShift, ModShift, 3200)
	if shift != 1 {
		t.Fatalf("shifted letter fired %d", shift)
	}
	// a held shift (down for long) is no tap
	key(EventKeyDown, KeyLeftShift, ModShift, 5000)
	key(EventKeyUp, KeyLeftShift, 0, 5900)
	tap(KeyLeftShift, ModShift, 6000)
	if shift != 1 {
		t.Fatalf("held shift fired %d", shift)
	}
	// ⌃ ⌃ fires its own binding; removed, it does not
	tap(KeyLeftControl, ModControl, 8000)
	tap(KeyLeftControl, ModControl, 8200)
	if ctrl != 1 || shift != 1 {
		t.Fatalf("ctrl %d shift %d", ctrl, shift)
	}
	remove()
	tap(KeyLeftControl, ModControl, 9000)
	tap(KeyLeftControl, ModControl, 9200)
	if ctrl != 1 {
		t.Fatalf("removed binding fired")
	}
	// ⇧ then ⌃: different modifiers, nothing
	tap(KeyLeftShift, ModShift, 11000)
	tap(KeyLeftControl, ModControl, 11200)
	if shift != 1 {
		t.Fatalf("mixed fired")
	}
}

func TestParseBareModifier(t *testing.T) {
	k, m, err := ParseShortcut("Shift")
	if err != nil || k != KeyLeftShift || m != ModShift {
		t.Fatalf("Shift → %v %v %v", k, m, err)
	}
	if k, m, _ := ParseShortcut("Cmd+Ctrl"); k != KeyLeftControl || m != ModSuper|ModControl {
		t.Fatalf("Cmd+Ctrl → %v %v", k, m)
	}
}
