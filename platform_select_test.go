package qui

import (
	"strings"
	"testing"
)

// The selector is what makes running two backends side by side possible,
// so its failure modes matter as much as its success: a typo'd
// QUI_PLATFORM must be loud, and the default must not drift onto a
// backend that hasn't earned it.

func TestGLFWBackendIsRegisteredAndDefault(t *testing.T) {
	if _, ok := platformBackends["glfw"]; !ok {
		t.Fatalf("glfw backend not registered; available: %v", availablePlatformBackends())
	}
	if defaultPlatformBackend != "glfw" {
		t.Fatalf("default backend is %q; it should stay the most proven one until a "+
			"native backend has been verified against real apps", defaultPlatformBackend)
	}
	if _, ok := platformBackends[defaultPlatformBackend]; !ok {
		t.Fatalf("default backend %q is not registered", defaultPlatformBackend)
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
	if name != defaultPlatformBackend {
		t.Fatalf("blank QUI_PLATFORM resolved to %q, want %q", name, defaultPlatformBackend)
	}
}

func TestEveryRegisteredBackendHasAFactory(t *testing.T) {
	names := availablePlatformBackends()
	if len(names) == 0 {
		t.Fatal("no platform backends registered")
	}
	for _, name := range names {
		if platformBackends[name] == nil {
			t.Errorf("backend %q registered with a nil factory", name)
		}
	}
}
