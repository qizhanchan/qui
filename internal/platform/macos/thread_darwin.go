//go:build darwin && cgo

package macos

/*
#include <pthread.h>
#include <stdint.h>

static uint64_t quiCurrentThreadID(void) {
	uint64_t id = 0;
	pthread_threadid_np(NULL, &id);
	return id;
}
*/
import "C"

// CurrentThreadID returns the calling OS thread's id.
func CurrentThreadID() uint64 { return uint64(C.quiCurrentThreadID()) }
