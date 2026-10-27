//go:build darwin && cgo

package qui

import (
	"sync"

	"github.com/qizhanchan/qui/internal/platform/macos"
)

// darwinSpellChecker bridges NSSpellChecker. One mutex serializes every
// call so the shared checker never sees concurrent requests from Go.
type darwinSpellChecker struct {
	mu sync.Mutex
}

var systemSpell = &darwinSpellChecker{}

func platformSpellChecker() SpellChecker { return systemSpell }

func (c *darwinSpellChecker) Available() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return macos.SpellAvailable()
}

func (c *darwinSpellChecker) Check(text, lang string) ([]SpellRange, error) {
	runes := []rune(text)
	if len(runes) == 0 {
		return nil, nil
	}
	u, startAt, endAt := encodeUTF16(runes)
	c.mu.Lock()
	pairs, ok := macos.SpellCheck(u, lang)
	c.mu.Unlock()
	if !ok {
		return nil, ErrSpellTimeout
	}
	if len(pairs) == 0 {
		return nil, nil
	}
	return spellRangesFromUTF16(pairs, startAt, endAt), nil
}

func (c *darwinSpellChecker) Suggestions(word, lang string) []string {
	u, _, _ := encodeUTF16([]rune(word))
	if len(u) == 0 {
		return nil
	}
	c.mu.Lock()
	out := macos.SpellGuesses(u, lang)
	c.mu.Unlock()
	if len(out) > 8 {
		out = out[:8]
	}
	return out
}

func (c *darwinSpellChecker) word16(word string) ([]uint16, bool) {
	u, _, _ := encodeUTF16([]rune(word))
	return u, len(u) > 0
}

func (c *darwinSpellChecker) Learn(word string) {
	if u, ok := c.word16(word); ok {
		c.mu.Lock()
		macos.SpellLearn(u)
		c.mu.Unlock()
	}
}

func (c *darwinSpellChecker) Unlearn(word string) {
	if u, ok := c.word16(word); ok {
		c.mu.Lock()
		macos.SpellUnlearn(u)
		c.mu.Unlock()
	}
}

func (c *darwinSpellChecker) HasLearned(word string) bool {
	u, ok := c.word16(word)
	if !ok {
		return false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return macos.SpellHasLearned(u)
}
