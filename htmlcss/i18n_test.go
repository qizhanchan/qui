package htmlcss_test

import (
	"testing"
	"testing/fstest"

	"github.com/qizhanchan/qui"
	"github.com/qizhanchan/qui/htmlcss"
	"github.com/qizhanchan/qui/i18n"
)

// setupCatalog installs a two-language test catalog and restores the
// process locale afterwards.
func setupCatalog(t *testing.T) {
	t.Helper()
	fs := fstest.MapFS{
		"loc/en.json": &fstest.MapFile{Data: []byte(`{
			"app.title": "Inbox",
			"search.hint": "Search mail",
			"cart.items": {"one": "{n} item", "other": "{n} items"},
			"greet": "Hello, {name}"
		}`)},
		"loc/de.json": &fstest.MapFile{Data: []byte(`{
			"app.title": "Posteingang",
			"search.hint": "E-Mail durchsuchen",
			"cart.items": {"one": "{n} Artikel", "other": "{n} Artikel"},
			"greet": "Hallo, {name}"
		}`)},
		"loc/zh-Hans.json": &fstest.MapFile{Data: []byte(`{
			"app.title": "收件箱",
			"search.hint": "搜索邮件"
		}`)},
	}
	if err := i18n.Install(); err != nil {
		t.Fatalf("Install: %v", err)
	}
	if err := i18n.Load(fs, "loc"); err != nil {
		t.Fatalf("Load: %v", err)
	}
	prev := qui.DefaultLocale()
	t.Cleanup(func() { qui.SetDefaultLocale(prev) })
}

// findText walks a rendered tree and returns the text content of the
// element with the given id.
func elemText(t *testing.T, res htmlcss.RenderResult, id string) string {
	t.Helper()
	return elemByID(t, res, id).TextContent()
}

func elemByID(t *testing.T, res htmlcss.RenderResult, id string) *htmlcss.El {
	t.Helper()
	w, ok := res.ByID[id]
	if !ok {
		t.Fatalf("no element with id %q", id)
	}
	el, ok := w.(*htmlcss.El)
	if !ok {
		t.Fatalf("#%s is %T, not *htmlcss.El", id, w)
	}
	return el
}

func TestDataI18nResolvesText(t *testing.T) {
	setupCatalog(t)
	html := `<div>
		<h1 id="title" data-i18n="app.title">Inbox</h1>
		<span id="greeting" data-i18n="greet" data-i18n-arg-name="Ada">Hello, X</span>
	</div>`

	qui.SetDefaultLocale("en")
	res := htmlcss.RenderDoc(html, "", htmlcss.Options{})
	if got := elemText(t, res, "title"); got != "Inbox" {
		t.Errorf("en title = %q, want %q", got, "Inbox")
	}
	if got := elemText(t, res, "greeting"); got != "Hello, Ada" {
		t.Errorf("en greeting = %q, want %q", got, "Hello, Ada")
	}

	qui.SetDefaultLocale("de")
	res = htmlcss.RenderDoc(html, "", htmlcss.Options{})
	if got := elemText(t, res, "title"); got != "Posteingang" {
		t.Errorf("de title = %q, want %q", got, "Posteingang")
	}
	if got := elemText(t, res, "greeting"); got != "Hallo, Ada" {
		t.Errorf("de greeting = %q, want %q", got, "Hallo, Ada")
	}
}

// The markup literal is the fallback: an element whose key is missing
// keeps the text a developer wrote, not the raw key.
func TestDataI18nFallsBackToMarkup(t *testing.T) {
	setupCatalog(t)
	qui.SetDefaultLocale("en")
	res := htmlcss.RenderDoc(`<p id="x" data-i18n="no.such.key">Literal text</p>`, "", htmlcss.Options{})
	if got := elemText(t, res, "x"); got != "Literal text" {
		t.Errorf("missing key should keep the markup literal, got %q", got)
	}
}

