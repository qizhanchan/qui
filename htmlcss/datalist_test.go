package htmlcss

import (
	"testing"

	"github.com/qizhanchan/qui"
	"github.com/qizhanchan/qui/widgets"
)

const datalistHTML = `<body>
  <form id="f">
    <input id="city" name="city" list="cities">
  </form>
  <datalist id="cities">
    <option value="Paris">
    <option value="Portland">
    <option>Berlin</option>
  </datalist>
</body>`

// datalistField mounts the fixture in a window (the popup needs an overlay
// host) and returns the input element plus its backing field.
func datalistField(t *testing.T) (*El, *widgets.Input, RenderResult) {
	t.Helper()
	win := qui.NewTestWindow(qui.Size{W: 400, H: 300})
	res := RenderDoc(datalistHTML, ``, Options{Window: win})
	win.SetRoot(res.Root)
	res.Root.Layout(qui.Rect{W: 400, H: 300})
	el := res.ByID["city"].(*El)
	return el, backingInput(t, el), res
}

// The linked <datalist>'s options become the field's suggestions: `value`
// wins, and an option with only text contributes its text.
func TestDatalistSuggestionsResolved(t *testing.T) {
	el, _, _ := datalistField(t)
	want := []string{"Paris", "Portland", "Berlin"}
	if len(el.suggestions) != len(want) {
		t.Fatalf("suggestions = %v, want %v", el.suggestions, want)
	}
	for i := range want {
		if el.suggestions[i] != want[i] {
			t.Fatalf("suggestions = %v, want %v", el.suggestions, want)
		}
	}
}

// <datalist> itself renders nothing.
func TestDatalistRendersNoBox(t *testing.T) {
	_, _, res := datalistField(t)
	if w := res.ByID["cities"]; w != nil {
		t.Errorf("<datalist> produced a widget (%T); it is a data holder", w)
	}
}

func TestMatchSuggestions(t *testing.T) {
	items := []string{"Paris", "Portland", "Berlin"}
	cases := []struct {
		typed string
		want  int
	}{
		{"", 3},      // empty offers everything
		{"p", 2},     // prefix
		{"P", 2},     // case-insensitive
		{"or", 1},    // substring, not just prefix (Chrome behavior): Portland
		{"lin", 1},   // Berlin
		{"zzz", 0},   // no match
		{"  p  ", 2}, // trimmed
	}
	for _, c := range cases {
		if got := len(matchSuggestions(items, c.typed)); got != c.want {
			t.Errorf("matchSuggestions(%q) = %d matches, want %d", c.typed, got, c.want)
		}
	}
}

// Typing opens the list; a filter that matches nothing closes it rather than
// showing an empty box.
func TestDatalistPopupOpensWhileTyping(t *testing.T) {
	el, in, _ := datalistField(t)
	if el.suggestOpen() {
		t.Fatal("popup should start closed")
	}
	in.SetText("por") // fires OnChange → refreshSuggestions
	if !el.suggestOpen() {
		t.Fatal("popup did not open while typing a matching prefix")
	}
	if len(el.suggestMatches) != 1 || el.suggestMatches[0] != "Portland" {
		t.Errorf("matches = %v, want [Portland]", el.suggestMatches)
	}
	in.SetText("zzz")
	if el.suggestOpen() {
		t.Error("popup should close when nothing matches")
	}
}

