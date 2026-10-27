package typeset

import (
	"sync"

	shapinglang "github.com/go-text/typesetting/language"
)

// The engine's locale lives above the text engine. It hands in how to read
// the default locale and its generation counter: shaping picks its
// language from the first, and every cache of measured or shaped text
// keys on the second.
var (
	typesetLocaleTag = func() string { return "" }
	typesetLocaleGen = func() uint64 { return 0 }
)

// SetLocaleSource installs the locale source. Call it once, at init.
func SetLocaleSource(tag func() string, gen func() uint64) {
	typesetLocaleTag, typesetLocaleGen = tag, gen
}

var shapingLanguageCache struct {
	mu   sync.RWMutex
	gen  uint64
	lang shapinglang.Language
}

// ShapingLanguage returns the OpenType language tag handed to the
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
func ShapingLanguage() shapinglang.Language {
	gen := typesetLocaleGen()
	shapingLanguageCache.mu.RLock()
	if shapingLanguageCache.gen == gen {
		l := shapingLanguageCache.lang
		shapingLanguageCache.mu.RUnlock()
		return l
	}
	shapingLanguageCache.mu.RUnlock()

	lang := shapinglang.NewLanguage(typesetLocaleTag())
	shapingLanguageCache.mu.Lock()
	shapingLanguageCache.gen = gen
	shapingLanguageCache.lang = lang
	shapingLanguageCache.mu.Unlock()
	return lang
}
