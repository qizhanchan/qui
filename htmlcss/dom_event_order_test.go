package htmlcss

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/qizhanchan/qui"
	"github.com/qizhanchan/qui/widgets"
)

// Differential test: qui's htmlcss event ordering against real Chrome.
//
// Each fixture in testdata/events is an HTML page (<name>.html) plus an input
// script (<name>.steps.json). widgets/scripts/web/dom-events.mjs loads the
// page in Chrome, puts a listener for every logged event type on every
// element with an id, replays the script and records, per step, the ordered
// "<type> <id> <phase>" entries plus the default actions that followed
// ("@focus <id>", "@value <id> <json>", "submit <form> target"). The result
// is committed as <name>.golden.json, so this test never needs Chrome.
//
// Here the same page is rendered through htmlcss, the same listeners are
// wired through the El SetOn* APIs, the same script is replayed with
// synthetic window events, and the two logs are diffed step by step.
//
// Refresh the goldens (needs Chrome + puppeteer-core, see the script):
//
//	REFRESH_REF=1 go test ./htmlcss -run DOMEventOrder
//
// A difference qui knowingly has goes in domEventAllowlist with its reason.
// An allowlisted cell that starts matching Chrome fails the test, so the
// list shrinks as fixes land instead of hiding them.

// domEventAllow excuses one cell — every entry matching `cell`
// ("<type> <id> [phase]", any part may be "*") in the fixtures matching
// `fixture` and the steps matching `step` (path.Match patterns over names
// like "text-fields" and step descriptions like "click label",
// "key Enter", "rightclick *"; a pattern starting with "#" matches the
// indexed form "#2 key Enter" to single out one of several alike steps)
// — from the comparison.
type domEventAllow struct {
	fixture, step, cell, reason string
}

var domEventAllowlist = []domEventAllow{
	// By design: an El click handler acts on the release and consumes it
	// (nested_click_test.go — a ✕ inside a clickable tab row), so click
	// and dblclick stop at the innermost handler instead of bubbling.
	{"*", "*", "click * bubble", "a click handler consumes the release: click doesn't bubble (nested_click_test.go)"},
	{"*", "*", "dblclick * bubble", "a dblclick handler consumes the release: dblclick doesn't bubble"},
	{"*", "*click *", "pointerup * bubble", "the release a click handler consumed never reaches ancestors' pointerup listeners"},
	{"*", "rightclick *", "pointerdown * bubble", "SetOnContextMenu consumes the right press, so ancestors miss its pointerdown"},
	{"*", "rightclick *", "contextmenu * bubble", "a contextmenu handler consumes the press: contextmenu doesn't bubble"},

	// Form controls: the backing widget eats the pointer events before the
	// element's listeners see them (session form-control-pointer-eve).
	{"text-fields", "wheel area", "wheel *", "<textarea> swallows wheel"},
	{"choice-controls", "click sel", "* *", "<select> opens its popup on press: focus leaves for the popup and pointer/click listeners don't run"},
	{"choice-controls", "key Escape", "* sel", "the open <select> popup eats Escape's keydown and refocuses the select"},


	// A push button doesn't take focus on click (the macOS convention, see
	// El.FocusOnClick); Chrome focuses it.
	{"nested-click", "*click btn", "focus btn", "a clicked button doesn't take focus (macOS convention)"},
	{"nested-click", "*click btn", "@focus *", "a clicked button doesn't take focus"},
	{"nested-click", "wheel plain", "@focus *", "the right-clicked button never took focus"},
	{"nested-click", "click tabinner", "blur btn", "the earlier-clicked button never took focus, so there is nothing to blur"},
	{"text-fields", "click submit", "focus submit", "a clicked button doesn't take focus (macOS convention)"},
	{"text-fields", "click submit", "blur text", "a clicked button doesn't take focus, so the field keeps it"},
	{"text-fields", "click submit", "@focus *", "a clicked button doesn't take focus"},

	// Keyboard.
	{"text-fields", "*", "@value area", "Tab focuses the <textarea> El itself (a key listener makes it Focusable) instead of its TextArea, so typed keys are lost"},
	{"text-fields", "click area", "blur area", "the Tab-focused <textarea> El hands focus to its TextArea on click: a spurious blur"},
	{"text-fields", "click area", "focus area", "the Tab-focused <textarea> El hands focus to its TextArea on click: a spurious focus"},
	{"*", "key Tab", "keydown *", "the window acts on Tab before keydown listeners: they never see it"},
	{"*", "key Tab", "focus *", "Tab focuses the next element before blurring the current one"},
	{"choice-controls", "key Space", "click *", "Space toggles a checkbox / presses a button without firing click"},
	{"text-fields", "#2 key Enter", "click submit", "implicit submission (Enter in a field) doesn't fire the submit button's click"},
	{"text-fields", "#2 key Enter", "keydown fields", "Enter that submits the form doesn't bubble its keydown past the <input>"},
}

