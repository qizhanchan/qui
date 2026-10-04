package i18n_test

import (
	"testing"
	"testing/fstest"
	"time"

	"github.com/qizhanchan/qui"
	"github.com/qizhanchan/qui/i18n"
)

// install sets up the builtin catalogs plus the test-only app catalog
// once, and restores the process locale after each test.
func install(t *testing.T) {
	t.Helper()
	if err := i18n.Install(); err != nil {
		t.Fatalf("Install: %v", err)
	}
	prev := qui.DefaultLocale()
	t.Cleanup(func() { qui.SetDefaultLocale(prev) })
}

func TestLocaleParsing(t *testing.T) {
	cases := []struct {
		in                   qui.Locale
		lang, script, region string
		dir                  qui.TextDirection
		fallbacks            []qui.Locale
	}{
		{"zh-Hans-CN", "zh", "Hans", "CN", qui.TextDirectionLTR, []qui.Locale{"zh-Hans-CN", "zh-Hans", "zh"}},
		{"ZH_hant_tw", "zh", "Hant", "TW", qui.TextDirectionLTR, []qui.Locale{"zh-Hant-TW", "zh-Hant", "zh"}},
		{"en", "en", "", "", qui.TextDirectionLTR, []qui.Locale{"en"}},
		{"ar-EG", "ar", "", "EG", qui.TextDirectionRTL, []qui.Locale{"ar-EG", "ar"}},
		{"he", "he", "", "", qui.TextDirectionRTL, []qui.Locale{"he"}},
		// Script subtag wins over the language table: Azerbaijani in
		// Arabic script is RTL, in Latin script it is not.
		{"az-Arab", "az", "Arab", "", qui.TextDirectionRTL, []qui.Locale{"az-Arab", "az"}},
		{"az-Latn", "az", "Latn", "", qui.TextDirectionLTR, []qui.Locale{"az-Latn", "az"}},
		{"", "", "", "", qui.TextDirectionLTR, nil},
	}
	for _, c := range cases {
		if got := c.in.Language(); got != c.lang {
			t.Errorf("%q.Language() = %q, want %q", c.in, got, c.lang)
		}
		if got := c.in.Script(); got != c.script {
			t.Errorf("%q.Script() = %q, want %q", c.in, got, c.script)
		}
		if got := c.in.Region(); got != c.region {
			t.Errorf("%q.Region() = %q, want %q", c.in, got, c.region)
		}
		if got := c.in.Direction(); got != c.dir {
			t.Errorf("%q.Direction() = %v, want %v", c.in, got, c.dir)
		}
		got := c.in.Fallbacks()
		if len(got) != len(c.fallbacks) {
			t.Errorf("%q.Fallbacks() = %v, want %v", c.in, got, c.fallbacks)
			continue
		}
		for i := range got {
			if got[i] != c.fallbacks[i] {
				t.Errorf("%q.Fallbacks() = %v, want %v", c.in, got, c.fallbacks)
				break
			}
		}
	}
}

func TestBuiltinTranslations(t *testing.T) {
	install(t)
	cases := []struct{ loc, key, want string }{
		{"en", "qui.ok", "OK"},
		{"zh-Hans", "qui.ok", "确定"},
		{"zh-Hant", "qui.save", "儲存"},
		{"ja", "qui.cancel", "キャンセル"},
		{"de", "qui.edit.undo", "Rückgängig machen"},
		{"ar", "qui.close", "إغلاق"},
		{"ru", "qui.delete", "Удалить"},
	}
	for _, c := range cases {
		qui.SetDefaultLocale(qui.Locale(c.loc))
		if got := i18n.T(c.key); got != c.want {
			t.Errorf("locale %s: T(%q) = %q, want %q", c.loc, c.key, got, c.want)
		}
	}
}

// A region-specific locale with no catalog of its own must resolve
// through its fallback chain rather than dropping to English.
func TestFallbackChain(t *testing.T) {
	install(t)
	qui.SetDefaultLocale("zh-Hans-CN")
	if got := i18n.T("qui.ok"); got != "确定" {
		t.Errorf("zh-Hans-CN should fall back to zh-Hans: got %q", got)
	}
	qui.SetDefaultLocale("zh-Hant-TW")
	if got := i18n.T("qui.ok"); got != "確定" {
		t.Errorf("zh-Hant-TW should fall back to zh-Hant: got %q", got)
	}
	// zh-Hant-HK -> zh-Hant, NOT zh-Hans: a two-step chain must not
	// collapse to the bare language and pick the wrong script.
	qui.SetDefaultLocale("zh-Hant-HK")
	if got := i18n.T("qui.save"); got != "儲存" {
		t.Errorf("zh-Hant-HK should resolve traditional: got %q", got)
	}
}

