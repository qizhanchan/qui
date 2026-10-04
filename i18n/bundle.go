package i18n

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"math"
	"path"
	"sort"
	"strings"
	"sync"

	"github.com/qizhanchan/qui"
	"golang.org/x/text/feature/plural"
	"golang.org/x/text/language"
)

// Bundle is a set of message catalogs keyed by locale. It satisfies
// qui.Translator.
//
// A Bundle is safe for concurrent reads once loaded; Load/Register take
// a write lock. Translation happens on the UI goroutine during Draw and
// on the agent goroutine during tree serialization, so the read path is
// a plain RWMutex-guarded map lookup with no allocation for the common
// no-placeholder message.
type Bundle struct {
	mu sync.RWMutex
	// catalogs is keyed by canonicalized locale string. Lookup walks
	// the fallback chain in root qui (Locale.Fallbacks), so this map
	// holds only exactly-specified locales — no derived entries.
	catalogs map[qui.Locale]map[string]Message
	// tags caches the parsed language.Tag per locale for the plural
	// matcher, which needs a real tag rather than a string.
	tags map[qui.Locale]language.Tag
}

// NewBundle returns an empty bundle.
func NewBundle() *Bundle {
	return &Bundle{
		catalogs: map[qui.Locale]map[string]Message{},
		tags:     map[qui.Locale]language.Tag{},
	}
}

// Register adds (or merges into) the catalog for one locale. Later
// registrations override earlier ones key by key, which is how an
// application overrides a framework-supplied qui.* string.
func (b *Bundle) Register(loc qui.Locale, msgs map[string]Message) {
	if loc == "" || len(msgs) == 0 {
		return
	}
	key := canonical(loc)
	b.mu.Lock()
	defer b.mu.Unlock()
	cat := b.catalogs[key]
	if cat == nil {
		cat = make(map[string]Message, len(msgs))
		b.catalogs[key] = cat
	}
	for k, v := range msgs {
		cat[k] = v
	}
	if _, ok := b.tags[key]; !ok {
		b.tags[key] = parseTag(key)
	}
}

// Load reads every *.json file under dir in fsys as a catalog, taking
// the locale from the file's base name: "zh-Hans.json" -> "zh-Hans".
//
// Files named "qui.<locale>.json" are treated as locale <locale> too —
// that prefix exists only to keep the framework's own strings in
// separate files from an application's, in the same directory.
//
// All files are parsed before any is registered, so a syntax error in
// one locale does not leave the bundle half-loaded.
func (b *Bundle) Load(fsys fs.FS, dir string) error {
	if dir == "" {
		dir = "."
	}
	entries, err := fs.ReadDir(fsys, dir)
	if err != nil {
		return fmt.Errorf("i18n: read %s: %w", dir, err)
	}
	type pending struct {
		loc  qui.Locale
		msgs map[string]Message
	}
	var staged []pending
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		names = append(names, e.Name())
	}
	// Deterministic order so "last registration wins" is reproducible.
	sort.Strings(names)
	for _, name := range names {
		data, err := fs.ReadFile(fsys, path.Join(dir, name))
		if err != nil {
			return fmt.Errorf("i18n: read %s: %w", name, err)
		}
		msgs, err := ParseCatalog(data)
		if err != nil {
			return fmt.Errorf("i18n: parse %s: %w", name, err)
		}
		staged = append(staged, pending{loc: localeFromFilename(name), msgs: msgs})
	}
	for _, p := range staged {
		if p.loc == "" {
			continue
		}
		b.Register(p.loc, p.msgs)
	}
	return nil
}

// ParseCatalog decodes one catalog file. The reserved "@meta" key is
// accepted and skipped, so catalogs can carry authoring metadata
// (translator notes, source revision) without polluting the key space.
func ParseCatalog(data []byte) (map[string]Message, error) {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, err
	}
	out := make(map[string]Message, len(raw))
	for k, v := range raw {
		if strings.HasPrefix(k, "@") {
			continue
		}
		var m Message
		if err := m.UnmarshalJSON(v); err != nil {
			return nil, fmt.Errorf("key %q: %w", k, err)
		}
		out[k] = m
	}
	return out, nil
}

// localeFromFilename strips the .json suffix and an optional "qui."
// namespace prefix: "qui.zh-Hans.json" -> "zh-Hans".
func localeFromFilename(name string) qui.Locale {
	base := strings.TrimSuffix(name, ".json")
	if i := strings.IndexByte(base, '.'); i >= 0 {
		base = base[i+1:]
	}
	return canonical(qui.Locale(base))
}

