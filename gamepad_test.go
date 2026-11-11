package qui

import "testing"

func TestGamepadAccessors(t *testing.T) {
	g := Gamepad{Axes: [6]float32{0.5, -1, 0, 0, 0.25, 1}, Buttons: 1<<GamepadA | 1<<GamepadDpadLeft}
	if !g.Pressed(GamepadA) || !g.Pressed(GamepadDpadLeft) || g.Pressed(GamepadB) {
		t.Fatal("button bits")
	}
	if g.Axis(GamepadLeftY) != -1 || g.Axis(GamepadRightTrigger) != 1 || g.Axis(GamepadAxis(9)) != 0 {
		t.Fatal("axes")
	}
	// Polling works with no controller attached (the usual CI case).
	for _, p := range Gamepads() {
		if p.Name == "" && p.ID == 0 {
			t.Fatalf("connected pad without identity: %+v", p)
		}
	}
}
