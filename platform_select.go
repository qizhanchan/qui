package qui

import (
	"fmt"
	"os"
	"strings"

	"github.com/qizhanchan/qui/internal/platform/contract"
)

// Backend selection.
//
// More than one implementation of the platform seam can be compiled in at
// once, and which one runs is a runtime choice via QUI_PLATFORM. That is
// deliberate rather than a build-tag switch:
//
//   - a native backend can be exercised against real apps while the
//     proven one stays the default, so "does the Cocoa backend regress
//     anything?" is one env var and a re-run rather than a rebuild;
//   - if a native backend turns out to misbehave on someone's machine
//     (an OS version, an unusual display setup), falling back is a
//     support instruction instead of a patch release;
//   - both implementations stay compiled, so neither can rot.
//
// The default is the platform's native backend where one exists (cocoa on
// macOS, x11 on Linux, web under GOOS=js); GLFW remains only where no
// native backend has been written yet (Windows) or when built with
// -tags qui_glfw. headless is compiled in everywhere as the fallback.
// installPlatformHost hands backends the engine services they call back
// into (contract.Host). Runs before the first backend is created.
func installPlatformHost() {
	contract.Host.ActivationPolicy = func() int { return int(ActivationPolicy()) }
	contract.Host.QuitRequested = runQuitRequest
	contract.Host.WakeIfBlocked = wakeIfBlocked
	contract.Host.NativeCursor = nativeCursorForBackend
	contract.Host.ImageCursor = imageCursorForBackend
	contract.Host.RawKeyCaptured = rawKeyCapturedHook()
}

// defaultPlatformBackend names the backend used when QUI_PLATFORM is
// unset: the highest-priority compiled-in backend (contract.Default).
func defaultPlatformBackend() string { return contract.Default() }

// newPlatformApp creates the platform backend named by QUI_PLATFORM, or
// the platform default.
//
// An unrecognized name is an error rather than a silent fallback: someone
// who sets QUI_PLATFORM=cocao wants to know they typo'd, not to spend an
// afternoon wondering why their measurements didn't change.
func newPlatformApp() (platformApp, error) {
	installPlatformHost()
	name := resolvePlatformName()
	backend, ok := contract.Lookup(name)
	if !ok {
		return nil, fmt.Errorf("qui: unknown QUI_PLATFORM %q (available: %s)",
			name, strings.Join(availablePlatformBackends(), ", "))
	}
	app, err := backend.Factory()
	if err != nil {
		return nil, err
	}
	return adaptApp(app), nil
}

// resolvePlatformName reads the requested backend name, treating an unset
// or whitespace-only value as "use the default" — a script exporting an
// empty variable should behave like not setting it at all.
func resolvePlatformName() string {
	if name := strings.TrimSpace(os.Getenv("QUI_PLATFORM")); name != "" {
		return name
	}
	return defaultPlatformBackend()
}

// availablePlatformBackends lists the compiled-in backend names, sorted.
func availablePlatformBackends() []string { return contract.Names() }
