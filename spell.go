package qui

import (
	"errors"
	"sort"
	"sync"
	"unicode/utf8"
)

// SpellRange is a misspelled span inside the text passed to
// SpellChecker.Check, as half-open RUNE offsets [Start, End).
type SpellRange struct {
	Start, End int
}

var (
	// ErrSpellUnavailable is returned by Check when no spell checker
	// backend exists on this platform/build.
	ErrSpellUnavailable = errors.New("qui: spell checking unavailable")
	// ErrSpellTimeout is returned by Check when the platform checker did
	// not answer in time (background-thread path).
	ErrSpellTimeout = errors.New("qui: spell check timed out")
)

// SpellChecker is a system (or app-supplied) spell checker. All methods
// must be safe for concurrent use; Check may be called off the UI thread.
type SpellChecker interface {
	// Available reports whether the checker can check text at all.
	Available() bool
	// Check returns the misspelled ranges of text (rune offsets, sorted).
	// lang is a BCP-47-ish language code ("en", "en_US"); "" = automatic.
	Check(text, lang string) ([]SpellRange, error)
	// Suggestions returns replacement guesses for word, best first.
	Suggestions(word, lang string) []string
	// Learn adds word to the user's dictionary.
	Learn(word string)
	// Unlearn removes word from the user's dictionary.
	Unlearn(word string)
	// HasLearned reports whether word is in the user's dictionary.
	HasLearned(word string) bool
}

var (
	spellMu       sync.RWMutex
	spellOverride SpellChecker
)

// SystemSpellChecker returns the platform spell checker (NSSpellChecker on
// macOS with cgo). It is never nil: on other builds it reports
// Available() == false and Check returns ErrSpellUnavailable.
func SystemSpellChecker() SpellChecker { return platformSpellChecker() }

// SetSpellChecker overrides the checker returned by CurrentSpellChecker.
// Pass nil to restore the system checker.
func SetSpellChecker(c SpellChecker) {
	spellMu.Lock()
	spellOverride = c
	spellMu.Unlock()
}

// CurrentSpellChecker returns the override installed with SetSpellChecker,
// or the system checker.
func CurrentSpellChecker() SpellChecker {
	spellMu.RLock()
	c := spellOverride
	spellMu.RUnlock()
	if c != nil {
		return c
	}
	return SystemSpellChecker()
}

// SpellCheckAvailable reports CurrentSpellChecker().Available().
func SpellCheckAvailable() bool { return CurrentSpellChecker().Available() }

// SpellCheck checks text with the current checker, dropping any error
// (an unavailable or timed-out checker yields no ranges).
func SpellCheck(text, lang string) []SpellRange {
	rs, _ := CurrentSpellChecker().Check(text, lang)
	return rs
}

// SpellSuggestions returns guesses for word from the current checker.
func SpellSuggestions(word, lang string) []string {
	return CurrentSpellChecker().Suggestions(word, lang)
}

// LearnWord adds word to the current checker's user dictionary.
func LearnWord(word string) { CurrentSpellChecker().Learn(word) }

// UnlearnWord removes word from the current checker's user dictionary.
func UnlearnWord(word string) { CurrentSpellChecker().Unlearn(word) }

// encodeUTF16 converts runes to UTF-16 code units plus two maps from
// code-unit offsets back to rune offsets (both of length len(u)+1):
// startAt[k] is the index of the rune containing unit k (floor), endAt[k]
// the number of runes lying entirely before unit k (ceil). Surrogate
// halves and invalid runes are encoded as U+FFFD (one unit) so rune and
// unit counts stay in lock-step.
func encodeUTF16(runes []rune) (u []uint16, startAt, endAt []int) {
	u = make([]uint16, 0, len(runes))
	startAt = make([]int, 0, len(runes)+1)
	endAt = make([]int, 0, len(runes)+1)
	for i, r := range runes {
		if r < 0 || r > utf8.MaxRune || (r >= 0xD800 && r <= 0xDFFF) {
			r = utf8.RuneError
		}
		if r >= 0x10000 {
			r -= 0x10000
			u = append(u, uint16(0xD800+(r>>10)), uint16(0xDC00+(r&0x3FF)))
			startAt = append(startAt, i, i)
			endAt = append(endAt, i, i+1)
			continue
		}
		u = append(u, uint16(r))
		startAt = append(startAt, i)
		endAt = append(endAt, i)
	}
	startAt = append(startAt, len(runes))
	endAt = append(endAt, len(runes))
	return u, startAt, endAt
}

// spellRangesFromUTF16 maps (location, length) UTF-16 pairs back to rune
// ranges via the encodeUTF16 maps, clamping out-of-range values, dropping
// empty ranges and sorting by Start.
func spellRangesFromUTF16(pairs []int32, startAt, endAt []int) []SpellRange {
	n := len(startAt) - 1
	if n < 0 {
		return nil
	}
	clamp := func(v int) int {
		if v < 0 {
			return 0
		}
		if v > n {
			return n
		}
		return v
	}
	var out []SpellRange
	for i := 0; i+1 < len(pairs); i += 2 {
		loc := clamp(int(pairs[i]))
		end := clamp(int(pairs[i]) + int(pairs[i+1]))
		s, e := startAt[loc], endAt[end]
		if e <= s {
			continue
		}
		out = append(out, SpellRange{Start: s, End: e})
	}
	sort.SliceStable(out, func(a, b int) bool { return out[a].Start < out[b].Start })
	return out
}
