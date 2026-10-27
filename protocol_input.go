package qui

import (
	"errors"
	"fmt"
	"time"
)

// Raw, coordinate-level input — the Input domain. These are the CDP
// primitives: one event at a window point, no selector resolution and no
// reachability check. The selector-targeted actions (Click, Type, …) and
// the Agent domain built on them are what an agent normally uses; these
// exist for drawn surfaces with no addressable node and for clients (the
// DevTools screencast) that only speak coordinates.

// MouseAction is the kind of raw mouse event DispatchMouse synthesizes.
type MouseAction int

const (
	MousePressed MouseAction = iota
	MouseReleased
	MouseMoved
)

// DispatchMouse synthesizes one raw mouse event at window point (x, y).
// Safe from any goroutine.
func (w *Window) DispatchMouse(action MouseAction, x, y float32, button MouseButton, mods Modifiers) error {
	var et EventType
	switch action {
	case MousePressed:
		et = EventMouseDown
	case MouseReleased:
		et = EventMouseUp
	case MouseMoved:
		et = EventMouseMove
	default:
		return fmt.Errorf("qui: unknown mouse action %d", action)
	}
	return w.synchronously(func() error {
		w.dispatch(MouseEvent{
			baseEvent: baseEvent{shared: &eventState{}},
			eventType: et,
			When:      time.Now(),
			X:         x,
			Y:         y,
			Button:    button,
			Mods:      mods,
		})
		return nil
	})
}

// DispatchScroll synthesizes a scroll of (dx, dy) at window point (x, y).
func (w *Window) DispatchScroll(x, y, dx, dy float32) error {
	return w.synchronously(func() error {
		w.dispatchScrollAt(Point{X: x, Y: y}, dx, dy)
		return nil
	})
}

// DispatchPinch synthesizes a complete pinch gesture at (x, y) ending at the
// cumulative magnification scale.
func (w *Window) DispatchPinch(x, y, scale float32) error {
	if scale <= 0 {
		return errors.New("pinch: scale must be > 0")
	}
	return w.synchronously(func() error {
		w.dispatchPinchAt(Point{X: x, Y: y}, scale)
		return nil
	})
}

// DispatchKey synthesizes one raw key press (down) or release (!down) to
// the focused widget.
func (w *Window) DispatchKey(key Key, mods Modifiers, down bool) error {
	et := EventKeyUp
	if down {
		et = EventKeyDown
	}
	return w.synchronously(func() error {
		w.dispatch(KeyEvent{
			baseEvent: baseEvent{shared: &eventState{}},
			eventType: et,
			When:      time.Now(),
			Key:       key,
			Mods:      mods,
		})
		return nil
	})
}

// InsertText delivers text to the focused widget as committed characters,
// the way an input method commits — no key events.
func (w *Window) InsertText(text string) error {
	return w.synchronously(func() error {
		now := time.Now()
		for _, r := range text {
			w.dispatch(CharEvent{baseEvent: baseEvent{shared: &eventState{}}, Rune: r, When: now})
		}
		return nil
	})
}

// cdpModifiers decodes CDP's modifier bitmask (Alt=1, Ctrl=2, Meta=4,
// Shift=8).
func cdpModifiers(bits int) Modifiers {
	var m Modifiers
	if bits&1 != 0 {
		m |= ModAlt
	}
	if bits&2 != 0 {
		m |= ModControl
	}
	if bits&4 != 0 {
		m |= ModSuper
	}
	if bits&8 != 0 {
		m |= ModShift
	}
	return m
}

func cdpButton(name string) MouseButton {
	switch name {
	case "right":
		return MouseButtonRight
	case "middle":
		return MouseButtonMiddle
	}
	return MouseButtonLeft
}

type inputMouseParams struct {
	Type       string  `json:"type" desc:"mousePressed | mouseReleased | mouseMoved | mouseWheel"`
	X          float32 `json:"x" desc:"window x, logical pixels"`
	Y          float32 `json:"y" desc:"window y, logical pixels"`
	Button     string  `json:"button,omitempty" desc:"none | left | middle | right"`
	ClickCount int     `json:"clickCount,omitempty"`
	Modifiers  int     `json:"modifiers,omitempty" desc:"bitmask: Alt=1 Ctrl=2 Meta/Cmd=4 Shift=8"`
	DeltaX     float32 `json:"deltaX,omitempty" desc:"mouseWheel only; qui wheel units (1 = one wheel notch), positive scrolls right"`
	DeltaY     float32 `json:"deltaY,omitempty" desc:"mouseWheel only; qui wheel units (1 = one wheel notch), positive scrolls down"`
}

