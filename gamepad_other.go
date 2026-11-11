//go:build !darwin || !cgo

package qui

func platformGamepads() []Gamepad { return nil }
