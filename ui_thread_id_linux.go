//go:build linux

package qui

import "syscall"

func currentUIThreadID() uint64 { return uint64(syscall.Gettid()) }
