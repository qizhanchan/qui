package widgets_test

import (
	"testing"

	"github.com/qizhanchan/qui"
	"github.com/qizhanchan/qui/widgets"
)

// mapTranslator is an exact locale -> key -> text lookup. Root walks the
// fallback chain, so this deliberately does none.
type mapTranslator map[qui.Locale]map[string]string

func (m mapTranslator) Translate(loc qui.Locale, key string, args map[string]any) (string, bool) {
	v, ok := m[loc][key]
	if !ok {
		return "", false
	}
	for name, val := range args {
		v = replaceAll(v, "{"+name+"}", toString(val))
	}
	return v, true
}

func (m mapTranslator) TranslatePlural(loc qui.Locale, key string, n float64, args map[string]any) (string, bool) {
	return m.Translate(loc, key, args)
}

func replaceAll(s, old, new string) string {
	for {
		i := indexOf(s, old)
		if i < 0 {
			return s
		}
		s = s[:i] + new + s[i+len(old):]
	}
}

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}

func toString(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	return "?"
}

func install(t *testing.T, m mapTranslator) {
	t.Helper()
	prevT := qui.CurrentTranslator()
	prevLoc := qui.DefaultLocale()
	prevFb := qui.FallbackLocale()
	qui.SetTranslator(m)
	qui.SetFallbackLocale("en")
	t.Cleanup(func() {
		qui.SetTranslator(prevT)
		qui.SetDefaultLocale(prevLoc)
		qui.SetFallbackLocale(prevFb)
	})
}

var catalog = mapTranslator{
	"en": {
		"qui.save":     "Save",
		"search.hint":  "Search",
		"opt.in":       "Enable sync",
		"greet":        "Hello, {name}",
		"select.empty": "Choose…",
	},
	"de": {
		"qui.save":     "Speichern",
		"search.hint":  "Suchen",
		"opt.in":       "Synchronisierung aktivieren",
		"greet":        "Hallo, {name}",
		"select.empty": "Auswählen…",
	},
}

// The core retained-mode requirement: a widget built once must show the
// new language after a switch, with no rebuild.
func TestTextKeyFollowsLocaleSwitch(t *testing.T) {
	install(t, catalog)
	qui.SetDefaultLocale("en")

	btn := widgets.NewButton("Save", nil)
	btn.SetTextKey("qui.save")
	if got := btn.DisplayText(); got != "Save" {
		t.Fatalf("en: %q", got)
	}

	qui.SetDefaultLocale("de")
	if got := btn.DisplayText(); got != "Speichern" {
		t.Errorf("after switch to de: %q, want Speichern", got)
	}
	// The literal field is untouched — it is the fallback, not state.
	if btn.Text != "Save" {
		t.Errorf("Text field was mutated: %q", btn.Text)
	}
}

// A key the catalog lacks must fall back to the widget's literal, NOT to
// the raw key: an app shipping no catalog should look unchanged.
func TestTextKeyFallsBackToLiteral(t *testing.T) {
	install(t, catalog)
	qui.SetDefaultLocale("en")

	btn := widgets.NewButton("Publish", nil)
	btn.SetTextKey("no.such.key")
	if got := btn.DisplayText(); got != "Publish" {
		t.Errorf("got %q, want the literal Publish", got)
	}
}

func TestTextKeyPlaceholders(t *testing.T) {
	install(t, catalog)
	qui.SetDefaultLocale("de")

	l := widgets.NewLabel("Hello")
	l.SetTextKey("greet", "name", "Ada")
	if got := l.Text(); got != "Hallo, Ada" {
		t.Errorf("got %q, want 'Hallo, Ada'", got)
	}
}

// Every keyed widget resolves through the same path; this pins the whole
// family so a new widget cannot quietly skip it.
func TestKeyedWidgetFamily(t *testing.T) {
	install(t, catalog)
	qui.SetDefaultLocale("de")

	input := widgets.NewInput("Search")
	input.SetPlaceholderKey("search.hint")
	if got := input.DisplayPlaceholder(); got != "Suchen" {
		t.Errorf("Input placeholder = %q", got)
	}

	area := widgets.NewTextArea("Search")
	area.SetPlaceholderKey("search.hint")
	if got := area.DisplayPlaceholder(); got != "Suchen" {
		t.Errorf("TextArea placeholder = %q", got)
	}

	cb := widgets.NewCheckBox("Enable sync", nil)
	cb.SetLabelKey("opt.in")
	if got := cb.DisplayLabel(); got != "Synchronisierung aktivieren" {
		t.Errorf("CheckBox label = %q", got)
	}

	sw := widgets.NewSwitch("Enable sync", nil)
	sw.SetLabelKey("opt.in")
	if got := sw.DisplayLabel(); got != "Synchronisierung aktivieren" {
		t.Errorf("Switch label = %q", got)
	}

	sel := widgets.NewSelect(nil, []string{"a", "b"}, nil)
	sel.Placeholder = "Choose…"
	sel.SetPlaceholderKey("select.empty")
	if got := sel.DisplayPlaceholder(); got != "Auswählen…" {
		t.Errorf("Select placeholder = %q", got)
	}
}

// Measure must use the RESOLVED caption. A widget that measured its
// literal and drew its translation is exactly how localized buttons end
// up clipping their own text.
func TestMeasureUsesResolvedText(t *testing.T) {
	install(t, catalog)

	qui.SetDefaultLocale("en")
	short := widgets.NewButton("Save", nil)
	short.SetTextKey("qui.save")
	enSize := short.Measure(qui.Size{W: 1000, H: 100})

	qui.SetDefaultLocale("de")
	deSize := short.Measure(qui.Size{W: 1000, H: 100})

	// "Speichern" is materially longer than "Save".
	if deSize.W <= enSize.W {
		t.Errorf("German measured %v, English %v — Measure is not seeing the translation",
			deSize.W, enSize.W)
	}
}

// The accessibility layer must publish both the translated name and the
// stable key, so [name=] and [key=] both work — the latter across
// languages.
func TestAccessibilityExposesNameAndKey(t *testing.T) {
	install(t, catalog)
	qui.SetDefaultLocale("de")

	btn := widgets.NewButton("Save", nil)
	btn.SetTextKey("qui.save")
	if got := qui.WidgetName(btn); got != "Speichern" {
		t.Errorf("AccessibleName = %q, want Speichern", got)
	}
	if got := qui.WidgetNameKey(btn); got != "qui.save" {
		t.Errorf("AccessibleNameKey = %q, want qui.save", got)
	}

	// A widget with no key reports an empty one rather than its text.
	plain := widgets.NewButton("Literal", nil)
	if got := qui.WidgetNameKey(plain); got != "" {
		t.Errorf("unkeyed button reported key %q", got)
	}
	if got := qui.WidgetName(plain); got != "Literal" {
		t.Errorf("unkeyed button name = %q", got)
	}
}

// Clearing the key returns the widget to its literal.
func TestClearingTextKey(t *testing.T) {
	install(t, catalog)
	qui.SetDefaultLocale("de")

	btn := widgets.NewButton("Save", nil)
	btn.SetTextKey("qui.save")
	if btn.DisplayText() != "Speichern" {
		t.Fatal("precondition")
	}
	btn.SetTextKey("")
	if got := btn.DisplayText(); got != "Save" {
		t.Errorf("after clearing the key: %q, want Save", got)
	}
	if got := btn.TextKey(); got != "" {
		t.Errorf("TextKey = %q after clearing", got)
	}
}
