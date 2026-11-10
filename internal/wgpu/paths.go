package wgpu

import (
	"os"
	"path/filepath"
	"runtime"
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
// to the executable, the app bundle's Frameworks directory, the user cache
// (cmd/qui-dawn), and finally the bare name (the system search path).
func LibraryCandidates() []string {
	name := LibraryName()
	var out []string
	if p := os.Getenv("QUI_DAWN_LIB"); p != "" {
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
	return append(out, name)
}
