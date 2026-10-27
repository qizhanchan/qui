//go:build darwin && cgo

package qui

import "testing"

func TestMenuShortcutNamedKeys(t *testing.T) {
	cases := []struct {
		in   string
		key  string
		mods int
	}{
		{"Cmd+S", "S", 8},
		{"Cmd+Shift+F12", string(rune(0xF70F)), 9},
		{"Alt+Enter", "\r", 4},
		{"Cmd+Alt+Left", string(rune(0xF702)), 12},
		{"Shift+F6", string(rune(0xF709)), 1},
		{"Cmd+,", ",", 8},
	}
	for _, c := range cases {
		if k, m := parseShortcut(c.in); k != c.key || m != c.mods {
			t.Errorf("parseShortcut(%q) = %q,%d; want %q,%d", c.in, k, m, c.key, c.mods)
		}
	}
}
