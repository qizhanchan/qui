//go:build darwin && cgo

package qui

import "github.com/qizhanchan/qui/internal/platform/macos"

func platformGamepads() []Gamepad {
	src := macos.Gamepads()
	out := make([]Gamepad, len(src))
	for i, g := range src {
		out[i] = Gamepad{ID: g.ID, Name: g.Name, Axes: g.Axes, Buttons: g.Buttons}
	}
	return out
}
