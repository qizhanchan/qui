package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeTree materializes a map of relative path -> content under a temp
// dir and returns it.
func writeTree(t *testing.T, files map[string]string) string {
	t.Helper()
	root := t.TempDir()
	for name, body := range files {
		full := filepath.Join(root, name)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

func TestScanFindsEveryCallShape(t *testing.T) {
	root := writeTree(t, map[string]string{
		"app.go": `package app

import (
	"github.com/qizhanchan/qui"
	"github.com/qizhanchan/qui/i18n"
	h "github.com/qizhanchan/qui/reactive/html"
)

func build() {
	_ = i18n.T("plain.key")
	_ = i18n.T("with.args", "name", "Ada", "count", 3)
	_ = i18n.TN("plural.key", 5)
	_ = qui.TOr("fallback.key", "Literal")
	_ = qui.Translate("de", "explicit.locale.key", nil)
	btn.SetTextKey("widget.key")
	inp.SetPlaceholderKey("placeholder.key")
	_ = h.Span().T("dsl.key").TN("dsl.plural", 2)
}

const inline = ` + "`" + `<b data-i18n="inline.markup.key">x</b>` + "`" + `
`,
		"page.html": `<h1 data-i18n="file.markup.key">Title</h1>
<span data-i18n="file.plural.key" data-i18n-count="3" data-i18n-arg-who="you">x</span>`,
		// Must be skipped.
		"vendor/dep.go":   `package dep; func f() { _ = i18n.T("vendored.key") }`,
		"testdata/fix.go": `package fix; func f() { _ = i18n.T("fixture.key") }`,
	})

	refs, err := scanTree(root)
	if err != nil {
		t.Fatalf("scanTree: %v", err)
	}

	want := []string{
		"plain.key", "with.args", "plural.key", "fallback.key",
		"explicit.locale.key", "widget.key", "placeholder.key",
		"dsl.key", "dsl.plural", "inline.markup.key",
		"file.markup.key", "file.plural.key",
	}
	for _, k := range want {
		if _, ok := refs[k]; !ok {
			t.Errorf("missing key %q (found: %v)", k, sortedRefKeys(refs))
		}
	}
	for _, k := range []string{"vendored.key", "fixture.key"} {
		if _, ok := refs[k]; ok {
			t.Errorf("%q should have been skipped (vendor/testdata)", k)
		}
	}

	// Plural detection drives whether extract seeds forms.
	for _, k := range []string{"plural.key", "dsl.plural", "file.plural.key"} {
		if !refs[k].plural {
			t.Errorf("%q should be marked plural", k)
		}
	}
	if refs["plain.key"].plural {
		t.Error("plain.key should not be marked plural")
	}

	// Placeholder names feed the lint cross-check.
	if args := refs["with.args"].args; !args["name"] || !args["count"] {
		t.Errorf("with.args placeholders = %v, want name+count", args)
	}
	if args := refs["file.plural.key"].args; !args["who"] {
		t.Errorf("markup arg attribute not captured: %v", args)
	}
	// TN's count argument must not be mistaken for a placeholder name.
	if args := refs["plural.key"].args; len(args) != 0 {
		t.Errorf("plural.key should have no named placeholders, got %v", args)
	}
}

// The locale argument of Translate must not be read as the key.
func TestScanRespectsLocaleFirstSignatures(t *testing.T) {
	root := writeTree(t, map[string]string{
		"a.go": `package a
func f() { _ = qui.Translate("zh-Hans", "real.key", nil) }`,
	})
	refs, err := scanTree(root)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := refs["real.key"]; !ok {
		t.Error("did not find the key after the locale argument")
	}
	if _, ok := refs["zh-Hans"]; ok {
		t.Error("read the locale argument as a message key")
	}
}

// extract must never overwrite an existing translation.
func TestMergePreservesTranslations(t *testing.T) {
	existing := map[string]catalogEntry{
		"@meta":   {raw: `{"locale":"en"}`},
		"kept":    {text: "Human-written text"},
		"orphan":  {text: "No longer referenced"},
		"pluralK": {forms: map[string]string{"one": "1 thing", "other": "{n} things"}},
	}
	refs := map[string]keyRef{
		"kept":     {key: "kept"},
		"pluralK":  {key: "pluralK", plural: true},
		"brandNew": {key: "brandNew"},
		"newCount": {key: "newCount", plural: true},
	}

	merged, added, removed := mergeCatalog(existing, refs, false)
	if merged["kept"].text != "Human-written text" {
		t.Errorf("clobbered an existing translation: %q", merged["kept"].text)
	}
	if merged["pluralK"].forms["other"] != "{n} things" {
		t.Error("clobbered existing plural forms")
	}
	if merged["@meta"].raw == "" {
		t.Error("dropped @meta")
	}
	if _, ok := merged["orphan"]; !ok {
		t.Error("dropped an unreferenced key without -prune")
	}
	if len(removed) != 1 || removed[0] != "orphan" {
		t.Errorf("removed = %v, want [orphan]", removed)
	}
	if len(added) != 2 || added[0] != "brandNew" || added[1] != "newCount" {
		t.Errorf("added = %v, want [brandNew newCount]", added)
	}
	// A newly-referenced plural key gets plural stubs, not a flat string.
	if !merged["newCount"].isPlural() {
		t.Error("newCount should have been seeded with plural forms")
	}
	if merged["brandNew"].isPlural() {
		t.Error("brandNew should be a flat string")
	}

	// With -prune the orphan goes.
	pruned, _, _ := mergeCatalog(existing, refs, true)
	if _, ok := pruned["orphan"]; ok {
		t.Error("-prune did not drop the unreferenced key")
	}
	if _, ok := pruned["@meta"]; !ok {
		t.Error("-prune dropped @meta")
	}
}

func TestLintFindings(t *testing.T) {
	refs := map[string]keyRef{
		"present":     {key: "present", sites: []string{"a.go:1"}},
		"undefined":   {key: "undefined", sites: []string{"a.go:2"}},
		"needsArgs":   {key: "needsArgs", sites: []string{"a.go:3"}, args: map[string]bool{"name": true}},
		"countedFlat": {key: "countedFlat", sites: []string{"a.go:4"}, plural: true},
		"pluralNoN":   {key: "pluralNoN", sites: []string{"a.go:5"}},
	}
	cats := map[string]map[string]catalogEntry{
		"en": {
			"present":     {text: "Present"},
			"needsArgs":   {text: "Hello"}, // drops {name}
			"countedFlat": {text: "{n} things"},
			"pluralNoN":   {forms: map[string]string{"one": "a", "other": "b"}},
			"unused":      {text: "Nobody"},
		},
		"de": {
			"present":   {text: "Vorhanden"},
			"needsArgs": {text: "Hallo, {vorname}"}, // renamed placeholder
			"pluralNoN": {forms: map[string]string{"one": "a"}},
		},
	}

	got := map[string]bool{}
	for _, f := range lintAll(refs, cats) {
		got[f.kind] = true
	}
	for _, kind := range []string{
		"missing-key",            // "undefined"
		"unused-key",             // "unused"
		"incomplete-locale",      // de lacks countedFlat/unused
		"dropped-placeholder",    // en/needsArgs ignores {name}
		"unsupplied-placeholder", // de/needsArgs wants {vorname}
		"plural-missing-other",   // de/pluralNoN
		"plural-without-count",   // pluralNoN called without a count
		"count-without-plural",   // countedFlat called with one
	} {
		if !got[kind] {
			t.Errorf("lint did not report %s (reported: %v)", kind, got)
		}
	}
}

// {n} is bound automatically by plural calls, so a message using it must
// not be flagged as unsupplied.
func TestLintExemptsAutomaticCountPlaceholder(t *testing.T) {
	refs := map[string]keyRef{"k": {key: "k", plural: true, sites: []string{"a.go:1"}}}
	cats := map[string]map[string]catalogEntry{
		"en": {"k": {forms: map[string]string{"one": "{n} item", "other": "{n} items"}}},
	}
	for _, f := range lintAll(refs, cats) {
		if f.kind == "unsupplied-placeholder" {
			t.Errorf("flagged the automatic count placeholder: %s", f.detail)
		}
	}
}

func TestPlaceholderParsingSkipsEscapes(t *testing.T) {
	got := placeholdersIn("{{literal}} and {real} plus {{also}} {second}")
	if got["literal"] || got["also"] {
		t.Errorf("escaped braces read as placeholders: %v", got)
	}
	if !got["real"] || !got["second"] {
		t.Errorf("missed a real placeholder: %v", got)
	}
}

func TestPseudoPreservesPlaceholders(t *testing.T) {
	out := pseudoString("Hello, {name}! You have {{brace}} items", 1.4)
	if !strings.Contains(out, "{name}") {
		t.Errorf("mangled a placeholder: %q", out)
	}
	// An escaped brace stays escaped, but its CONTENT is accented: {{x}}
	// renders to the user as the literal "{x}", so it is visible text
	// and pseudo-localizing it is the point.
	if !strings.Contains(out, "{{") || !strings.Contains(out, "}}") {
		t.Errorf("lost the brace escaping: %q", out)
	}
	if strings.Contains(out, "{{brace}}") {
		t.Errorf("escaped-brace content should be accented like any other visible text: %q", out)
	}
	if !strings.HasPrefix(out, "⟪") || !strings.HasSuffix(out, "⟫") {
		t.Errorf("missing bounding brackets: %q", out)
	}
	// Accented and longer than the source — the two things the fixture
	// exists to surface.
	if !strings.ContainsRune(out, 'é') {
		t.Errorf("no accented characters: %q", out)
	}
	if len([]rune(out)) <= len([]rune("Hello, {name}! You have {{brace}} items")) {
		t.Errorf("pseudo string did not expand: %q", out)
	}
}

func TestPseudoHandlesPluralEntries(t *testing.T) {
	in := catalogEntry{forms: map[string]string{"one": "{n} item", "other": "{n} items"}}
	out := pseudoEntry(in, 1.4)
	if !out.isPlural() || len(out.forms) != 2 {
		t.Fatalf("plural shape lost: %+v", out)
	}
	for cat, v := range out.forms {
		if !strings.Contains(v, "{n}") {
			t.Errorf("%s form lost its count placeholder: %q", cat, v)
		}
	}
	// @meta and other raw entries pass through untouched.
	raw := catalogEntry{raw: `{"locale":"en"}`}
	if pseudoEntry(raw, 1.4).raw != raw.raw {
		t.Error("pseudo mangled a raw entry")
	}
}

// Catalogs are reviewed as diffs, so the writer must be deterministic:
// @meta first, keys sorted, plural forms in CLDR order.
func TestCatalogWriteIsStable(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "out.json")
	cat := map[string]catalogEntry{
		"zebra": {text: "Z"},
		"alpha": {text: "A"},
		"@meta": {raw: `{"locale":"en"}`},
		"plural": {forms: map[string]string{
			"other": "many", "one": "single", "few": "some",
		}},
	}
	if err := writeCatalogFile(path, cat); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	got := string(data)

	if i, j := strings.Index(got, "@meta"), strings.Index(got, "alpha"); i > j {
		t.Error("@meta should come first")
	}
	if i, j := strings.Index(got, "alpha"), strings.Index(got, "zebra"); i > j {
		t.Error("keys should be sorted")
	}
	// CLDR order: one, few, other — not map or alphabetical order.
	one, few, other := strings.Index(got, `"one"`), strings.Index(got, `"few"`), strings.Index(got, `"other"`)
	if !(one < few && few < other) {
		t.Errorf("plural forms out of CLDR order: one=%d few=%d other=%d", one, few, other)
	}

	// Round-trip: reading it back must reproduce the same catalog.
	back, err := readCatalogFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(back) != len(cat) {
		t.Fatalf("round-trip lost entries: %d -> %d", len(cat), len(back))
	}
	if back["plural"].forms["few"] != "some" {
		t.Error("round-trip lost a plural form")
	}
	if back["alpha"].text != "A" {
		t.Error("round-trip lost a flat entry")
	}
}