// An unknown locale falls through to the default locale's catalog, and
// an unknown key returns the key itself.
func TestMissingKeyAndLocale(t *testing.T) {
	install(t)
	qui.SetDefaultLocale("en")
	if got := i18n.T("no.such.key"); got != "no.such.key" {
		t.Errorf("missing key should return itself, got %q", got)
	}
	qui.SetDefaultLocale("xx-YY")
	if got := i18n.T("qui.ok"); got == "" || got == "qui.ok" {
		t.Errorf("unknown locale should fall back to the default catalog, got %q", got)
	}
}

func TestPluralCategories(t *testing.T) {
	install(t)
	// English: one / other.
	qui.SetDefaultLocale("en")
	if got := i18n.TN("qui.relative.past.day", 1); got != "1 day ago" {
		t.Errorf("en n=1: got %q", got)
	}
	if got := i18n.TN("qui.relative.past.day", 3); got != "3 days ago" {
		t.Errorf("en n=3: got %q", got)
	}

	// Chinese has a single form; the count still interpolates.
	qui.SetDefaultLocale("zh-Hans")
	if got := i18n.TN("qui.relative.past.day", 1); got != "1 天前" {
		t.Errorf("zh n=1: got %q", got)
	}

	// Russian: 1 -> one, 3 -> few, 5 -> many, 21 -> one.
	qui.SetDefaultLocale("ru")
	for _, c := range []struct {
		n    int
		want string
	}{
		{1, "1 день назад"},
		{3, "3 дня назад"},
		{5, "5 дней назад"},
		{21, "21 день назад"},
		{25, "25 дней назад"},
	} {
		if got := i18n.TN("qui.relative.past.day", c.n); got != c.want {
			t.Errorf("ru n=%d: got %q, want %q", c.n, got, c.want)
		}
	}

	// Arabic exercises zero / one / two / few / many.
	qui.SetDefaultLocale("ar")
	for _, c := range []struct {
		n    int
		want string
	}{
		{0, "منذ 0 يوم"},
		{1, "منذ يوم واحد"},
		{2, "منذ يومين"},
		{3, "منذ 3 أيام"},
		{11, "منذ 11 يومًا"},
	} {
		if got := i18n.TN("qui.relative.past.day", c.n); got != c.want {
			t.Errorf("ar n=%d: got %q, want %q", c.n, got, c.want)
		}
	}
}

func TestInterpolation(t *testing.T) {
	install(t)
	i18n.Register("en", map[string]i18n.Message{
		"greet":   {Text: "Hello, {name}!"},
		"braces":  {Text: "{{literal}} and {name}"},
		"missing": {Text: "value is {absent}"},
		"unterm":  {Text: "open {brace"},
		"count":   {Text: "{n} of {total}"},
	})
	qui.SetDefaultLocale("en")
	// Registering after Install does not bump the locale generation, so
	// nudge it — mirrors what an app does when it hot-loads a catalog.
	qui.SetDefaultLocale("en")

	cases := []struct {
		key, want string
		args      []any
	}{
		{"greet", "Hello, Ada!", []any{"name", "Ada"}},
		{"greet", "Hello, Ada!", []any{map[string]any{"name": "Ada"}}},
		{"braces", "{literal} and Ada", []any{"name", "Ada"}},
		// A placeholder with no argument stays visible rather than
		// silently rendering as empty.
		{"missing", "value is {absent}", nil},
		{"unterm", "open {brace", nil},
		{"count", "3 of 10", []any{"n", 3, "total", 10}},
		// Integral floats must not print "3.0 of 10".
		{"count", "3 of 10", []any{"n", 3.0, "total", 10}},
	}
	for _, c := range cases {
		if got := i18n.T(c.key, c.args...); got != c.want {
			t.Errorf("T(%q, %v) = %q, want %q", c.key, c.args, got, c.want)
		}
	}
}

// An application catalog overrides a framework string for the same key.
func TestAppOverridesBuiltin(t *testing.T) {
	install(t)
	appFS := fstest.MapFS{
		"loc/en.json": &fstest.MapFile{Data: []byte(`{"qui.ok":"Got it","app.title":"Demo"}`)},
	}
	if err := i18n.Load(appFS, "loc"); err != nil {
		t.Fatalf("Load: %v", err)
	}
	qui.SetDefaultLocale("en")
	if got := i18n.T("qui.ok"); got != "Got it" {
		t.Errorf("app catalog should override the builtin: got %q", got)
	}
	if got := i18n.T("app.title"); got != "Demo" {
		t.Errorf("app key: got %q", got)
	}
}

