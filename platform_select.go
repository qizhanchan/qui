package qui

import (
	"fmt"
	"os"
	"sort"
	"strings"
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
// The default is the most proven backend for the platform, not the newest.
type platformFactory func() (platformApp, error)

// platformBackends is the registry of compiled-in backends. Populated by
// each backend's own file in an init, so adding one touches nothing here.
var platformBackends = map[string]platformFactory{}

// defaultPlatformBackend is the name used when QUI_PLATFORM is unset.
// Backends override it only when they are the better default on their
// platform; it stays "glfw" until a native backend has proven itself.
var defaultPlatformBackend = "glfw"

// registerPlatformBackend adds a backend under name. Called from init.
func registerPlatformBackend(name string, factory platformFactory) {
	platformBackends[name] = factory
}

// newPlatformApp creates the platform backend named by QUI_PLATFORM, or
// the platform default.
//
// An unrecognized name is an error rather than a silent fallback: someone
// who sets QUI_PLATFORM=cocao wants to know they typo'd, not to spend an
// afternoon wondering why their measurements didn't change.
func newPlatformApp() (platformApp, error) {
	name := resolvePlatformName()
	factory, ok := platformBackends[name]
	if !ok {
		return nil, fmt.Errorf("qui: unknown QUI_PLATFORM %q (available: %s)",
			name, strings.Join(availablePlatformBackends(), ", "))
	}
	return factory()
}

// resolvePlatformName reads the requested backend name, treating an unset
// or whitespace-only value as "use the default" — a script exporting an
// empty variable should behave like not setting it at all.
func resolvePlatformName() string {
	if name := strings.TrimSpace(os.Getenv("QUI_PLATFORM")); name != "" {
		return name
	}
	return defaultPlatformBackend
}

// availablePlatformBackends lists the compiled-in backend names, sorted.
func availablePlatformBackends() []string {
	names := make([]string, 0, len(platformBackends))
	for name := range platformBackends {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}
