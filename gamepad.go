package qui

// GamepadButton names a game controller button (the standard extended
// gamepad layout; A is the bottom face button).
type GamepadButton int

const (
	GamepadA GamepadButton = iota
	GamepadB
	GamepadX
	GamepadY
	GamepadLeftShoulder
	GamepadRightShoulder
	GamepadDpadUp
	GamepadDpadDown
	GamepadDpadLeft
	GamepadDpadRight
	GamepadMenu
	GamepadOptions
	GamepadLeftThumb
	GamepadRightThumb
)

// GamepadAxis names an analog control.
type GamepadAxis int

const (
	GamepadLeftX GamepadAxis = iota
	GamepadLeftY             // up is positive
	GamepadRightX
	GamepadRightY
	GamepadLeftTrigger // 0–1
	GamepadRightTrigger
)

// Gamepad is a connected controller's state at the moment it was polled.
type Gamepad struct {
	ID      int // stable while connected
	Name    string
	Axes    [6]float32 // by GamepadAxis; sticks −1…1, triggers 0…1
	Buttons uint32     // bit per GamepadButton
}

// Pressed reports whether b is down.
func (g Gamepad) Pressed(b GamepadButton) bool { return g.Buttons&(1<<uint(b)) != 0 }

// Axis is the value of a.
func (g Gamepad) Axis(a GamepadAxis) float32 {
	if a < 0 || int(a) >= len(g.Axes) {
		return 0
	}
	return g.Axes[a]
}

// Gamepads polls the connected game controllers (macOS: the
// GameController framework's extended gamepads). Poll from the UI thread,
// once per frame; elsewhere it returns none.
func Gamepads() []Gamepad { return platformGamepads() }