// ↓ opens a closed list, ↓/↑ move the highlight with wrapping, Enter accepts,
// Esc closes.
func TestDatalistKeyboardNavigation(t *testing.T) {
	el, in, _ := datalistField(t)
	in.SetFocused(true)

	key := func(k qui.Key) bool { return el.handleSuggestKey(qui.NewKeyEvent(qui.EventKeyDown, k, 0)) }

	if !key(qui.KeyDown) || !el.suggestOpen() {
		t.Fatal("ArrowDown should open the suggestion list")
	}
	if el.suggestIdx != -1 {
		t.Fatalf("opening should highlight nothing, got %d", el.suggestIdx)
	}
	key(qui.KeyDown)
	if el.suggestIdx != 0 {
		t.Fatalf("first ArrowDown highlight = %d, want 0", el.suggestIdx)
	}
	key(qui.KeyUp) // wraps to the end
	if el.suggestIdx != len(el.suggestMatches)-1 {
		t.Fatalf("ArrowUp from the top should wrap, got %d", el.suggestIdx)
	}
	key(qui.KeyDown) // wraps back to 0
	if el.suggestIdx != 0 {
		t.Fatalf("ArrowDown from the end should wrap, got %d", el.suggestIdx)
	}

	if !key(qui.KeyEnter) {
		t.Fatal("Enter should be consumed when a suggestion is highlighted")
	}
	if in.Text != "Paris" {
		t.Errorf("accepted value = %q, want Paris", in.Text)
	}
	if el.suggestOpen() {
		t.Error("accepting should close the list")
	}

	// Esc closes without changing the value.
	key(qui.KeyDown)
	before := in.Text
	if !key(qui.KeyEscape) {
		t.Error("Esc should be consumed while the list is open")
	}
	if el.suggestOpen() {
		t.Error("Esc should close the list")
	}
	if in.Text != before {
		t.Errorf("Esc changed the value to %q", in.Text)
	}
}

// With nothing highlighted, Enter belongs to the field (form submit) — the
// list must not swallow it.
func TestDatalistEnterWithoutHighlightFallsThrough(t *testing.T) {
	el, in, _ := datalistField(t)
	in.SetFocused(true)
	in.SetText("p") // opens, nothing highlighted
	if !el.suggestOpen() {
		t.Fatal("popup should be open")
	}
	if el.handleSuggestKey(qui.NewKeyEvent(qui.EventKeyDown, qui.KeyEnter, 0)) {
		t.Error("Enter with no highlight must fall through to the field")
	}
	if el.suggestOpen() {
		t.Error("Enter should still close the list")
	}
}

// Accepting a suggestion reports through onInput once, and does not re-open
// the list on the value it just committed.
func TestDatalistApplyFiresOnInputOnce(t *testing.T) {
	el, in, _ := datalistField(t)
	var seen []string
	el.SetOnInput(func(s string) { seen = append(seen, s) })
	in.SetFocused(true)
	in.SetText("berl")
	seen = nil // ignore the typing itself
	el.handleSuggestKey(qui.NewKeyEvent(qui.EventKeyDown, qui.KeyDown, 0))
	el.handleSuggestKey(qui.NewKeyEvent(qui.EventKeyDown, qui.KeyEnter, 0))

	if in.Text != "Berlin" {
		t.Fatalf("value = %q, want Berlin", in.Text)
	}
	if len(seen) != 1 || seen[0] != "Berlin" {
		t.Errorf("onInput calls = %v, want one [Berlin]", seen)
	}
	if el.suggestOpen() {
		t.Error("the list reopened on the value it just committed")
	}
}

// The suggestion list is as wide as the field (browser behavior), and every
// row stretches across it — not just as wide as the widest suggestion.
func TestDatalistPopupMatchesFieldWidth(t *testing.T) {
	el, in, _ := datalistField(t)
	field := in.Bounds()
	if field.W <= 0 {
		t.Fatalf("field not laid out: %+v", field)
	}
	in.SetFocused(true)
	// ↓ on an empty field opens the list with every suggestion in it.
	el.handleSuggestKey(qui.NewKeyEvent(qui.EventKeyDown, qui.KeyDown, 0))
	if !el.suggestOpen() {
		t.Fatal("popup should be open")
	}
	list := el.suggestPopup.Content
	if got := list.Bounds().W; got != field.W {
		t.Errorf("list width = %v, want the field width %v", got, field.W)
	}
	for _, row := range list.(*widgets.Box).ChildList() {
		if got := row.Bounds().W; got <= field.W*0.5 {
			t.Errorf("row width = %v, want it stretched across the %v-wide list", got, field.W)
		}
	}
}

// A field with no `list` attribute gets no autocompletion at all.
func TestNoDatalistNoSuggestions(t *testing.T) {
	win := qui.NewTestWindow(qui.Size{W: 400, H: 300})
	res := RenderDoc(`<body><input id="plain"></body>`, ``, Options{Window: win})
	el := res.ByID["plain"].(*El)
	in := backingInput(t, el)
	in.SetText("anything")
	if len(el.suggestions) != 0 || el.suggestOpen() {
		t.Error("a field without `list` must not autocomplete")
	}
}
