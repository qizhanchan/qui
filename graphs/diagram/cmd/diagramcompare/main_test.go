package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestParityCasesReturnsSortedFixtures(t *testing.T) {
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
	if len(paths) != 2 || filepath.Base(paths[0]) != "a.json" || filepath.Base(paths[1]) != "z.json" {
		t.Fatalf("paths = %v", paths)
	}
}

func TestParityDirectoryHasTwelveValidFixtures(t *testing.T) {
	paths, err := parityCases("../../testdata/parity")
	if err != nil {
		t.Fatal(err)
	}
	if len(paths) != 12 {
		t.Fatalf("fixtures = %d, want 12", len(paths))
	}
	for _, path := range paths {
		if _, err := loadCase(path); err != nil {
			t.Errorf("%s: %v", filepath.Base(path), err)
		}
	}
}
