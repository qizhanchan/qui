//go:build cgo && !js

package wgpu

/*
#cgo CFLAGS: -I${SRCDIR}
#cgo !windows LDFLAGS: -ldl
#include <stdlib.h>
#include "helpers.h"
*/
import "C"

import (
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"unsafe"
)

var (
	loadOnce sync.Once
	loadErr  error
	loadPath string
)

// Load opens the Dawn library (once per process) and reports why it could
// not. QUI_WEBGPU=0 disables WebGPU.
func Load() error {
	loadOnce.Do(func() {
		if v := os.Getenv("QUI_WEBGPU"); v == "0" || strings.EqualFold(v, "off") {
			loadErr = errors.New("wgpu: disabled by QUI_WEBGPU=0")
			return
		}
		var tried []string
		for _, p := range LibraryCandidates() {
			if filepath.IsAbs(p) {
				if _, err := os.Stat(p); err != nil {
					continue
				}
			}
			cp := C.CString(p)
			var buf [512]C.char
			rc := C.quiWGPUOpen(cp, &buf[0], C.size_t(len(buf)))
			C.free(unsafe.Pointer(cp))
			msg := C.GoString(&buf[0])
			switch {
			case rc == 0:
				loadPath = p
				return
			case rc > 0:
				// Opened, but it is not the revision our header describes.
				loadErr = fmt.Errorf("wgpu: %s: %s (expected Dawn %s)", p, msg, DawnRevision[:12])
				return
			}
			tried = append(tried, p+": "+msg)
		}
		loadErr = fmt.Errorf("wgpu: Dawn library not found (run `go run github.com/qizhanchan/qui/cmd/qui-dawn`); tried:\n  %s",
			strings.Join(tried, "\n  "))
	})
	return loadErr
}

// LibraryPath is the library Load opened ("" before a successful Load).
func LibraryPath() string { return loadPath }

//export quiWGPUGoError
func quiWGPUGoError(kind C.int, msg *C.char, n C.int) {
	text := ""
	if msg != nil && n > 0 {
		text = C.GoStringN(msg, n)
	}
	if kind < 0 {
		log.Printf("qui: WebGPU device lost: %s", text)
		return
	}
	reportError(int(kind), text)
}

var errorHook struct {
	sync.Mutex
	fn func(kind int, msg string)
}

// SetErrorHandler replaces the default handler of uncaptured device errors
// (validation, out-of-memory, internal), which logs them. Tests use it to
// fail on any error.
func SetErrorHandler(fn func(kind int, msg string)) {
	errorHook.Lock()
	errorHook.fn = fn
	errorHook.Unlock()
}

func reportError(kind int, msg string) {
	errorHook.Lock()
	fn := errorHook.fn
	errorHook.Unlock()
	if fn != nil {
		fn(kind, msg)
		return
	}
	log.Printf("qui: WebGPU error (%d): %s", kind, msg)
}
