package qui

import (
	"log"
	"os"
	"strings"
	"sync"
	"sync/atomic"

	shapinglang "github.com/go-text/typesetting/language"
)

// i18nStrictEnabled turns on QUI_I18N_STRICT=1 diagnostics: unresolved
// message keys render bracketed (⟦file.save⟧) and log once each, so a
// missing translation is impossible to overlook while developing.
// Off in production, where a bare key is the friendlier degradation.
var i18nStrictEnabled = os.Getenv("QUI_I18N_STRICT") == "1"

func i18nStrict() bool { return i18nStrictEnabled }

// Locale is a BCP-47 language tag — "en", "zh-Hans", "zh-Hant-TW",
// "ar-EG", "pt-BR". The zero value means "follow the system", resolved
// lazily by DefaultLocale via SystemLocale.
//
// Locale is deliberately a string, not a parsed struct: it crosses the
// root/subpackage seam (Translator), lands in AX JSON, and gets compared
// in cache keys. A string does all of that for free. The subpackage that
// needs real CLDR semantics (i18n) parses it with golang.org/x/text.
//
// Root qui only needs three things from a tag — its language subtag, its
// writing direction, and its fallback chain — and all three are cheap
// prefix work that does not justify pulling a matcher into the engine.
type Locale string

// String makes Locale printable without a conversion at every call site.
func (l Locale) String() string { return string(l) }

// IsZero reports whether the locale is unset ("follow the system").
func (l Locale) IsZero() bool { return l == "" }

// canonical normalizes separator and casing: language lowercase, script
// title-case, region uppercase — "ZH_hant_tw" becomes "zh-Hant-TW".
func (l Locale) canonical() Locale {
	if l == "" {
		return ""
	}
	parts := strings.Split(strings.ReplaceAll(string(l), "_", "-"), "-")
	out := parts[:0]
	for i, p := range parts {
		if p == "" {
			continue
		}
		switch {
		case i == 0:
			p = strings.ToLower(p)
		case len(p) == 4:
			p = strings.ToUpper(p[:1]) + strings.ToLower(p[1:])
		case len(p) <= 3:
			p = strings.ToUpper(p)
		default:
			p = strings.ToLower(p)
		}
		out = append(out, p)
	}
	return Locale(strings.Join(out, "-"))
}

// Language returns the primary language subtag: "zh-Hans-CN" -> "zh".
func (l Locale) Language() string {
	c := string(l.canonical())
	if i := strings.IndexByte(c, '-'); i >= 0 {
		return c[:i]
	}
	return c
}

// Script returns the 4-letter script subtag, or "" when absent:
// "zh-Hans-CN" -> "Hans".
func (l Locale) Script() string {
	for _, p := range strings.Split(string(l.canonical()), "-")[1:] {
		if len(p) == 4 {
			return p
		}
	}
	return ""
}

// Region returns the region subtag, or "" when absent:
// "zh-Hans-CN" -> "CN".
func (l Locale) Region() string {
	parts := strings.Split(string(l.canonical()), "-")
	for _, p := range parts[1:] {
		if len(p) == 4 {
			continue // script
		}
		if len(p) == 2 || len(p) == 3 {
			return p
		}
	}
	return ""
}

// rtlLanguages is the set of language subtags written right-to-left.
// Kept as an explicit table rather than derived from CLDR data because
// the list is short, stable, and the engine must answer Direction()
// without loading locale data.
var rtlLanguages = map[string]bool{
	"ar":  true, // Arabic
	"he":  true, // Hebrew
	"iw":  true, // Hebrew (legacy code)
	"fa":  true, // Persian
	"ur":  true, // Urdu
	"ps":  true, // Pashto
	"sd":  true, // Sindhi
	"ug":  true, // Uyghur
	"yi":  true, // Yiddish
	"ji":  true, // Yiddish (legacy code)
	"ckb": true, // Central Kurdish
	"dv":  true, // Divehi
	"ku":  true, // Kurdish (Arabic script in practice)
	"nqo": true, // N'Ko
	"syr": true, // Syriac
	"arc": true, // Aramaic
}

// rtlScripts covers tags that carry direction on the script subtag
// rather than the language — "az-Arab" is RTL while plain "az" is not.
var rtlScripts = map[string]bool{
	"Arab": true, "Hebr": true, "Syrc": true, "Thaa": true,
	"Nkoo": true, "Adlm": true, "Mand": true, "Samr": true,
}