// Locales returns the locales this bundle has catalogs for, sorted.
func (b *Bundle) Locales() []qui.Locale {
	b.mu.RLock()
	defer b.mu.RUnlock()
	out := make([]qui.Locale, 0, len(b.catalogs))
	for l := range b.catalogs {
		out = append(out, l)
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

// Keys returns the message keys defined for a locale, sorted.
func (b *Bundle) Keys(loc qui.Locale) []string {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return sortedKeys(b.catalogs[canonical(loc)])
}

// Lookup returns the raw Message for an exact locale (no fallback).
func (b *Bundle) Lookup(loc qui.Locale, key string) (Message, bool) {
	b.mu.RLock()
	defer b.mu.RUnlock()
	m, ok := b.catalogs[canonical(loc)][key]
	return m, ok
}

// Translate implements qui.Translator.
//
// Note the contract: this resolves an EXACT locale only. Walking the
// fallback chain (zh-Hant-TW -> zh-Hant -> zh) and then the default
// locale is root qui.Translate's job — it calls this once per candidate.
// Keeping the walk in root means every Translator implementation gets
// identical fallback semantics for free.
func (b *Bundle) Translate(loc qui.Locale, key string, args map[string]any) (string, bool) {
	m, ok := b.Lookup(loc, key)
	if !ok {
		return "", false
	}
	text := m.Text
	if m.IsPlural() {
		// A plural message asked for without a count: "other" is the
		// only form guaranteed to exist and is the least-wrong choice.
		text = m.Forms["other"]
	}
	return interpolate(text, args), true
}

// TranslatePlural implements qui.Translator, selecting a CLDR plural
// category for n.
//
// {n} is bound to the count automatically unless args already supplies
// it, so catalogs can write "{n} items" without every call site passing
// the count twice.
func (b *Bundle) TranslatePlural(loc qui.Locale, key string, n float64, args map[string]any) (string, bool) {
	m, ok := b.Lookup(loc, key)
	if !ok {
		return "", false
	}
	if _, has := args["n"]; !has {
		merged := make(map[string]any, len(args)+1)
		for k, v := range args {
			merged[k] = v
		}
		merged["n"] = n
		args = merged
	}
	if !m.IsPlural() {
		return interpolate(m.Text, args), true
	}
	cat := formName(b.pluralForm(loc, n))
	text, ok := m.Forms[cat]
	if !ok {
		text = m.Forms["other"]
	}
	return interpolate(text, args), true
}

// pluralForm computes the CLDR operands for n and asks x/text which
// category the locale puts it in.
//
// Operands (UTS #35): i = integer part, v = visible fraction digits with
// trailing zeros, w = the same without, f = fraction digits as an
// integer with trailing zeros, t = the same without. We render at most 3
// fraction digits, which matches what a UI count or measurement carries
// and keeps the operands inside int range.
func (b *Bundle) pluralForm(loc qui.Locale, n float64) plural.Form {
	b.mu.RLock()
	tag, ok := b.tags[canonical(loc)]
	b.mu.RUnlock()
	if !ok {
		tag = parseTag(loc)
	}

	neg := n < 0
	if neg {
		n = -n // CLDR operands are defined on the absolute value.
	}
	i := int(math.Floor(n))
	frac := n - math.Floor(n)
	// Round to 3 decimals, then strip trailing zeros to get w/t.
	f := int(math.Round(frac * 1000))
	if f >= 1000 { // rounding carried into the integer part
		i++
		f = 0
	}
	v, t := 3, f
	if f == 0 {
		v, t = 0, 0
	} else {
		for t%10 == 0 {
			t /= 10
		}
	}
	w := 0
	for x := t; x > 0; x /= 10 {
		w++
	}
	if f == 0 {
		v = 0
	}
	return plural.Cardinal.MatchPlural(tag, i, v, w, f, t)
}

func parseTag(loc qui.Locale) language.Tag {
	tag, err := language.Parse(string(loc))
	if err != nil {
		return language.English
	}
	return tag
}

// canonical routes through root's normalization so bundle keys and
// engine-side fallback chains agree on casing and separators.
func canonical(loc qui.Locale) qui.Locale {
	if loc == "" {
		return ""
	}
	// Locale.Fallbacks canonicalizes internally and returns the full
	// tag first; that is the cheapest exported route to the normalized
	// form without duplicating the rules here.
	if fb := loc.Fallbacks(); len(fb) > 0 {
		return fb[0]
	}
	return loc
}
