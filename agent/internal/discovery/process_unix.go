//go:build !windows && !js

package discovery

import "syscall"

// ProcessAlive reports whether pid names a running process.
func ProcessAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	err := syscall.Kill(pid, 0)
	return err == nil || err == syscall.EPERM
}