// Direction reports the writing direction of the locale. An unset
// locale, or one whose language is unknown, resolves to LTR — the same
// neutral default resolveTextDirection uses for direction-free text.
//
// This is the value that seeds a window's base text direction and (once
// RTL mirroring lands) the layout direction of its root widget.
func (l Locale) Direction() TextDirection {
	if l == "" {
		return TextDirectionLTR
	}
	if s := l.Script(); s != "" {
		if rtlScripts[s] {
			return TextDirectionRTL
		}
		// An explicit non-RTL script overrides the language table:
		// "az-Latn" is LTR even though "az-Arab" is not.
		return TextDirectionLTR
	}
	if rtlLanguages[l.Language()] {
		return TextDirectionRTL
	}
	return TextDirectionLTR
}

// Fallbacks returns the lookup chain for this locale, most specific
// first, ending with the bare language:
//
//	"zh-Hant-TW" -> ["zh-Hant-TW", "zh-Hant", "zh"]
//	"en"         -> ["en"]
//
// The chain is what a message bundle walks before giving up. Root only
// builds it; deciding what to do at the end of the chain (default
// locale, then the key itself) is Translate's job.
func (l Locale) Fallbacks() []Locale {
	c := l.canonical()
	if c == "" {
		return nil
	}
	parts := strings.Split(string(c), "-")
	out := make([]Locale, 0, len(parts))
	for i := len(parts); i > 0; i-- {
		out = append(out, Locale(strings.Join(parts[:i], "-")))
	}
	return out
}

// Translator resolves message keys to localized strings. It is the sole
// seam between the engine and the i18n subpackage: root defines the
// interface, the subpackage implements it, and root never imports it —
// the same arrangement as Animator, IMEClient and VectorSource.
//
// Implementations must be safe for concurrent use: Translate is called
// from Draw on the UI goroutine and from the agent goroutine when it
// serializes the accessibility tree.
type Translator interface {
	// Translate returns the message for key in loc, and whether it was
	// found. args holds named placeholder values ({name} in the message).
	Translate(loc Locale, key string, args map[string]any) (string, bool)

	// TranslatePlural selects a CLDR plural form for n. Implementations
	// that do not do plurals may delegate to Translate.
	TranslatePlural(loc Locale, key string, n float64, args map[string]any) (string, bool)
}

var (
	localeMu         sync.RWMutex
	translator       Translator
	defaultLocale    Locale
	fallbackLocale   Locale = "en"
	systemLocaleOnce sync.Once
	systemLocaleVal  Locale
	localeSubs       []func()
	localeGen        atomic.Uint64
	// missingLogged dedupes the strict-mode miss log so a key rendered
	// every frame does not flood the terminal.
	missingLogged sync.Map // string -> struct{}
)

// LocaleGeneration returns a counter that changes every time the active
// locale or the installed Translator does. Anything caching a resolved
// string, a measured text layout, or a shaped paragraph must include it
// in the cache key — exactly like FontRegistryGeneration, and for the
// same reason: the same input text can measure differently after the
// change (locale-sensitive shaping picks different glyphs).
//
// Cheap enough to read on every cache lookup.
func LocaleGeneration() uint64 { return localeGen.Load() }

// SetTranslator installs the message source. Passing nil removes it, at
// which point Translate degrades to returning the key.
//
// Call once at startup, before constructing widgets — i18n.Install()
// wraps this.
func SetTranslator(t Translator) {
	localeMu.Lock()
	translator = t
	localeMu.Unlock()
	bumpLocale()
}

// CurrentTranslator returns the installed Translator, or nil.
func CurrentTranslator() Translator {
	localeMu.RLock()
	defer localeMu.RUnlock()
	return translator
}

// SetDefaultLocale swaps the process-wide locale. Every subscribed
// window repaints and re-lays-out, so switching languages at runtime is
// a single call — widgets resolve their message keys inside Draw, they
// do not cache resolved text across a generation bump.
//
// Passing the zero Locale reverts to "follow the system".
func SetDefaultLocale(l Locale) {
	localeMu.Lock()
	defaultLocale = l.canonical()
	subs := append([]func(){}, localeSubs...)
	localeMu.Unlock()
	bumpLocale()
	for _, fn := range subs {
		if fn != nil {
			fn()
		}
	}
}

