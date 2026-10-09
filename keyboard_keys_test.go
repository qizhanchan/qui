package qui

import (
	"testing"

	"github.com/go-gl/glfw/v3.3/glfw"
)

func TestGLFWMapsModifierAndExtendedKeys(t *testing.T) {
	cases := map[glfw.Key]Key{
		glfw.KeyLeftShift: KeyLeftShift, glfw.KeyRightShift: KeyRightShift,
		glfw.KeyLeftControl: KeyLeftControl, glfw.KeyRightControl: KeyRightControl,
		glfw.KeyLeftAlt: KeyLeftAlt, glfw.KeyRightAlt: KeyRightAlt,
		glfw.KeyLeftSuper: KeyLeftSuper, glfw.KeyRightSuper: KeyRightSuper,
		glfw.KeyCapsLock: KeyCapsLock, glfw.KeyGraveAccent: KeyGrave,
		glfw.KeyF13: KeyF13, glfw.KeyF20: KeyF20,
		glfw.KeyKP0: KeyKP0, glfw.KeyKP9: KeyKP9, glfw.KeyKPAdd: KeyKPAdd,
	}
	for in, want := range cases {
		if got := mapKey(in); got != want {
			t.Errorf("mapKey(%v) = %v, want %v", in, got, want)
		}
	}
}

// capsFake adds a Caps Lock source to the fake backend.
type capsFake struct {
	*fakePlatformWindow
	capsState bool
}

func (c *capsFake) capsLock() bool { return c.capsState }

func TestOnKeyCarriesScanCodeRepeatAndCapsLock(t *testing.T) {
	f := &capsFake{fakePlatformWindow: newFakePlatformWindow(), capsState: true}
	w := &Window{plat: f, platformBacked: true, renderer: NoopRenderer{}, lastSize: Size{W: 800, H: 600}}
	var got []KeyEvent
	var chars []rune
	probe := newEventProbe(&got, &chars)
	w.SetRoot(probe)
	w.SetFocus(probe)

	w.onKey(KeyLeftSuper, 0x37, true, false, ModSuper) // Command down (kVK_Command)
	w.onKey(KeyA, 0x00, true, true, ModSuper)          // auto-repeat
	w.onKey(KeyA, 0x00, false, true, ModSuper)         // a release is never a repeat
	if len(got) != 3 {
		t.Fatalf("got %d key events", len(got))
	}
	if got[0].Key != KeyLeftSuper || got[0].ScanCode != 0x37 || got[0].Type() != EventKeyDown {
		t.Errorf("modifier key event = %+v", got[0])
	}
	if !got[1].Repeat || got[2].Repeat {
		t.Errorf("repeat flags = %v/%v", got[1].Repeat, got[2].Repeat)
	}
	if !got[1].CapsLock || got[1].Mods != ModSuper {
		t.Errorf("caps lock must ride beside Mods, not in it: %+v", got[1])
	}
}