type domEventStep struct {
	Do  string  `json:"do"`
	On  string  `json:"on,omitempty"`
	Key string  `json:"key,omitempty"`
	DY  float32 `json:"dy,omitempty"`
}

func (s domEventStep) String() string {
	if s.Do == "key" {
		return "key " + s.Key
	}
	return s.Do + " " + s.On
}

type domEventGolden struct {
	Fixture string `json:"fixture"`
	Log     []struct {
		Step    domEventStep `json:"step"`
		Entries []string     `json:"entries"`
	} `json:"log"`
}

func TestDOMEventOrder(t *testing.T) {
	dir := filepath.Join("testdata", "events")
	if os.Getenv("REFRESH_REF") == "1" {
		node := os.Getenv("NODE_BIN")
		if node == "" {
			node = "node"
		}
		cmd := exec.Command(node, filepath.Join("..", "widgets", "scripts", "web", "dom-events.mjs"))
		cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
		if err := cmd.Run(); err != nil {
			t.Fatalf("regenerating goldens: %v", err)
		}
	}
	stepFiles, err := filepath.Glob(filepath.Join(dir, "*.steps.json"))
	if err != nil || len(stepFiles) == 0 {
		t.Fatalf("no fixtures in %s (%v)", dir, err)
	}
	used := make([]bool, len(domEventAllowlist))
	for _, sf := range stepFiles {
		name := strings.TrimSuffix(filepath.Base(sf), ".steps.json")
		t.Run(name, func(t *testing.T) { runDOMEventFixture(t, dir, name, used) })
	}
	if t.Failed() {
		return
	}
	for i, a := range domEventAllowlist {
		if !used[i] {
			t.Errorf("allowlist entry {%s / %s / %s} matches no difference any more — qui agrees with Chrome now, delete it",
				a.fixture, a.step, a.cell)
		}
	}
}

func runDOMEventFixture(t *testing.T, dir, name string, used []bool) {
	html, err := os.ReadFile(filepath.Join(dir, name+".html"))
	if err != nil {
		t.Fatal(err)
	}
	var steps []domEventStep
	readJSON(t, filepath.Join(dir, name+".steps.json"), &steps)
	var golden domEventGolden
	readJSON(t, filepath.Join(dir, name+".golden.json"), &golden)
	if len(golden.Log) != len(steps) {
		t.Fatalf("golden has %d steps, steps.json %d — regenerate with REFRESH_REF=1", len(golden.Log), len(steps))
	}
	for i, s := range steps {
		if golden.Log[i].Step != s {
			t.Fatalf("step %d is %v in steps.json but %v in the golden — regenerate with REFRESH_REF=1",
				i, s, golden.Log[i].Step)
		}
	}

	h := newDOMEventHarness(t, string(html))
	for i, s := range steps {
		want := golden.Log[i].Entries
		got := h.run(s, want)
		for id := range h.unobserved {
			for _, typ := range []string{"keydown", "keyup"} {
				want = filterCells(want, typ+" "+id, false)
				got = filterCells(got, typ+" "+id, false)
			}
		}
		var excused []int
		for ai, a := range domEventAllowlist {
			fixOK, _ := path.Match(a.fixture, name)
			stepOK, _ := path.Match(a.step, s.String())
			if strings.HasPrefix(a.step, "#") {
				stepOK, _ = path.Match(a.step, fmt.Sprintf("#%d %v", i, s))
			}
			if fixOK && stepOK {
				excused = append(excused, ai)
			}
		}
		// Compare with every excused cell dropped; an entry is "used" when
		// the step would differ without it (its cell differs, or its
		// entries sit in a different order relative to the rest).
		strip := func(skip int) (w, g []string) {
			w, g = want, got
			for _, ai := range excused {
				if ai != skip {
					w = filterCells(w, domEventAllowlist[ai].cell, false)
					g = filterCells(g, domEventAllowlist[ai].cell, false)
				}
			}
			return w, g
		}
		for _, ai := range excused {
			if w, g := strip(ai); !equalStrings(w, g) {
				used[ai] = true
			}
		}
		w, g := strip(-1)
		if !equalStrings(w, g) {
			t.Errorf("step %d (%v) differs from Chrome (- chrome, + qui):\n%s", i, s, lineDiff(w, g))
		}
	}
}

