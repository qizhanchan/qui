package qui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestWindowingLibraryStaysBehindTheSeam is an architectural guard, not a
// behavior test.
//
// The whole value of the platform seam is that exactly one set of files
// knows which windowing library is in use. That property is invisible at
// runtime and easy to erode: one `import "github.com/go-gl/glfw/..."`
// added for convenience — to reach a native handle, poll a key, wake the
// loop — and the engine quietly depends on GLFW again. This test makes
// that erosion a build failure instead of a slow leak.
//
// When adding a backend, add its files to the allowlist. When you feel the
// urge to import a windowing library anywhere else, the seam is missing a
// method — add it to platform.go instead.
func TestWindowingLibraryStaysBehindTheSeam(t *testing.T) {
	// Files permitted to name a windowing library, by prefix.
	allowed := []string{
		"platform_glfw", // the GLFW backend
	}
	// Import paths that constitute a windowing dependency.
	forbidden := []string{
		"github.com/go-gl/glfw",
	}

	roots, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatalf("glob: %v", err)
	}
	for _, path := range roots {
		if strings.HasSuffix(path, "_test.go") {
			continue
		}
		base := filepath.Base(path)
		if isAllowedPlatformFile(base, allowed) {
			continue
		}
		src, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		for _, imp := range forbidden {
			if strings.Contains(string(src), `"`+imp) {
				t.Errorf("%s imports %s directly.\n"+
					"Only the platform backend may name a windowing library — if the\n"+
					"engine needs something from the OS, add a method to the seam in\n"+
					"platform.go and implement it in each backend.", base, imp)
			}
		}
	}
}

// TestBackendEntryPointsAreNotCalledDirectly guards the same boundary from
// the other side.
//
// The GLFW rule can be enforced by import path, but a native backend has no
// import to look for — its C entry points are visible to every file in the
// package. That makes "just call quiCocoaNSWindow here" an easy shortcut,
// and each one is a place the engine stops being backend-agnostic.
//
// Note this is narrower than "no Cocoa outside the backend": several files
// legitimately use AppKit for things that are not windowing (IME, native
// menus, dialogs, tray, clipboard, printing). Those are feature bridges that
// reach a window through the seam's nativeWindow, which is the sanctioned
// door. What's forbidden is bypassing the seam to drive the window, the
// event loop or presentation.
func TestBackendEntryPointsAreNotCalledDirectly(t *testing.T) {
	const prefix = "quiCocoa"
	allowed := []string{"platform_cocoa"}

	paths, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatalf("glob: %v", err)
	}
	for _, path := range paths {
		base := filepath.Base(path)
		if strings.HasSuffix(base, "_test.go") || isAllowedPlatformFile(base, allowed) {
			continue
		}
		src, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		if strings.Contains(string(src), "C."+prefix) {
			t.Errorf("%s calls a %s* backend entry point directly.\n"+
				"Go through the seam: platformWindow / platformApp in platform.go.\n"+
				"If the capability is missing there, add it and implement it in every backend.",
				base, prefix)
		}
	}
}

func isAllowedPlatformFile(base string, allowed []string) bool {
	for _, prefix := range allowed {
		if strings.HasPrefix(base, prefix) {
			return true
		}
	}
	return false
}

// TestHandlerContractIsFullyImplemented pins the direction of the seam:
// *Window is the engine's handler, and a backend that grows a new event
// must break at compile time until Window handles it.
//
// The compile-time assertion in window_handler.go already enforces this;
// keeping a named test here means the intent survives a future refactor
// that deletes the bare `var _ platformHandler` line as "unused".
func TestHandlerContractIsFullyImplemented(t *testing.T) {
	var h platformHandler = NewTestWindow(Size{W: 1, H: 1})
	if h == nil {
		t.Fatal("Window must implement platformHandler")
	}
}
