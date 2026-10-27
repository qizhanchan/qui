// Package discovery is how agent servers announce themselves and clients
// find them: one JSON file per running app in Dir, named after the app.
package discovery

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Dir is where servers announce themselves.
func Dir() string { return filepath.Join(os.TempDir(), "qui-agent") }

// Info is the content of a discovery file.
type Info struct {
	Name      string `json:"name"`
	PID       int    `json:"pid"`
	Socket    string `json:"socket,omitempty"`
	TCP       string `json:"tcp,omitempty"`
	StartedAt string `json:"startedAt"`
}

// Path is the discovery file for app name.
func Path(name string) string { return filepath.Join(Dir(), SafeName(name)+".json") }

// SafeName keeps a discovery file name inside Dir.
func SafeName(name string) string {
	var b strings.Builder
	for _, r := range name {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_', r == '.':
			b.WriteRune(r)
		default:
			b.WriteByte('_')
		}
	}
	out := strings.Trim(b.String(), ".")
	if out == "" {
		return "app"
	}
	return out
}

// Read loads a discovery file, failing when its process is gone.
func Read(path string) (Info, error) {
	var info Info
	b, err := os.ReadFile(path)
	if err != nil {
		return info, err
	}
	if err := json.Unmarshal(b, &info); err != nil {
		return info, err
	}
	if !ProcessAlive(info.PID) {
		return info, fmt.Errorf("pid %d is gone (stale discovery file)", info.PID)
	}
	return info, nil
}

// Running lists the live apps in Dir, by name.
func Running() []Info {
	files, _ := filepath.Glob(filepath.Join(Dir(), "*.json"))
	var out []Info
	for _, f := range files {
		if info, err := Read(f); err == nil {
			out = append(out, info)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}
