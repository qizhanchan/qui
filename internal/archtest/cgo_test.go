package archtest

import (
	"go/parser"
	"go/token"
	"path/filepath"
	"strconv"
	"testing"
)

// Inspect every source file, including inactive build tags: native text
// services belong behind the platform bridge, never inside the text engine.
func TestTypesetHasNoCgo(t *testing.T) {
	files, err := filepath.Glob(filepath.Join("..", "typeset", "*.go"))
	if err != nil || len(files) == 0 {
		t.Fatalf("find typeset sources: %v (%d files)", err, len(files))
	}
	for _, path := range files {
		f, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.ImportsOnly)
		if err != nil {
			t.Fatal(err)
		}
		for _, imp := range f.Imports {
			name, err := strconv.Unquote(imp.Path.Value)
			if err != nil {
				t.Fatal(err)
			}
			if name == "C" {
				t.Errorf("%s imports C; put native bridges in internal/platform/macos", path)
			}
		}
	}
}