// A `lang` attribute overrides the active locale for its subtree.
func TestLangAttributeOverridesLocale(t *testing.T) {
	setupCatalog(t)
	qui.SetDefaultLocale("en")
	html := `<div>
		<h1 id="a" data-i18n="app.title">Inbox</h1>
		<section lang="de"><h1 id="b" data-i18n="app.title">Inbox</h1></section>
		<section lang="zh-Hans"><span><h1 id="c" data-i18n="app.title">Inbox</h1></span></section>
	</div>`
	res := htmlcss.RenderDoc(html, "", htmlcss.Options{})
	if got := elemText(t, res, "a"); got != "Inbox" {
		t.Errorf("a = %q, want Inbox", got)
	}
	if got := elemText(t, res, "b"); got != "Posteingang" {
		t.Errorf("lang=de subtree = %q, want Posteingang", got)
	}
	// Inherited through an intermediate element with no lang of its own.
	if got := elemText(t, res, "c"); got != "收件箱" {
		t.Errorf("nested lang=zh-Hans = %q, want 收件箱", got)
	}
}

func TestDataI18nPlural(t *testing.T) {
	setupCatalog(t)
	qui.SetDefaultLocale("en")
	for _, c := range []struct{ count, want string }{
		{"1", "1 item"},
		{"5", "5 items"},
	} {
		html := `<span id="c" data-i18n="cart.items" data-i18n-count="` + c.count + `">x</span>`
		res := htmlcss.RenderDoc(html, "", htmlcss.Options{})
		if got := elemText(t, res, "c"); got != c.want {
			t.Errorf("count=%s: got %q, want %q", c.count, got, c.want)
		}
	}
}

func TestDataI18nPlaceholder(t *testing.T) {
	setupCatalog(t)
	qui.SetDefaultLocale("de")
	res := htmlcss.RenderDoc(
		`<input id="q" data-i18n-placeholder="search.hint" placeholder="Search">`, "", htmlcss.Options{})
	got, _ := elemByID(t, res, "q").Attr("placeholder")
	if got != "E-Mail durchsuchen" {
		t.Errorf("placeholder = %q, want %q", got, "E-Mail durchsuchen")
	}
}

// A live tree must re-resolve its messages when the language changes,
// without being rebuilt — the whole point of resolving during restyle.
func TestLocaleSwitchRestylesLiveTree(t *testing.T) {
	setupCatalog(t)
	qui.SetDefaultLocale("en")

	eng := htmlcss.NewStyleEngine("")
	t.Cleanup(eng.Close)
	root := eng.NewEl("div")
	title := eng.NewEl("h1")
	title.SetAttr("data-i18n", "app.title")
	title.SetTextContent("Inbox")
	root.SetElementChildren([]qui.Widget{title})
	eng.SetRoot(root)
	eng.Restyle()

	if got := title.TextContent(); got != "Inbox" {
		t.Fatalf("before switch = %q, want Inbox", got)
	}

	qui.SetDefaultLocale("de")
	// SubscribeLocale fires Restyle synchronously; no window, so the
	// engine's coalescing path runs inline.
	if got := title.TextContent(); got != "Posteingang" {
		t.Errorf("after switch to de = %q, want Posteingang", got)
	}

	qui.SetDefaultLocale("zh-Hans")
	if got := title.TextContent(); got != "收件箱" {
		t.Errorf("after switch to zh-Hans = %q, want 收件箱", got)
	}
}

// The message key must reach the accessibility layer so [key=] selectors
// work — the locale-independent handle agent scripts rely on.
func TestMessageKeyReachesAccessibility(t *testing.T) {
	setupCatalog(t)
	qui.SetDefaultLocale("de")
	res := htmlcss.RenderDoc(`<button id="b" data-i18n="app.title">Inbox</button>`, "", htmlcss.Options{})
	if got := qui.WidgetNameKey(elemByID(t, res, "b")); got != "app.title" {
		t.Errorf("WidgetNameKey = %q, want app.title", got)
	}
}
