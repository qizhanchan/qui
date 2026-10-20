package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestParityCasesReturnsSortedJSONFixtures(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"z.json", "a.json", "ignored.txt"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("{}"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	paths, err := parityCases(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(paths) != 2 {
		t.Fatalf("paths = %v, want two JSON fixtures", paths)
	}
	if filepath.Base(paths[0]) != "a.json" || filepath.Base(paths[1]) != "z.json" {
		t.Fatalf("paths not sorted: %v", paths)
	}
}

func TestParityCasesRejectsEmptyDirectory(t *testing.T) {
	if _, err := parityCases(t.TempDir()); err == nil {
		t.Fatal("expected an error for an empty fixture directory")
	}
}