// DefaultLocale returns the active process-wide locale, resolving the
// zero value through SystemLocale. Never returns "" — a locale-free
// process reports "en".
func DefaultLocale() Locale {
	localeMu.RLock()
	l := defaultLocale
	localeMu.RUnlock()
	if l != "" {
		return l
	}
	if s := SystemLocale(); s != "" {
		return s
	}
	return "en"
}

// SetFallbackLocale designates the catalog every lookup ends at — the
// locale the application's messages were authored in. Defaults to "en".
//
// This is the difference between a user on an unsupported locale seeing
// English and seeing raw message keys: DefaultLocale is where we TRY to
// translate, FallbackLocale is what we KNOW is complete. They are only
// the same by coincidence.
//
// Pass the zero Locale to disable the final step, which makes missing
// translations render as keys everywhere — occasionally useful when
// auditing coverage.
func SetFallbackLocale(l Locale) {
	localeMu.Lock()
	fallbackLocale = l.canonical()
	localeMu.Unlock()
	bumpLocale()
}

// FallbackLocale returns the locale lookups end at. See SetFallbackLocale.
func FallbackLocale() Locale {
	localeMu.RLock()
	defer localeMu.RUnlock()
	return fallbackLocale
}

// SubscribeLocale registers a callback fired whenever the active locale
// changes, and returns a function that removes the subscription. Window
// hooks itself here so language switches flow through without
// widget-level plumbing; mirrors SubscribeTheme.
func SubscribeLocale(fn func()) func() {
	localeMu.Lock()
	idx := len(localeSubs)
	localeSubs = append(localeSubs, fn)
	localeMu.Unlock()
	return func() {
		localeMu.Lock()
		if idx >= 0 && idx < len(localeSubs) {
			localeSubs[idx] = nil
		}
		localeMu.Unlock()
	}
}

// SystemLocale returns the OS preferred language, or "" when it cannot
// be determined. Resolved once per process — the OS value does not
// change under a running app on any platform we target, and callers hit
// this on the Draw path.
func SystemLocale() Locale {
	systemLocaleOnce.Do(func() {
		systemLocaleVal = platformSystemLocale().canonical()
	})
	return systemLocaleVal
}

func bumpLocale() {
	localeGen.Add(1)
	// Resolved-text and shaped-glyph caches embed locale-dependent
	// results; a language switch invalidates them wholesale.
	invalidateTextLayoutCache()
}

// Translate resolves a message key in loc through the installed
// Translator, walking the locale's fallback chain and then the default
// locale before giving up.
//
// Unresolvable keys return the key itself — a missing translation shows
// a developer-legible string rather than an empty widget. Under
// QUI_I18N_STRICT=1 the key is bracketed (⟦file.save⟧) and logged once,
// so gaps are impossible to miss during development.
//
// args supplies named placeholders; pass nil when the message has none.
func Translate(loc Locale, key string, args map[string]any) string {
	if key == "" {
		return ""
	}
	t := CurrentTranslator()
	if t == nil {
		return missingMessage(key)
	}
	for _, cand := range lookupChain(loc) {
		if s, ok := t.Translate(cand, key, args); ok {
			return s
		}
	}
	return missingMessage(key)
}

// lookupChain builds the full ordered candidate list for a lookup:
// the requested locale's fallbacks, then the active default locale's,
// then the authored fallback locale's — deduped, order preserved.
//
// Three tiers rather than two because "the locale we're rendering in",
// "the locale the process is set to", and "the locale the strings were
// written in" are independent. A per-window Japanese view inside an
// app running in German, whose catalogs were authored in English, must
// try ja, then de, then en before giving up.
func lookupChain(loc Locale) []Locale {
	if loc == "" {
		loc = DefaultLocale()
	}
	out := make([]Locale, 0, 6)
	seen := make(map[Locale]bool, 6)
	add := func(l Locale) {
		if l == "" {
			return
		}
		for _, cand := range l.Fallbacks() {
			if !seen[cand] {
				seen[cand] = true
				out = append(out, cand)
			}
		}
	}
	add(loc)
	add(DefaultLocale())
	add(FallbackLocale())
	return out
}

// TranslatePlural is Translate for count-dependent messages. n selects a
// CLDR plural category ("one", "other", and for some languages "zero",
// "two", "few", "many"); the chosen form still goes through placeholder
// substitution, and {n} is bound to the count automatically.
func TranslatePlural(loc Locale, key string, n float64, args map[string]any) string {
	if key == "" {
		return ""
	}
	t := CurrentTranslator()
	if t == nil {
		return missingMessage(key)
	}
	for _, cand := range lookupChain(loc) {
		if s, ok := t.TranslatePlural(cand, key, n, args); ok {
			return s
		}
	}
	return missingMessage(key)
}

