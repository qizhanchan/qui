// i18n example — one app, seven languages, switched at runtime.
//
// Run:            go run ./examples/i18n
// Drive/observe:  QUI_AGENT=1 go run ./examples/i18n
// Audit coverage: QUI_I18N_STRICT=1 go run ./examples/i18n
//
// What it shows:
//
//   - Runtime language switching with NO tree rebuild. qui.SetDefaultLocale
//     bumps the locale generation; the style engine restyles, every
//     data-i18n element re-resolves in place, and the reactive reconciler
//     never runs. Same cost model as a signal update.
//   - CLDR plurals that a naive `if n == 1` cannot express: the item count
//     picks between two forms in English and German, four in Russian, and
//     six in Arabic — try the ± buttons at 0, 1, 2, 3, 5, 11, 21.
//   - Locale-sensitive formatting: grouped numbers, currency symbol and
//     placement, relative time, and list joining all come from the locale,
//     not from fmt.Sprintf.
//   - RTL text: switching to Arabic renders right-to-left through the same
//     shaping pipeline as everything else. NOTE the layout does not mirror
//     yet — that is phase 2 of docs/i18n-design.md; text direction works,
//     box order does not.
//   - Locale-tagged shaping: the shaper is handed the locale's language
//     tag, which is the precondition for OpenType `locl` glyph
//     substitution (the mechanism that renders 骨/直/戸 differently in
//     zh-Hans, zh-Hant and ja). Whether you SEE a difference depends on
//     the font: the macOS system CJK face qui auto-probes carries no
//     `locl` feature at all, so the sample line below is pixel-identical
//     across the three. Register a font that does — a full Source Han
//     Sans / Noto Sans CJK build — and the same code renders the right
//     regional forms. See the note in docs/i18n-design.md.
//   - Locale-independent agent selectors: every control carries a message
//     key, so `[key=demo.items]` matches in all seven languages while
//     `[name="3 items in the basket"]` only matches in English.
package main

import (
	"embed"
	"log"
	"time"

	"github.com/qizhanchan/qui"
	"github.com/qizhanchan/qui/agent"
	"github.com/qizhanchan/qui/i18n"
	"github.com/qizhanchan/qui/reactive"
	h "github.com/qizhanchan/qui/reactive/html"
	"golang.org/x/text/number"
)

//go:embed locales/*.json
var locales embed.FS

//go:embed style.css
var styleCSS string

// languages is the switcher's contents. The label is deliberately the
// language's OWN name (an endonym): a user who has landed in a language
// they cannot read needs to find their own, and "Russian" does not help
// someone who only reads Russian.
var languages = []struct {
	loc   qui.Locale
	label string
}{
	{"en", "English"},
	{"zh-Hans", "简体中文"},
	{"zh-Hant", "繁體中文"},
	{"ja", "日本語"},
	{"de", "Deutsch"},
	{"ru", "Русский"},
	{"ar", "العربية"},
}

func main() {
	// Load catalogs and install the translator BEFORE building any
	// widgets. Order matters less than it looks — the tree resolves keys
	// at Draw time, so a late Install still works — but doing it first
	// avoids one avoidable full repaint at startup.
	if err := i18n.Install(); err != nil {
		log.Fatal(err)
	}
	if err := i18n.Load(locales, "locales"); err != nil {
		log.Fatal(err)
	}
	// Follow the OS language when we have a catalog for it; otherwise the
	// fallback locale (en) renders. Comment this out to always start in
	// English.
	qui.SetDefaultLocale(qui.SystemLocale())

	app, err := qui.NewApp()
	if err != nil {
		log.Fatal(err)
	}
	window, err := app.NewWindow("qui — i18n", 720, 720)
	if err != nil {
		log.Fatal(err)
	}
	window.SetRenderer(qui.NewGLRenderer())

	h.Mount(window, styleCSS, func() h.Node { return App(window) })

	if srv, err := agent.BindEnv(window); err == nil && srv != nil {
		window.OnClose(func() { _ = srv.Stop() })
	}
	app.Run()
}

