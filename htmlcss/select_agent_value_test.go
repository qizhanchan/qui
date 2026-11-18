package htmlcss

import (
	"encoding/json"
	"testing"

	"github.com/qizhanchan/qui"
)

func TestAgentSelectWrappedOptionByValue(t *testing.T) {
	win := qui.NewTestWindow(qui.Size{W: 400, H: 300})
	res := RenderDoc(`<body><select id="language"><option value="en">English</option><option value="fr">Français</option><option value="de" disabled>Deutsch</option></select></body>`, ``, Options{Window: win, ScriptDOM: true})
	win.SetRoot(res.Root)
	res.Root.Layout(qui.Rect{W: 400, H: 300})
	el, sel := selectWidget(t, res.ByID["language"])
	var changed string
	el.SetOnSelect(func(_ int, value string) { changed = value })
	cmd, ok := qui.LookupProtocolCommand("Agent.selectOption")
	if !ok {
		t.Fatal("missing select command")
	}
	choose := func(value string) error {
		p, _ := json.Marshal(map[string]any{"target": el.NodeID().String(), "value": value})
		_, err := cmd.Call(&qui.ProtocolContext{Window: win}, p)
		return err
	}
	if err := choose("fr"); err != nil {
		t.Fatal(err)
	}
	if sel.SelectedIdx != 1 || changed != "fr" {
		t.Fatalf("selected=%d changed=%q", sel.SelectedIdx, changed)
	}
	if err := choose("de"); err == nil || sel.SelectedIdx != 1 {
		t.Fatalf("disabled option selected: err=%v index=%d", err, sel.SelectedIdx)
	}
	// Changing DOM option values after mounting must update the same AX list.
	el.SetSelectValues([]string{"eng", "fra", "deu"})
	if err := choose("eng"); err != nil || sel.SelectedIdx != 0 || changed != "eng" {
		t.Fatalf("updated value: err=%v index=%d changed=%q", err, sel.SelectedIdx, changed)
	}
}
