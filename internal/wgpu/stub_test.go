//go:build cgo && !js && (darwin || linux)

package wgpu

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// stub_gen.go must mirror the cgo API: regenerate it and compare.
func TestStubUpToDate(t *testing.T) {
	if testing.Short() {
		t.Skip("runs go run")
	}
	out := filepath.Join(t.TempDir(), "stub_gen.go")
	cmd := exec.Command("go", "run", "gen/genstub.go", "-o", out)
	if b, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("genstub: %v\n%s", err, b)
	}
	want, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile("stub_gen.go")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want) {
		t.Fatal("stub_gen.go is stale: run go generate ./internal/wgpu")
	}
}