func App(window *qui.Window) h.Node {
	// The active locale lives in component state only so the switcher can
	// highlight the current chip. The TEXT does not come from here — it
	// comes from the catalog at restyle time, which is why changing
	// language does not require this state at all.
	loc, setLoc := reactive.UseState(qui.DefaultLocale())
	// UseStateFn, not UseState: the ± handlers must read the LATEST count,
	// not the one captured when this render ran. Two clicks in one frame
	// would otherwise both apply to the same stale value.
	count, _, updateCount := reactive.UseStateFn(3)

	p := i18n.In(loc)
	now := time.Now()

	return h.Div(
		h.Div(
			h.H1("Internationalization").T("demo.title"),
			h.P("One tree, seven languages, no rebuild.").Class("sub").T("demo.subtitle"),
		).Class("header"),

		// --- language switcher -------------------------------------
		h.Div(
			langButtons(loc, func(l qui.Locale) {
				// This single call is the whole language switch: it bumps
				// the locale generation, drops the text-layout memo, and
				// fires every window's and style engine's subscription.
				qui.SetDefaultLocale(l)
				setLoc(l)
			}),
		).Class("langs").ID("langs"),

		// --- plurals ------------------------------------------------
		section("demo.section.plurals", "Plurals",
			h.Div(
				// TTitle, not T: the caption stays the "−" glyph in every
				// language and only the tooltip is translated. Using T here
				// would replace the glyph with "Eins entfernen" and blow out
				// the 34px button — which is precisely the bug class
				// `qui-i18n pseudo` exists to surface.
				h.Button("−").Class("step").ID("dec").TTitle("demo.dec").Attr("title", "Remove one").
					OnClick(func() { updateCount(func(n int) int { return max(0, n-1) }) }),
				h.Span(itoa(count)).Class("count").ID("count"),
				h.Button("+").Class("step").ID("inc").TTitle("demo.inc").Attr("title", "Add one").
					OnClick(func() { updateCount(func(n int) int { return n + 1 }) }),
			).Class("row"),
			// The engine picks the CLDR category for `count` in the active
			// locale. Arabic has six forms here, Russian four, English two.
			h.P("items").ID("plural").Class("result").TN("demo.items", count),
		),

		// --- formatting ---------------------------------------------
		section("demo.section.formats", "Formatting",
			kv("demo.label.number", "Number", p.Number(1234567.89), "fmt-number"),
			// Without an explicit precision x/text rounds a percentage to whole
			// units (7.55% -> 8%), which is right for a dashboard and wrong
			// for a rate. Precision is the caller's call, not the locale's.
			kv("demo.label.percent", "Percent", p.Percent(0.0755, number.MaxFractionDigits(2)), "fmt-percent"),
			kv("demo.label.currency", "Currency", p.Currency(1234.5, currencyFor(loc)), "fmt-currency"),
			kv("demo.label.date", "Date", p.Date(now, i18n.DateShort), "fmt-date"),
			kv("demo.label.relative", "Relative", p.RelativeTime(now.Add(-3*time.Hour), now), "fmt-relative"),
			kv("demo.label.list", "List", p.List(fruitNames(p), i18n.ListAnd), "fmt-list"),
		),

		// --- script rendering ---------------------------------------
		section("demo.section.text", "Text rendering",
			// Same code points in every locale. The shaper receives the
			// locale's language tag, so a font with `locl` coverage would
			// render regional forms here — the stock macOS CJK face does
			// not have one, so these are identical across zh/ja. The
			// switch that DOES change below is the translated sample line.
			h.P("骨 直 戸 今 令 海").Class("han").ID("han"),
			h.P("Shaping is locale-tagged; regional forms need a font with locl.").Class("note").T("demo.hanNote"),
			h.P("The quick brown fox jumps over the lazy dog.").Class("sample").ID("sample").T("demo.sample"),
		),
	).Class("page").ID("page")
}

// langButtons builds one chip per language. The chip labels are the only
// strings in this app that are NOT translated — see the note on
// `languages`.
func langButtons(active qui.Locale, onPick func(qui.Locale)) []h.Node {
	out := make([]h.Node, 0, len(languages))
	for _, lang := range languages {
		l := lang
		cls := "lang"
		if l.loc == active {
			cls += " active"
		}
		out = append(out, h.Button(l.label).
			Class(cls).
			// A stable, locale-independent id: an agent script switches
			// language with `click #lang-ar` regardless of what is on
			// screen.
			ID("lang-"+string(l.loc)).
			Attr("lang", string(l.loc)).
			OnClick(func() { onPick(l.loc) }))
	}
	return out
}

// section is a titled block. The heading takes a key plus its English
// literal — the literal is the fallback and the in-code documentation.
func section(key, literal string, body ...h.Node) h.Node {
	return h.Div(append([]h.Node{
		h.H2(literal).T(key),
	}, body...)).Class("section")
}

// kv renders one "label: value" row. The label is translated; the value
// is already locale-formatted by the caller's Printer.
func kv(key, literal, value, id string) h.Node {
	return h.Div(
		h.Span(literal).Class("k").T(key),
		h.Span(value).Class("v").ID(id),
	).Class("kv")
}

// currencyFor picks a currency that makes the locale's formatting
// visible. Real apps use the currency of the transaction, not of the UI
// language — this is a demo of symbol placement and grouping.
func currencyFor(loc qui.Locale) string {
	switch loc.Language() {
	case "zh":
		return "CNY"
	case "ja":
		return "JPY"
	case "de":
		return "EUR"
	case "ru":
		return "RUB"
	case "ar":
		return "AED"
	default:
		return "USD"
	}
}

// fruitNames returns three translated nouns to feed the list formatter,
// showing that list joining is a locale rule ("A, B, and C" vs "A、B和C").
func fruitNames(p *i18n.Printer) []string {
	return []string{
		p.T("demo.fruit.apple"),
		p.T("demo.fruit.pear"),
		p.T("demo.fruit.plum"),
	}
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