// domEventHarness is one rendered fixture with every id'd element logging
// into log.
type domEventHarness struct {
	t    *testing.T
	win  *qui.Window
	root qui.Widget
	byID map[string]*El
	idOf map[*El]string
	// unobserved: ids with no key listener (see domFocusable); their
	// keydown / keyup entries are dropped from both logs.
	unobserved map[string]bool
	log        []string
	clock      time.Time
	// target is the element the event being dispatched is aimed at (the
	// DOM's event.target): listeners on it log "target", ancestors "bubble".
	target *El
}

func newDOMEventHarness(t *testing.T, html string) *domEventHarness {
	res := RenderDoc(html, "", Options{ViewportWidth: 480})
	h := &domEventHarness{
		t: t, root: res.Root,
		byID:       map[string]*El{},
		idOf:       map[*El]string{},
		unobserved: map[string]bool{},
		clock:      time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
	}
	h.win = qui.NewTestWindow(qui.Size{W: 480, H: 360})
	h.win.SetRoot(res.Root)
	for id, w := range res.ByID {
		el, ok := w.(*El)
		if !ok {
			continue
		}
		h.byID[id], h.idOf[el] = el, id
		h.wire(el, id)
	}
	h.layout()
	return h
}

func (h *domEventHarness) layout() {
	h.root.Measure(qui.Size{W: 480, H: 0})
	h.root.Layout(qui.Rect{W: 480, H: 360})
}

func (h *domEventHarness) logEvent(el *El, typ string) {
	phase := "bubble"
	if el == h.target {
		phase = "target"
	}
	h.log = append(h.log, typ+" "+h.idOf[el]+" "+phase)
}

func (h *domEventHarness) wire(el *El, id string) {
	el.SetOnPointerDown(func(PointerEvent) bool { h.logEvent(el, "pointerdown"); return false })
	el.SetOnPointerUp(func(PointerEvent) bool { h.logEvent(el, "pointerup"); return false })
	el.SetOnClick(func() { h.logEvent(el, "click") })
	el.SetOnDoubleClick(func() { h.logEvent(el, "dblclick") })
	el.SetOnContextMenu(func(x, y float32) { h.logEvent(el, "contextmenu") })
	el.SetOnWheel(func(dx, dy float32) bool { h.logEvent(el, "wheel"); return false })
	if domFocusable(el) {
		el.SetOnKeyDown(func(qui.KeyEvent) bool { h.logEvent(el, "keydown"); return false })
		el.SetOnKeyUp(func(qui.KeyEvent) bool { h.logEvent(el, "keyup"); return false })
	} else {
		h.unobserved[id] = true
	}
	// focus / blur don't bubble: whoever hears them is the target.
	el.SetOnFocus(func() { h.log = append(h.log, "focus "+id+" target") })
	el.SetOnBlur(func() { h.log = append(h.log, "blur "+id+" target") })
	if el.Tag() == "form" {
		el.SetOnFormSubmit(func(map[string]string) { h.log = append(h.log, "submit "+id+" target") })
	}
}

