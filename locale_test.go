package qui

import "testing"

// stubTranslator is a minimal Translator: an exact locale -> key -> text
// map, with no fallback logic of its own. That is deliberate — walking
// the fallback chain is root's job (lookupChain), and these tests assert
// root does it rather than assuming the bundle will.
type stubTranslator struct {
	msgs map[Locale]map[string]string
	// calls records every (locale, key) asked for, in order, so a test
	// can assert the SHAPE of the lookup and not just its result.
	calls []Locale
}

func (s *stubTranslator) Translate(loc Locale, key string, _ map[string]any) (string, bool) {
	s.calls = append(s.calls, loc)
	v, ok := s.msgs[loc][key]
	return v, ok
}

func (s *stubTranslator) TranslatePlural(loc Locale, key string, _ float64, _ map[string]any) (string, bool) {
	return s.Translate(loc, key, nil)
}

func withTranslator(t *testing.T, tr Translator) {
	t.Helper()
	prevT := CurrentTranslator()
	prevLoc := DefaultLocale()
	prevFallback := FallbackLocale()
	SetTranslator(tr)
	t.Cleanup(func() {
		SetTranslator(prevT)
		SetDefaultLocale(prevLoc)
		SetFallbackLocale(prevFallback)
	})
}

// The three-tier chain — requested locale, active default, authored
// fallback — must be tried in that order and deduped.
func TestLookupChainOrder(t *testing.T) {
	tr := &stubTranslator{msgs: map[Locale]map[string]string{
		"en": {"k": "English"},
	}}
	withTranslator(t, tr)
	SetDefaultLocale("de")
	SetFallbackLocale("en")

	if got := Translate("zh-Hant-TW", "k", nil); got != "English" {
		t.Errorf("got %q, want English via the fallback locale", got)
	}
	want := []Locale{"zh-Hant-TW", "zh-Hant", "zh", "de", "en"}
	if len(tr.calls) != len(want) {
		t.Fatalf("lookup order = %v, want %v", tr.calls, want)
	}
	for i := range want {
		if tr.calls[i] != want[i] {
			t.Fatalf("lookup order = %v, want %v", tr.calls, want)
		}
	}
}

// A locale that appears in more than one tier must be tried once.
func TestLookupChainDedupes(t *testing.T) {
	tr := &stubTranslator{msgs: map[Locale]map[string]string{}}
	withTranslator(t, tr)
	SetDefaultLocale("en")
	SetFallbackLocale("en")

	Translate("en", "missing", nil)
	if len(tr.calls) != 1 {
		t.Errorf("expected 1 lookup for a locale in all three tiers, got %v", tr.calls)
	}
}

func TestTranslateFallsBackToKey(t *testing.T) {
	withTranslator(t, &stubTranslator{msgs: map[Locale]map[string]string{}})
	SetDefaultLocale("en")
	if got := Translate("en", "some.key", nil); got != "some.key" {
		t.Errorf("got %q, want the key itself", got)
	}
}

// TranslateOr must prefer the caller's literal over the key — that is
// what keeps non-i18n apps rendering English instead of "qui.submit".
func TestTranslateOrPrefersLiteral(t *testing.T) {
	withTranslator(t, &stubTranslator{msgs: map[Locale]map[string]string{
		"de": {"qui.ok": "OK-de"},
	}})
	SetDefaultLocale("de")
	SetFallbackLocale("de")

	if got := TranslateOr("de", "qui.ok", "OK", nil); got != "OK-de" {
		t.Errorf("present key: got %q, want the translation", got)
	}
	if got := TranslateOr("de", "no.such", "Fallback", nil); got != "Fallback" {
		t.Errorf("missing key: got %q, want the literal", got)
	}
}

// With NO translator installed at all, TranslateOr must still return the
// literal. This is the path every app that ignores i18n takes.
func TestTranslateOrWithoutTranslator(t *testing.T) {
	prev := CurrentTranslator()
	SetTranslator(nil)
	t.Cleanup(func() { SetTranslator(prev) })

	if got := TOr("qui.submit", "Submit"); got != "Submit" {
		t.Errorf("got %q, want Submit", got)
	}
	if got := T("qui.submit"); got != "qui.submit" {
		t.Errorf("bare T should degrade to the key, got %q", got)
	}
}

func TestLocaleGenerationMoves(t *testing.T) {
	prev := DefaultLocale()
	t.Cleanup(func() { SetDefaultLocale(prev) })

	SetDefaultLocale("en")
	g0 := LocaleGeneration()
	SetDefaultLocale("ja")
	if LocaleGeneration() == g0 {
		t.Error("SetDefaultLocale did not bump the generation")
	}
	g1 := LocaleGeneration()
	SetFallbackLocale("de")
	if LocaleGeneration() == g1 {
		t.Error("SetFallbackLocale did not bump the generation")
	}
}