// T is the ergonomic form of Translate for the active default locale.
// Widgets on the Draw path use it; it takes no map so the common
// no-placeholder case allocates nothing.
func T(key string) string { return Translate("", key, nil) }

// TranslateOr is Translate with an explicit fallback string instead of
// the key.
//
// This is what engine and library code should use for its own strings.
// An application that never calls i18n.Install has no Translator at
// all, and rendering "qui.submit" on a submit button would be a
// regression for every app that does not use i18n. With a fallback, the
// untranslated path renders the English source text — which is exactly
// what those apps got before — and the translated path takes over the
// moment a catalog is installed.
//
// The fallback is the source string, so it doubles as documentation of
// what the key means at the call site.
func TranslateOr(loc Locale, key, fallback string, args map[string]any) string {
	if key == "" {
		return fallback
	}
	t := CurrentTranslator()
	if t == nil {
		return fallback
	}
	for _, cand := range lookupChain(loc) {
		if s, ok := t.Translate(cand, key, args); ok {
			return s
		}
	}
	if i18nStrict() {
		// A Translator IS installed and this key is not in it — that is a
		// genuine coverage gap, and hiding it behind the fallback is
		// exactly what strict mode exists to prevent.
		return missingMessage(key)
	}
	return fallback
}

// TOr is TranslateOr for the active locale with no placeholders.
func TOr(key, fallback string) string { return TranslateOr("", key, fallback, nil) }

// SetLocale overrides the process-wide locale for this window only.
// Pass the zero Locale to go back to following DefaultLocale.
//
// The window repaints and re-lays-out immediately. Prefer
// SetDefaultLocale for the ordinary "the user changed the app language"
// case — this exists for multi-window apps that genuinely need two
// languages on screen at once.
func (w *Window) SetLocale(l Locale) {
	if w == nil {
		return
	}
	l = l.canonical()
	if w.locale == l {
		return
	}
	w.locale = l
	w.Invalidate()
	w.InvalidateLayout()
}

// Locale returns the window's effective locale: its own override when
// set, otherwise DefaultLocale.
func (w *Window) Locale() Locale {
	if w == nil {
		return DefaultLocale()
	}
	if w.locale != "" {
		return w.locale
	}
	return DefaultLocale()
}

// TextDirection returns the base writing direction implied by the
// window's locale. Text with no strong directional character inherits
// it, and it seeds the root widget's layout direction.
func (w *Window) TextDirection() TextDirection { return w.Locale().Direction() }

// shapingLanguageCache memoizes the go-text language tag for the active
// locale. shapeParagraph asks for it on every run, so the conversion
// must not re-parse a string each time.
var shapingLanguageCache struct {
	mu   sync.RWMutex
	gen  uint64
	lang shapinglang.Language
}

// shapingLanguage returns the OpenType language tag handed to the
// shaper for the active locale.
//
// This is what makes Han unification render correctly. U+9AA8 (骨) has a
// different standard glyph in Simplified Chinese, Traditional Chinese
// and Japanese, and a font covering all three ships all three behind a
// `locl` feature keyed on the language tag. Without a language, the
// shaper takes the font's default — which is right for at most one of
// the three audiences. Same mechanism covers Turkish dotted/dotless i
// and Serbian Cyrillic italic forms.
//
// Note this necessarily makes shaped output locale-dependent, which is
// why LocaleGeneration is part of every text-layout cache key.
func shapingLanguage() shapinglang.Language {
	gen := localeGen.Load()
	shapingLanguageCache.mu.RLock()
	if shapingLanguageCache.gen == gen {
		l := shapingLanguageCache.lang
		shapingLanguageCache.mu.RUnlock()
		return l
	}
	shapingLanguageCache.mu.RUnlock()

	lang := shapinglang.NewLanguage(string(DefaultLocale()))
	shapingLanguageCache.mu.Lock()
	shapingLanguageCache.gen = gen
	shapingLanguageCache.lang = lang
	shapingLanguageCache.mu.Unlock()
	return lang
}

func missingMessage(key string) string {
	if !i18nStrict() {
		return key
	}
	if _, loaded := missingLogged.LoadOrStore(key, struct{}{}); !loaded {
		log.Printf("qui/i18n: missing message %q", key)
	}
	return "⟦" + key + "⟧"
}