// domFocusable reports whether the DOM can focus el. Only those elements
// get key listeners: SetOnKeyDown also makes an element focusable (see
// El.Focusable), so a keydown listener on a plain <div> or <span> would
// change where clicks put focus — a side effect a DOM listener doesn't
// have — and cascade into every later step. Keys bubbling to a plain
// ancestor are therefore not compared; a tabindex ancestor (text-fields'
// #fields) covers the bubbling path instead.
func domFocusable(el *El) bool {
	if _, ok := el.Attr("tabindex"); ok {
		return true
	}
	switch el.Tag() {
	case "input", "textarea", "select", "button":
		return true
	}
	return false
}

// owner maps a hit / focused widget to the nearest id'd element above it —
// the element Chrome would report as event.target in these fixtures, where
// every element that matters has an id.
func (h *domEventHarness) owner(w qui.Widget) *El {
	for ; w != nil; w = w.Parent() {
		if el, ok := w.(*El); ok {
			if _, has := h.idOf[el]; has {
				return el
			}
		}
	}
	return nil
}

// focusedEl is the element holding focus, or nil (document.body). Focus
// on an element's own backing control counts as the element's; focus that
// a click on plain text parks on its selectable label (for Cmd+C) is not
// DOM focus — document.activeElement stays <body> there.
func (h *domEventHarness) focusedEl() *El {
	f := h.win.Focused()
	for w := f; w != nil; w = w.Parent() {
		if el, ok := w.(*El); ok {
			if _, has := h.idOf[el]; has && (f == qui.Widget(el) || (el.backing != nil && f == el.backing)) {
				return el
			}
			return nil
		}
	}
	return nil
}

func (h *domEventHarness) center(id string) (float32, float32) {
	el := h.byID[id]
	if el == nil {
		h.t.Fatalf("fixture has no element #%s", id)
	}
	b := el.Bounds()
	p := qui.LocalPointToWindow(el, qui.Point{X: b.X + b.W/2, Y: b.Y + b.H/2})
	return p.X, p.Y
}

func (h *domEventHarness) mouse(kind qui.EventType, x, y float32, button qui.MouseButton, clicks int) {
	h.target = h.owner(h.win.HitTestForTest(qui.Point{X: x, Y: y}))
	me := qui.NewMouseEvent(kind, x, y, button, 0)
	me.When = h.clock
	me.Clicks = clicks
	h.win.DispatchTestEvent(me)
	h.clock = h.clock.Add(30 * time.Millisecond)
}

func (h *domEventHarness) press(x, y float32, button qui.MouseButton, clicks int) {
	h.mouse(qui.EventMouseDown, x, y, button, clicks)
	h.mouse(qui.EventMouseUp, x, y, button, clicks)
}

// domEventKeys maps a steps.json key name to a qui key and the character
// it types (0 for none), as the platform layer would deliver it.
func domEventKey(t *testing.T, name string) (qui.Key, rune) {
	switch name {
	case "Space":
		k, _, _ := qui.ParseShortcut("Space")
		return k, ' '
	case "Enter", "Tab", "Escape":
		k, _, err := qui.ParseShortcut(name)
		if err != nil {
			t.Fatal(err)
		}
		return k, 0
	}
	if r, n := utf8.DecodeRuneInString(name); n == len(name) {
		k, _, err := qui.ParseShortcut(strings.ToUpper(name))
		if err != nil {
			t.Fatal(err)
		}
		return k, r
	}
	t.Fatalf("unknown key %q", name)
	return 0, 0
}

func (h *domEventHarness) key(name string) {
	k, r := domEventKey(h.t, name)
	h.target = h.focusedEl()
	h.win.DispatchTestEvent(qui.NewKeyEvent(qui.EventKeyDown, k, 0))
	if r != 0 {
		h.win.DispatchTestEvent(qui.NewCharEvent(r, 0))
	}
	// Keyup goes to wherever focus is now (after a Tab, the next field).
	h.target = h.focusedEl()
	h.win.DispatchTestEvent(qui.NewKeyEvent(qui.EventKeyUp, k, 0))
}

