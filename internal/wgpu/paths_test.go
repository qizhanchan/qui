package wgpu

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLibraryCandidatesAreAbsolute(t *testing.T) {
	t.Setenv("QUI_DAWN_LIB", "rel/libwebgpu_dawn.dylib")
	for _, p := range LibraryCandidates() {
		if !filepath.IsAbs(p) {
			t.Errorf("candidate %q is not absolute (dlopen would search the system paths)", p)
		}
	}
}

func TestRevisionMarker(t *testing.T) {
	lib := filepath.Join(t.TempDir(), LibraryName())
	if _, ok := readRevisionMarker(lib); ok {
		t.Fatal("marker found without a file")
	}
	if err := os.WriteFile(RevisionMarkerPath(lib), []byte(DawnRevision+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if rev, ok := readRevisionMarker(lib); !ok || rev != DawnRevision {
		t.Fatalf("marker = %q, %v", rev, ok)
	}
}
