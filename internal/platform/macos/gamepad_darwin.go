//go:build darwin && cgo

package macos

/*
#cgo CFLAGS: -x objective-c -fobjc-arc
#cgo LDFLAGS: -framework GameController -framework Foundation
#include "gamepad_darwin.h"
*/
import "C"

// Gamepad is one connected controller's state this instant.
type Gamepad struct {
	ID      int
	Name    string
	Axes    [6]float32
	Buttons uint32
}

// Gamepads polls the connected extended gamepads (at most 8).
func Gamepads() []Gamepad {
	var buf [8]C.QuiGamepad
	n := int(C.quiGamepads(&buf[0], C.int(len(buf))))
	out := make([]Gamepad, n)
	for i := range n {
		p := &buf[i]
		g := Gamepad{ID: int(p.id), Name: C.GoString(&p.name[0]), Buttons: uint32(p.buttons)}
		for k := range g.Axes {
			g.Axes[k] = float32(p.axes[k])
		}
		out[i] = g
	}
	return out
}