// run replays one step and returns its log in the golden's format. want is
// consulted only for which form controls' values the golden records.
func (h *domEventHarness) run(s domEventStep, want []string) []string {
	h.log = nil
	switch s.Do {
	case "click", "dblclick", "rightclick", "wheel":
		x, y := h.center(s.On)
		h.target = h.owner(h.win.HitTestForTest(qui.Point{X: x, Y: y}))
		h.win.DispatchTestEvent(qui.NewMouseEvent(qui.EventMouseMove, x, y, 0, 0))
		switch s.Do {
		case "click":
			h.press(x, y, qui.MouseButtonLeft, 1)
		case "dblclick":
			h.press(x, y, qui.MouseButtonLeft, 1)
			h.press(x, y, qui.MouseButtonLeft, 2)
		case "rightclick":
			h.press(x, y, qui.MouseButtonRight, 1)
		case "wheel":
			h.win.DispatchTestEvent(qui.NewScrollEvent(x, y, 0, -s.DY, 0))
		}
	case "key":
		h.key(s.Key)
	default:
		h.t.Fatalf("unknown step %v", s)
	}
	h.win.DrainJobsForTest()
	h.layout()
	// Keep consecutive steps out of the double-click window, as the
	// recorder's 600 ms pause does.
	h.clock = h.clock.Add(time.Second)

	out := append([]string(nil), h.log...)
	focus := "-"
	if el := h.focusedEl(); el != nil {
		focus = h.idOf[el]
	}
	out = append(out, "@focus "+focus)
	for _, e := range want {
		if f := strings.Fields(e); len(f) >= 2 && f[0] == "@value" {
			v, _ := json.Marshal(h.value(f[1]))
			out = append(out, "@value "+f[1]+" "+string(v))
		}
	}
	return out
}

// value reads a form control the way the DOM reports it: .value, or
// .checked as "true"/"false" for a checkbox / radio.
func (h *domEventHarness) value(id string) string {
	el := h.byID[id]
	if el == nil {
		return "<missing>"
	}
	el.ensureBacking()
	switch b := el.backing.(type) {
	case *widgets.CheckBox:
		return fmt.Sprint(b.Checked)
	case *widgets.RadioButton:
		return fmt.Sprint(b.Checked)
	case *widgets.Select:
		return el.selectValueAt(b.SelectedIdx)
	case interface{ GetText() string }:
		return b.GetText()
	}
	return fmt.Sprintf("<%T>", el.backing)
}

// filterCells keeps (keep=true) or drops the entries matching cell:
// "<type> <id> [phase]", any field "*" for a wildcard.
func filterCells(entries []string, cell string, keep bool) []string {
	pat := strings.Fields(cell)
	var out []string
	for _, e := range entries {
		f := strings.Fields(e)
		match := len(f) >= len(pat)
		for i := 0; match && i < len(pat); i++ {
			match = pat[i] == "*" || pat[i] == f[i]
		}
		if match == keep {
			out = append(out, e)
		}
	}
	return out
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// lineDiff renders an LCS diff of two entry lists.
func lineDiff(a, b []string) string {
	n, m := len(a), len(b)
	lcs := make([][]int, n+1)
	for i := range lcs {
		lcs[i] = make([]int, m+1)
	}
	for i := n - 1; i >= 0; i-- {
		for j := m - 1; j >= 0; j-- {
			if a[i] == b[j] {
				lcs[i][j] = lcs[i+1][j+1] + 1
			} else {
				lcs[i][j] = max(lcs[i+1][j], lcs[i][j+1])
			}
		}
	}
	var sb strings.Builder
	i, j := 0, 0
	for i < n || j < m {
		switch {
		case i < n && j < m && a[i] == b[j]:
			fmt.Fprintf(&sb, "      %s\n", a[i])
			i, j = i+1, j+1
		case j < m && (i == n || lcs[i][j+1] >= lcs[i+1][j]):
			fmt.Fprintf(&sb, "    + %s\n", b[j])
			j++
		default:
			fmt.Fprintf(&sb, "    - %s\n", a[i])
			i++
		}
	}
	return sb.String()
}

func readJSON(t *testing.T, path string, v any) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, v); err != nil {
		t.Fatalf("%s: %v", path, err)
	}
}
