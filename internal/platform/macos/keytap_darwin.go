//go:build darwin && cgo

package macos

/*
#cgo LDFLAGS: -framework ApplicationServices
#include <stdint.h>
int  quiTapPrepare(void);
void quiTapSetActive(uintptr_t handle, int active, int escCode, int escMods);
void quiTapAck(uint64_t seq);
*/
import "C"

// TapPrepare starts the CGEventTap thread; false means the process lacks
// the Accessibility permission.
func TapPrepare() bool { return C.quiTapPrepare() != 0 }

// TapSetActive turns key capture on or off for a window; escCode < 0 means
// no escape chord.
func TapSetActive(handle uintptr, active bool, escCode, escMods int) {
	C.quiTapSetActive(C.uintptr_t(handle), cBool(active), C.int(escCode), C.int(escMods))
}

// TapAck releases the tap thread waiting on key seq.
func TapAck(seq uint64) { C.quiTapAck(C.uint64_t(seq)) }
