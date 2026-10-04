//go:build darwin && cgo

package qui

/*
#cgo CFLAGS: -x objective-c -fobjc-arc
#cgo LDFLAGS: -framework Cocoa

#include <stdint.h>

// Installer exported by platform_glfw_gesture_darwin.m. Takes the NSWindow pointer and
// a uintptr handle we pass back on every gesture callback so the Go side
// can look up the right *Window. Returns a bitmask of what got installed
// (see gestureInstallReport).
int quiInstallGestures(void* nsWindowPtr, uintptr_t handle);
*/
import "C"

import (
	"log"
	"os"
	"sync"
)

// gestureDebug, set by QUI_DEBUG_GESTURE=1, logs what the ObjC bridge
// managed to install on GLFW's content view class. Worth having as a
// permanent diagnostic because the install is the only failure mode that
// is both silent and total: a swizzle that doesn't land means pinch never
// fires, and one that lands without capturing GLFW's own scrollWheel:
// implementation means scrolling dies everywhere.
var gestureDebug = os.Getenv("QUI_DEBUG_GESTURE") == "1"

// gestureRegistry maps uintptr handles to the GLFW backend windows whose
// views were swizzled. Same rationale as the IME registry: cgo forbids
// parking Go pointers in C, so C holds integer IDs and we resolve them
// here.
var (
	gestureRegistryMu sync.RWMutex
	gestureRegistry           = map[uintptr]*glfwWindow{}
	gestureNextID     uintptr = 1
)

func registerGestureWindow(w *glfwWindow) uintptr {
	gestureRegistryMu.Lock()
	defer gestureRegistryMu.Unlock()
	id := gestureNextID
	gestureNextID++
	gestureRegistry[id] = w
	return id
}

func lookupGestureWindow(id uintptr) *glfwWindow {
	gestureRegistryMu.RLock()
	defer gestureRegistryMu.RUnlock()
	return gestureRegistry[id]
}

// installGestures wires this window's NSView to the gesture bridge. Safe
// to call unconditionally: a window without a Cocoa handle is skipped, and
// the ObjC side installs its selector overrides on the view's class only
// once per process.
func (w *glfwWindow) installGestures() {
	if w == nil {
		return
	}
	nsWindow := glfwNativeWindow(w.handle)
	if nsWindow == nil {
		return
	}
	id := registerGestureWindow(w)
	report := int(C.quiInstallGestures(nsWindow, C.uintptr_t(id)))
	if gestureDebug {
		log.Printf("qui/gesture: install report %s", gestureInstallReport(report))
	}
}

// gestureInstallReport renders the ObjC installer's bitmask. chained=no is
// the alarming one — it means GLFW's own scrollWheel: was replaced without
// being preserved, so scrolling would be dead.
func gestureInstallReport(report int) string {
	yn := func(bit int) string {
		if report&(1<<bit) != 0 {
			return "yes"
		}
		return "no"
	}
	return "magnify=" + yn(0) +
		" rotate=" + yn(1) +
		" smartMagnify=" + yn(2) +
		" scrollWheel=" + yn(3) +
		" chained=" + yn(4)
}

// mapNSGesturePhase converts the NSEventPhase bit the ObjC side already
// reduced to a small integer into a GesturePhase. The reduction happens
// in C (quiPhaseOf) because NSEventPhase is a bitmask whose constants
// aren't worth re-declaring in Go.
func mapNSGesturePhase(phase C.int) GesturePhase {
	switch phase {
	case 1:
		return GesturePhaseBegan
	case 2:
		return GesturePhaseChanged
	case 3:
		return GesturePhaseEnded
	case 4:
		return GesturePhaseCancelled
	case 5:
		return GesturePhaseMomentum
	default:
		return GesturePhaseNone
	}
}

// mapNSMods converts the modifier bitmask the ObjC side normalized (see
// quiModsOf) into qui Modifiers.
func mapNSMods(mods C.int) Modifiers {
	var out Modifiers
	if mods&1 != 0 {
		out |= ModShift
	}
	if mods&2 != 0 {
		out |= ModControl
	}
	if mods&4 != 0 {
		out |= ModAlt
	}
	if mods&8 != 0 {
		out |= ModSuper
	}
	return out
}

// quiGestureMagnify is called from magnifyWithEvent:. magnification is
// NSEvent's per-event increment (a fraction, e.g. 0.02), which is
// exactly what GestureEvent.DScale carries.
//
//export quiGestureMagnify
func quiGestureMagnify(id C.uintptr_t, magnification C.double, phase C.int, mods C.int) {
	w := lookupGestureWindow(uintptr(id))
	if w == nil {
		return
	}
	w.noteGesture(EventGesturePinch, mapNSGesturePhase(phase),
		float32(magnification), 0, mapNSMods(mods))
}

// quiGestureRotate is called from rotateWithEvent:. NSEvent reports
// rotation in degrees, counter-clockwise positive.
//
//export quiGestureRotate
func quiGestureRotate(id C.uintptr_t, rotation C.double, phase C.int, mods C.int) {
	w := lookupGestureWindow(uintptr(id))
	if w == nil {
		return
	}
	w.noteGesture(EventGestureRotate, mapNSGesturePhase(phase),
		0, float32(rotation), mapNSMods(mods))
}

// quiGestureSmartMagnify is called from smartMagnifyWithEvent: — the
// two-finger double-tap. One-shot with no scale: the receiver decides
// what "smart zoom" means (typically toggling 100% ↔ fit).
//
//export quiGestureSmartMagnify
func quiGestureSmartMagnify(id C.uintptr_t, mods C.int) {
	w := lookupGestureWindow(uintptr(id))
	if w == nil {
		return
	}
	w.noteGesture(EventGestureSmartMagnify, GesturePhaseNone, 0, 0, mapNSMods(mods))
}

// quiNoteScroll is called from the scrollWheel: wrapper BEFORE it chains
// into GLFW's own implementation, recording the modifier + phase state
// GLFW's callback signature drops. GLFW's implementation synchronously
// invokes the Go scroll callback, which consumes this snapshot.
//
//export quiNoteScroll
func quiNoteScroll(id C.uintptr_t, mods C.int, phase C.int) {
	w := lookupGestureWindow(uintptr(id))
	if w == nil {
		return
	}
	w.noteScrollContext(mapNSMods(mods), mapNSGesturePhase(phase))
}

// quiClearScroll is called after GLFW's scrollWheel: returns, dropping
// any snapshot that never reached the Go callback (GLFW skips
// _glfwInputScroll when both deltas are zero) so it can't leak into an
// unrelated later scroll.
//
//export quiClearScroll
func quiClearScroll(id C.uintptr_t) {
	w := lookupGestureWindow(uintptr(id))
	if w == nil {
		return
	}
	w.clearScrollContext()
}