// Subscribers exist so windows repaint on a language switch; a dropped
// subscription there means a stale UI.
func TestSubscribeLocale(t *testing.T) {
	prev := DefaultLocale()
	t.Cleanup(func() { SetDefaultLocale(prev) })

	fired := 0
	cancel := SubscribeLocale(func() { fired++ })
	SetDefaultLocale("fr")
	if fired != 1 {
		t.Fatalf("subscriber fired %d times, want 1", fired)
	}
	cancel()
	SetDefaultLocale("it")
	if fired != 1 {
		t.Errorf("subscriber fired after cancel (%d)", fired)
	}
}

// Language switching must invalidate the text-layout memo: a cached
// measurement taken under the previous locale can be the wrong width.
func TestLocaleChangeInvalidatesTextLayoutCache(t *testing.T) {
	prev := DefaultLocale()
	t.Cleanup(func() { SetDefaultLocale(prev) })

	SetDefaultLocale("en")
	BuildTextLayout("hello", Font{Size: 14}, TextLayoutOptions{})
	textLayoutCacheMu.Lock()
	before := len(textLayoutCache)
	textLayoutCacheMu.Unlock()
	if before == 0 {
		t.Fatal("precondition: expected the layout to be memoized")
	}

	SetDefaultLocale("ja")
	textLayoutCacheMu.Lock()
	after := len(textLayoutCache)
	textLayoutCacheMu.Unlock()
	if after != 0 {
		t.Errorf("text layout cache survived a locale change (%d entries)", after)
	}
}

func TestShapingLanguageFollowsLocale(t *testing.T) {
	prev := DefaultLocale()
	t.Cleanup(func() { SetDefaultLocale(prev) })

	for _, c := range []struct{ loc, want string }{
		{"zh-Hans", "zh-hans"},
		{"ja", "ja"},
		{"ar-EG", "ar-eg"},
	} {
		SetDefaultLocale(Locale(c.loc))
		if got := string(shapingLanguage()); got != c.want {
			t.Errorf("locale %s -> shaping language %q, want %q", c.loc, got, c.want)
		}
	}
}

// keyedWidget is a widget whose accessible name comes from a catalog.
type keyedWidget struct {
	BaseWidget
	name, key string
}

func (k *keyedWidget) Role() string              { return RoleButton }
func (k *keyedWidget) AccessibleName() string    { return k.name }
func (k *keyedWidget) AccessibleNameKey() string { return k.key }

func TestWidgetNameKey(t *testing.T) {
	k := &keyedWidget{name: "Speichern", key: "qui.save"}
	k.BaseWidget = NewBaseWidget()
	if got := WidgetNameKey(k); got != "qui.save" {
		t.Errorf("WidgetNameKey = %q, want qui.save", got)
	}
	// A widget that does not implement NameKeyed reports "".
	plain := &keyedWidget{name: "x"}
	plain.BaseWidget = NewBaseWidget()
	plain.key = ""
	if got := WidgetNameKey(plain); got != "" {
		t.Errorf("unkeyed widget reported %q", got)
	}
	if got := WidgetNameKey(nil); got != "" {
		t.Errorf("nil widget reported %q", got)
	}
}

// The whole point of NameKey: a selector written once keeps matching
// after the UI is translated, where [name=] does not.
func TestKeySelectorSurvivesTranslation(t *testing.T) {
	nodes := []*AXNode{
		{Path: "a", Role: "button", Name: "Save", NameKey: "qui.save"},
		{Path: "b", Role: "button", Name: "Cancel", NameKey: "qui.cancel"},
	}
	tree := &AccessibilityTree{Root: &AXNode{Path: "root", Role: "box", Children: nodes}}

	byKey, err := parseSelector("[key=qui.save]")
	if err != nil {
		t.Fatalf("parse [key=]: %v", err)
	}
	first := func(sel *compiledSelector) *AXNode {
		if m := sel.matchNodes(tree); len(m) > 0 {
			return m[0]
		}
		return nil
	}
	if got := first(byKey); got == nil || got.Path != "a" {
		t.Fatalf("[key=qui.save] did not match the English tree: %v", got)
	}

	// Same tree rendered in German: names change, keys do not.
	nodes[0].Name = "Speichern"
	nodes[1].Name = "Abbrechen"
	if got := first(byKey); got == nil || got.Path != "a" {
		t.Error("[key=qui.save] stopped matching after translation")
	}
	byName, err := parseSelector(`[name="Save"]`)
	if err != nil {
		t.Fatal(err)
	}
	if got := first(byName); got != nil {
		t.Error("precondition failed: [name=\"Save\"] was expected to STOP matching")
	}

	// Prefix / substring operators work on keys too.
	byPrefix, err := parseSelector("[key^=qui.]")
	if err != nil {
		t.Fatal(err)
	}
	if all := byPrefix.matchNodes(tree); len(all) != 2 {
		t.Errorf("[key^=qui.] matched %d nodes, want 2", len(all))
	}
}

// NameKey participates in the tree hash so a language switch registers
// as a change for WaitTreeStable.
func TestAXHashCoversNameKey(t *testing.T) {
	mk := func(key string) *AccessibilityTree {
		return &AccessibilityTree{Root: &AXNode{Path: "r", Role: "button", Name: "Save", NameKey: key}}
	}
	if mk("qui.save").Hash() == mk("qui.store").Hash() {
		t.Error("hash ignores NameKey")
	}
}
