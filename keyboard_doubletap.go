package qui

import "time"

// keyboard_doubletap.go — double-tap modifier shortcuts: Shift pressed
// and released twice in quick succession with nothing else in between
// (JetBrains' Search Everywhere on ⇧⇧, Run Anything on ⌃⌃). A tap is a
// modifier key going down and up again alone; any other key — or another
// modifier held — spoils it. Left and right keys count as the same
// modifier.

// doubleTapWindow bounds both one tap (down → up) and the pause between
// the two taps.
const doubleTapWindow = 400 * time.Millisecond

type doubleTapEntry struct {
	id  int64
	mod Modifiers
	fn  func()
}

// doubleTapState tracks the modifier taps of one window.
type doubleTapState struct {
	mod    Modifiers // the modifier being tapped (0 = none)
	downAt time.Time
	clean  bool      // nothing else happened since it went down
	upAt   time.Time // the first tap's release (zero: no tap yet)
}

// BindDoubleTap binds fn to a double tap of one modifier (ModShift,
// ModControl, ModAlt or ModSuper). It returns the function that removes
// the binding. The newest binding of a modifier wins. Double taps do not
// fire while a modal is shown.
func (r *AcceleratorRegistry) BindDoubleTap(mod Modifiers, fn func()) (remove func()) {
	if r == nil || fn == nil {
		return func() {}
	}
	r.nextID++
	id := r.nextID
	r.taps = append(r.taps, doubleTapEntry{id: id, mod: mod, fn: fn})
	return func() {
		for i, e := range r.taps {
			if e.id == id {
				r.taps = append(r.taps[:i], r.taps[i+1:]...)
				return
			}
		}
	}
}

// doubleTapFor is the newest binding of mod.
func (r *AcceleratorRegistry) doubleTapFor(mod Modifiers) func() {
	if r == nil {
		return nil
	}
	for i := len(r.taps) - 1; i >= 0; i-- {
		if r.taps[i].mod == mod {
			return r.taps[i].fn
		}
	}
	return nil
}

// modifierOf is the modifier a key is (0 for any other key).
func modifierOf(k Key) Modifiers {
	switch k {
	case KeyLeftShift, KeyRightShift:
		return ModShift
	case KeyLeftControl, KeyRightControl:
		return ModControl
	case KeyLeftAlt, KeyRightAlt:
		return ModAlt
	case KeyLeftSuper, KeyRightSuper:
		return ModSuper
	}
	return 0
}

// observeDoubleTap watches every key event of the window; it returns
// true when the event completed a bound double tap (and ran it).
func (w *Window) observeDoubleTap(e KeyEvent) bool {
	s := &w.taps
	now := e.When
	if now.IsZero() {
		now = time.Now()
	}
	mod := modifierOf(e.Key)
	switch e.eventType {
	case EventKeyDown:
		if e.Repeat {
			if mod == 0 {
				*s = doubleTapState{}
			}
			return false
		}
		if mod == 0 || e.Mods&^mod != 0 {
			*s = doubleTapState{} // another key, or a chord: no tap
			return false
		}
		if s.mod == mod && !s.upAt.IsZero() && now.Sub(s.upAt) <= doubleTapWindow {
			*s = doubleTapState{}
			if fn := w.accels.doubleTapFor(mod); fn != nil && w.topModalIndex() < 0 {
				fn()
				return true
			}
			return false
		}
		*s = doubleTapState{mod: mod, downAt: now, clean: true}
	case EventKeyUp:
		if mod == 0 || mod != s.mod || !s.clean {
			return false
		}
		if now.Sub(s.downAt) > doubleTapWindow {
			*s = doubleTapState{}
			return false
		}
		if s.upAt.IsZero() {
			s.upAt = now // first tap done; the second must follow soon
		}
	}
	return false
}
