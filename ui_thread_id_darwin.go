//go:build darwin && cgo

package qui

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

func currentUIThreadID() uint64 { return uint64(C.quiCurrentThreadID()) }