type inputKeyParams struct {
	Type      string `json:"type" desc:"keyDown | rawKeyDown | keyUp | char"`
	Key       string `json:"key,omitempty" desc:"key name: Enter, Escape, Tab, ArrowLeft, a, F5, … (chords like Cmd+Z also accepted)"`
	Text      string `json:"text,omitempty" desc:"for type=char (or keyDown with a printable key): the text to commit"`
	Modifiers int    `json:"modifiers,omitempty" desc:"bitmask: Alt=1 Ctrl=2 Meta/Cmd=4 Shift=8"`
}

type inputTextParams struct {
	Text string `json:"text"`
}

type inputPinchParams struct {
	X           float32 `json:"x"`
	Y           float32 `json:"y"`
	ScaleFactor float32 `json:"scaleFactor" desc:"cumulative magnification: 1.5 zooms in to 150%"`
}

type inputScrollGestureParams struct {
	X         float32 `json:"x"`
	Y         float32 `json:"y"`
	XDistance float32 `json:"xDistance,omitempty"`
	YDistance float32 `json:"yDistance,omitempty"`
}

type empty struct{}

// cdpKeyAliases maps DOM key names CDP clients send onto qui's shortcut
// key names.
var cdpKeyAliases = map[string]string{
	"ArrowLeft": "Left", "ArrowRight": "Right", "ArrowUp": "Up", "ArrowDown": "Down",
	" ": "Space",
}

func parseProtocolKey(name string) (Key, Modifiers, error) {
	if alias, ok := cdpKeyAliases[name]; ok {
		name = alias
	}
	return ParseShortcut(name)
}

func inputDomain() ProtocolDomain {
	return ProtocolDomain{
		Name:        "Input",
		Description: "Raw coordinate-level input (CDP Input). Prefer the Agent domain's selector-targeted actions; use these for drawn surfaces with no addressable node.",
		Commands: []ProtocolCommand{
			NewProtocolCommand("dispatchMouseEvent", "Dispatch one mouse event at a window point.",
				func(ctx *ProtocolContext, p inputMouseParams) (empty, error) {
					mods := cdpModifiers(p.Modifiers)
					switch p.Type {
					case "mousePressed":
						return empty{}, ctx.Window.DispatchMouse(MousePressed, p.X, p.Y, cdpButton(p.Button), mods)
					case "mouseReleased":
						return empty{}, ctx.Window.DispatchMouse(MouseReleased, p.X, p.Y, cdpButton(p.Button), mods)
					case "mouseMoved":
						return empty{}, ctx.Window.DispatchMouse(MouseMoved, p.X, p.Y, cdpButton(p.Button), mods)
					case "mouseWheel":
						return empty{}, ctx.Window.DispatchScroll(p.X, p.Y, p.DeltaX, p.DeltaY)
					}
					return empty{}, &ProtocolError{Code: ProtocolInvalidParams, Reason: "invalid_params", Message: "unknown mouse event type " + p.Type}
				}),
			NewProtocolCommand("dispatchKeyEvent", "Dispatch one key event to the focused widget.",
				func(ctx *ProtocolContext, p inputKeyParams) (empty, error) {
					mods := cdpModifiers(p.Modifiers)
					if p.Type == "char" {
						return empty{}, ctx.Window.InsertText(p.Text)
					}
					key, keyMods, err := parseProtocolKey(p.Key)
					if err != nil {
						// A printable key CDP sends as key="a" text="a".
						if p.Text != "" && p.Type != "keyUp" {
							return empty{}, ctx.Window.InsertText(p.Text)
						}
						return empty{}, &ProtocolError{Code: ProtocolInvalidParams, Reason: "invalid_params", Message: err.Error()}
					}
					switch p.Type {
					case "keyDown", "rawKeyDown":
						return empty{}, ctx.Window.DispatchKey(key, mods|keyMods, true)
					case "keyUp":
						return empty{}, ctx.Window.DispatchKey(key, mods|keyMods, false)
					}
					return empty{}, &ProtocolError{Code: ProtocolInvalidParams, Reason: "invalid_params", Message: "unknown key event type " + p.Type}
				}),
			NewProtocolCommand("insertText", "Commit text to the focused widget, as an input method would.",
				func(ctx *ProtocolContext, p inputTextParams) (empty, error) {
					return empty{}, ctx.Window.InsertText(p.Text)
				}),
			NewProtocolCommand("synthesizePinchGesture", "A full two-finger pinch at a point.",
				func(ctx *ProtocolContext, p inputPinchParams) (empty, error) {
					return empty{}, ctx.Window.DispatchPinch(p.X, p.Y, p.ScaleFactor)
				}),
			NewProtocolCommand("synthesizeScrollGesture", "Scroll at a point. As in CDP, positive distances scroll up/left; units are qui wheel notches.",
				func(ctx *ProtocolContext, p inputScrollGestureParams) (empty, error) {
					return empty{}, ctx.Window.DispatchScroll(p.X, p.Y, -p.XDistance, -p.YDistance)
				}),
		},
	}
}
