//go:build windows || js

package discovery

// ProcessAlive cannot probe cheaply here; trust the discovery file.
func ProcessAlive(pid int) bool { return pid > 0 }
