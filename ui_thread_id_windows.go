//go:build windows

package qui

import "golang.org/x/sys/windows"

func currentUIThreadID() uint64 { return uint64(windows.GetCurrentThreadId()) }
