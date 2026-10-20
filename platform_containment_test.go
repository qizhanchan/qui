package qui

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestWindowingLibraryStaysBehindTheSeam is an architectural guard, not a
// behavior test.
//
// The whole value of the platform seam is that exactly one package knows
// which windowing library is in use. That property is invisible at runtime
// and easy to erode: one `import "github.com/go-gl/glfw/..."` added for
// convenience — to reach a native handle, poll a key, wake the loop — and
// the engine quietly depends on GLFW again. This test makes that erosion a
// build failure instead of a slow leak.
//
// When you feel the urge to import a windowing library anywhere else, the
// seam is missing a method — add it to internal/platform/contract instead.
func TestWindowingLibraryStaysBehindTheSeam(t *testing.T) {
	forbidden := map[string]string{
		// import path → the only directory allowed to use it
		"github.com/go-gl/glfw": filepath.Join("internal", "platform", "glfw"),
	}
	walkGoFiles(t, func(path, src string) {
		for imp, home := range forbidden {
			if strings.Contains(src, `"`+imp) && filepath.Dir(path) != home {
				t.Errorf("%s imports %s directly.\n"+
					"Only %s may name that windowing library — if the engine needs\n"+
					"something from the OS, add it to internal/platform/contract and\n"+
					"implement it in every backend.", path, imp, home)
			}
		}
	})
}

// TestBackendsAreReachedOnlyThroughTheRegistry guards the same boundary
// from the engine's side: root names backend packages only in the blank
// imports of platform_backends*.go, and never calls a backend's C entry
// points (quiCocoa*) — those are visible to anything that links the
// package, which makes "just call quiCocoaNSWindow here" an easy shortcut.
//
// Several root files legitimately use AppKit for things that are not
// windowing (IME, native menus, dialogs, tray, clipboard, printing). Those
// are feature bridges that reach a window through the seam's NativeWindow,
// which is the sanctioned door.
func TestBackendsAreReachedOnlyThroughTheRegistry(t *testing.T) {
	paths, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatalf("glob: %v", err)
	}
	for _, path := range paths {
		if strings.HasSuffix(path, "_test.go") {
			continue
		}
		src, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		s := string(src)
		if strings.Contains(s, "C.quiCocoa") {
			t.Errorf("%s calls a quiCocoa* backend entry point directly; go through the seam", path)
		}
		if strings.HasPrefix(path, "platform_backends") {
			continue
		}
		for _, b := range []string{"cocoa", "x11", "web", "headless", "glfw"} {
			if strings.Contains(s, `"github.com/qizhanchan/qui/internal/platform/`+b+`"`) {
				t.Errorf("%s imports the %s backend; only platform_backends*.go may, as a blank import", path, b)
			}
		}
	}
}

// walkGoFiles calls fn for every non-test Go file in the module, skipping
// vendored code and agent worktrees.
func walkGoFiles(t *testing.T, fn func(path, src string)) {
	t.Helper()
	err := filepath.WalkDir(".", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			switch d.Name() {
			case "vendor", ".claude", ".git", "testdata":
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		src, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		fn(path, string(src))
		return nil
	})
	if err != nil {
		t.Fatalf("walk: %v", err)
	}
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
	//lint:ignore SA4023 the assignment above is the assertion
	if h == nil {
		t.Fatal("Window must implement platformHandler")
	}
}
