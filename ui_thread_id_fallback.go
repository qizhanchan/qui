//go:build !linux && !windows && (!darwin || !cgo)

package qui

import "runtime"

// Platforms without a cheap native thread identifier retain a diagnostic
// fallback based on runtime.Stack. It is intentionally used only when checks
// are enabled; it is not synchronization and never participates in behavior.
func currentUIThreadID() uint64 {
	var stack [64]byte
	n := runtime.Stack(stack[:], false)
	const prefix = "goroutine "
	if n <= len(prefix) {
		return 0
	}
	var id uint64
	for _, ch := range stack[len(prefix):n] {
		if ch < '0' || ch > '9' {
			break
		}
		id = id*10 + uint64(ch-'0')
	}
	return id
}