func TestCatalogParseErrors(t *testing.T) {
	// A plural message without "other" is invalid: every CLDR language
	// requires it, so accepting one would produce empty labels at
	// runtime in whichever locale needs the missing form.
	if _, err := i18n.ParseCatalog([]byte(`{"k":{"one":"x"}}`)); err == nil {
		t.Error("plural message missing \"other\" should fail to parse")
	}
	if _, err := i18n.ParseCatalog([]byte(`{"k":{"lots":"x","other":"y"}}`)); err == nil {
		t.Error("unknown plural category should fail to parse")
	}
	if _, err := i18n.ParseCatalog([]byte(`{"k":{"one":"a","other":"b"}}`)); err != nil {
		t.Errorf("valid plural message rejected: %v", err)
	}
	// "@"-prefixed keys are metadata, not messages.
	cat, err := i18n.ParseCatalog([]byte(`{"@meta":{"locale":"en"},"k":"v"}`))
	if err != nil {
		t.Fatalf("metadata key rejected: %v", err)
	}
	if len(cat) != 1 {
		t.Errorf("metadata leaked into the catalog: %v", cat)
	}
}

func TestNumberAndCurrencyFormatting(t *testing.T) {
	install(t)
	cases := []struct{ loc, want string }{
		{"en", "1,234,567.5"},
		{"de", "1.234.567,5"},
	}
	for _, c := range cases {
		if got := i18n.In(qui.Locale(c.loc)).Number(1234567.5); got != c.want {
			t.Errorf("%s Number: got %q, want %q", c.loc, got, c.want)
		}
	}
	// Grouping and symbol placement both come from the locale.
	if got := i18n.In("en").Currency(1234.5, "USD"); got == "" {
		t.Error("en USD produced an empty string")
	}
	// An unknown code degrades instead of failing.
	if got := i18n.In("en").Currency(10, "NOTACODE"); got != "NOTACODE 10" {
		t.Errorf("unknown currency: got %q", got)
	}
}

func TestRelativeTime(t *testing.T) {
	install(t)
	now := time.Date(2026, 8, 15, 12, 0, 0, 0, time.UTC)
	cases := []struct {
		loc  string
		t    time.Time
		want string
	}{
		{"en", now.Add(-5 * time.Second), "just now"},
		{"en", now.Add(-3 * time.Minute), "3 minutes ago"},
		{"en", now.Add(-1 * time.Hour), "1 hour ago"},
		{"en", now.Add(3 * 24 * time.Hour), "in 3 days"},
		{"zh-Hans", now.Add(-3 * time.Minute), "3 分钟前"},
		{"ru", now.Add(-3 * 24 * time.Hour), "3 дня назад"},
	}
	for _, c := range cases {
		if got := i18n.In(qui.Locale(c.loc)).RelativeTime(c.t, now); got != c.want {
			t.Errorf("%s RelativeTime: got %q, want %q", c.loc, got, c.want)
		}
	}
}

func TestListFormatting(t *testing.T) {
	install(t)
	items := []string{"A", "B", "C"}
	cases := []struct {
		loc   string
		style i18n.ListStyle
		items []string
		want  string
	}{
		{"en", i18n.ListAnd, items, "A, B, and C"},
		{"en", i18n.ListAnd, []string{"A", "B"}, "A and B"},
		{"en", i18n.ListOr, items, "A, B, or C"},
		{"en", i18n.ListAnd, []string{"A"}, "A"},
		{"en", i18n.ListAnd, nil, ""},
		{"zh-Hans", i18n.ListAnd, items, "A、B和C"},
		{"de", i18n.ListAnd, items, "A, B und C"},
	}
	for _, c := range cases {
		if got := i18n.In(qui.Locale(c.loc)).List(c.items, c.style); got != c.want {
			t.Errorf("%s List(%v): got %q, want %q", c.loc, c.items, got, c.want)
		}
	}
}

// Collation must beat byte order for accented and CJK text.
func TestCollator(t *testing.T) {
	install(t)
	de := i18n.In("de").Collator()
	// "Ä" sorts with "A" in German, but its UTF-8 bytes put it after "Z".
	if de.CompareString("Äpfel", "Zebra") >= 0 {
		t.Error("de collator should sort Äpfel before Zebra")
	}
	if "Äpfel" < "Zebra" {
		t.Error("precondition failed: byte order was expected to disagree")
	}
}

// The locale generation must move when the language or translator does;
// text-layout caches key off it.
func TestLocaleGeneration(t *testing.T) {
	install(t)
	qui.SetDefaultLocale("en")
	before := qui.LocaleGeneration()
	qui.SetDefaultLocale("de")
	if qui.LocaleGeneration() == before {
		t.Error("SetDefaultLocale must bump LocaleGeneration")
	}
	before = qui.LocaleGeneration()
	qui.SetTranslator(i18n.Default())
	if qui.LocaleGeneration() == before {
		t.Error("SetTranslator must bump LocaleGeneration")
	}
}
