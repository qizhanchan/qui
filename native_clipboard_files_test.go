package qui

import (
	"reflect"
	"testing"
)

// fileClip is a provider that can carry files — the shape the macOS
// provider has.
type fileClip struct {
	plain string
	files []string
}

func (c *fileClip) Get() string             { return c.plain }
func (c *fileClip) Set(s string)            { c.plain, c.files = s, nil }
func (c *fileClip) SetFiles(paths []string) { c.files, c.plain = paths, "" }
func (c *fileClip) GetFiles() []string      { return c.files }

func TestSetClipboardFilesRoundTrips(t *testing.T) {
	fc := &fileClip{}
	SetClipboardProvider(fc)
	t.Cleanup(func() { SetClipboardProvider(nil) })

	paths := []string{"/work/a.go", "/work/pkg"}
	SetClipboardFiles(paths)
	if got := GetClipboardFiles(); !reflect.DeepEqual(got, paths) {
		t.Errorf("GetClipboardFiles() = %q, want %q", got, paths)
	}
	// A text copy replaces the files.
	SetClipboardText("hello")
	if got := GetClipboardFiles(); got != nil {
		t.Errorf("files survived a text write: %q", got)
	}
}

// Where the clipboard cannot carry files, the paths go on it as text.
func TestSetClipboardFilesFallsBackToPaths(t *testing.T) {
	fc := &htmlClip{}
	SetClipboardProvider(fc)
	t.Cleanup(func() { SetClipboardProvider(nil) })

	SetClipboardFiles([]string{"/work/a.go", "/work/b.go"})
	if got, want := GetClipboardText(), "/work/a.go\n/work/b.go"; got != want {
		t.Errorf("text = %q, want %q", got, want)
	}
	if got := GetClipboardFiles(); got != nil {
		t.Errorf("GetClipboardFiles() = %q from a text-only clipboard", got)
	}
	SetClipboardFiles(nil) // nothing to copy: the clipboard is left alone
	if got := GetClipboardText(); got != "/work/a.go\n/work/b.go" {
		t.Errorf("empty SetClipboardFiles changed the clipboard to %q", got)
	}
}
