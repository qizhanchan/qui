package typeset

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSystemFontCacheWritableFallback(t *testing.T) {
	cache := t.TempDir()
	dir, cleanup := writableSystemFontCacheDir(cache)
	if dir != cache {
		t.Fatalf("writable cache changed: %q", dir)
	}
	cleanup()
	if _, err := os.Stat(cache); err != nil {
		t.Fatal("removed user's cache", err)
	}
	// A file where a directory was expected deterministically models an
	// unavailable cache, including on machines running tests as root.
	blocked := filepath.Join(cache, "not-a-directory")
	if err := os.WriteFile(blocked, nil, 0600); err != nil {
		t.Fatal(err)
	}
	dir, cleanup = writableSystemFontCacheDir(blocked)
	if dir == "" || dir == blocked {
		t.Fatalf("no temporary fallback: %q", dir)
	}
	probe := filepath.Join(dir, "index")
	if err := os.WriteFile(probe, nil, 0600); err != nil {
		t.Fatal(err)
	}
	cleanup()
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Fatalf("temporary index not cleaned: %v", err)
	}
}
