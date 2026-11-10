package wgpu

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// LibraryName is the Dawn shared library's file name on this OS.
func LibraryName() string {
	switch runtime.GOOS {
	case "darwin":
		return "libwebgpu_dawn.dylib"
	case "windows":
		return "webgpu_dawn.dll"
	}
	return "libwebgpu_dawn.so"
}

// CacheDir is where cmd/qui-dawn installs the library for this revision:
// <user cache>/qui/dawn/<revision>/<os>-<arch>.
func CacheDir() string {
	base, err := os.UserCacheDir()
	if err != nil {
		return ""
	}
	return filepath.Join(base, "qui", "dawn", DawnRevision[:12], runtime.GOOS+"-"+runtime.GOARCH)
}

// LibraryCandidates lists where Load looks, in order: $QUI_DAWN_LIB, next
// to the executable, the app bundle's Frameworks directory and the user
// cache (cmd/qui-dawn). Every candidate is an absolute path: a bare name
// would let dlopen search DYLD_* / LD_LIBRARY_PATH (and, on macOS, the
// working directory) and pick up a library of an unknown revision.
func LibraryCandidates() []string {
	name := LibraryName()
	var out []string
	if p := os.Getenv("QUI_DAWN_LIB"); p != "" {
		if a, err := filepath.Abs(p); err == nil {
			p = a
		}
		out = append(out, p)
	}
	if exe, err := os.Executable(); err == nil {
		if r, err := filepath.EvalSymlinks(exe); err == nil {
			exe = r
		}
		dir := filepath.Dir(exe)
		out = append(out, filepath.Join(dir, name))
		if runtime.GOOS == "darwin" {
			out = append(out, filepath.Join(dir, "..", "Frameworks", name))
		}
	}
	if d := CacheDir(); d != "" {
		out = append(out, filepath.Join(d, name))
	}
	return out
}

// RevisionMarkerPath is the file next to a Dawn library that records the
// revision it was built from (written by cmd/qui-dawn). Load skips a
// library whose marker names another revision: struct layouts and enum
// values are not stable across Dawn revisions, and a stale copy can still
// export every entry point.
func RevisionMarkerPath(lib string) string { return lib + ".revision" }

func readRevisionMarker(lib string) (string, bool) {
	b, err := os.ReadFile(RevisionMarkerPath(lib))
	if err != nil {
		return "", false
	}
	return strings.TrimSpace(string(b)), true
}
