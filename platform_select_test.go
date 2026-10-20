package qui

import (
	"runtime"
	"strings"
	"testing"

	"github.com/qizhanchan/qui/internal/platform/contract"
)

// The selector is what makes running two backends side by side possible,
// so its failure modes matter as much as its success: a typo'd
// QUI_PLATFORM must be loud, and the default must not drift onto a
// backend that hasn't earned it.

func TestDefaultBackendIsTheNativeOne(t *testing.T) {
	if _, ok := contract.Lookup("headless"); !ok {
		t.Fatalf("headless backend not registered; available: %v", availablePlatformBackends())
	}
	def := defaultPlatformBackend()
	defBackend, ok := contract.Lookup(def)
	if !ok {
		t.Fatalf("default backend %q is not registered", def)
	}
	// Whatever native backend this build has must win over GLFW and
	// headless — the whole point of priorities.
	for _, name := range contract.Names() {
		if b, _ := contract.Lookup(name); b.Priority > defBackend.Priority {
			t.Fatalf("default %q outranked by %q", def, name)
		}
	}
	if runtime.GOOS == "darwin" && cgoEnabled && def != "cocoa" {
		t.Fatalf("darwin+cgo default is %q, want cocoa", def)
	}
}

func TestUnknownPlatformNameIsAnError(t *testing.T) {
	t.Setenv("QUI_PLATFORM", "cocao") // a plausible typo
	_, err := newPlatformApp()
	if err == nil {
		t.Fatal("expected an error for an unknown backend name")
	}
	// The message must name what was asked for and what exists — otherwise
	// the user's only signal is that nothing changed.
	if !strings.Contains(err.Error(), "cocao") {
		t.Errorf("error should quote the bad name, got: %v", err)
	}
	for _, name := range availablePlatformBackends() {
		if !strings.Contains(err.Error(), name) {
			t.Errorf("error should list available backend %q, got: %v", name, err)
		}
	}
}

func TestBlankPlatformNameFallsBackToTheDefault(t *testing.T) {
	// Whitespace is what an env var set from a script's variable ends up
	// as when the variable was empty; it must behave as "unset", not as an
	// unknown backend.
	t.Setenv("QUI_PLATFORM", "   ")
	name := resolvePlatformName()
	if name != defaultPlatformBackend() {
		t.Fatalf("blank QUI_PLATFORM resolved to %q, want %q", name, defaultPlatformBackend())
	}
}

func TestEveryRegisteredBackendHasAFactory(t *testing.T) {
	names := availablePlatformBackends()
	if len(names) == 0 {
		t.Fatal("no platform backends registered")
	}
	for _, name := range names {
		if b, _ := contract.Lookup(name); b.Factory == nil {
			t.Errorf("backend %q registered with a nil factory", name)
		}
	}
}
