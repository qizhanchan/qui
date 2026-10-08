//go:build darwin && cgo

package qui

/*
#cgo CFLAGS: -x objective-c -fobjc-arc
#cgo LDFLAGS: -framework Cocoa

#include <stdint.h>
#include <stdlib.h>

int quiSpellAvailable(void);
int quiSpellCheck(const uint16_t *s, int n, const char *lang, int32_t **out);
char *quiSpellGuesses(const uint16_t *w, int n, const char *lang);
void quiSpellLearn(const uint16_t *w, int n);
void quiSpellUnlearn(const uint16_t *w, int n);
int quiSpellHasLearned(const uint16_t *w, int n);
*/
import "C"

import (
	"strings"
	"sync"
	"unsafe"
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
	return C.quiSpellAvailable() != 0
}

func (c *darwinSpellChecker) Check(text, lang string) ([]SpellRange, error) {
	runes := []rune(text)
	if len(runes) == 0 {
		return nil, nil
	}
	u, startAt, endAt := encodeUTF16(runes)
	clang := C.CString(lang)
	defer C.free(unsafe.Pointer(clang))
	var out *C.int32_t
	c.mu.Lock()
	n := C.quiSpellCheck((*C.uint16_t)(unsafe.Pointer(&u[0])), C.int(len(u)), clang, &out)
	c.mu.Unlock()
	if n < 0 {
		return nil, ErrSpellTimeout
	}
	if n == 0 || out == nil {
		return nil, nil
	}
	pairs := make([]int32, 2*int(n))
	copy(pairs, unsafe.Slice((*int32)(unsafe.Pointer(out)), 2*int(n)))
	C.free(unsafe.Pointer(out))
	return spellRangesFromUTF16(pairs, startAt, endAt), nil
}

func (c *darwinSpellChecker) Suggestions(word, lang string) []string {
	u, _, _ := encodeUTF16([]rune(word))
	if len(u) == 0 {
		return nil
	}
	clang := C.CString(lang)
	defer C.free(unsafe.Pointer(clang))
	c.mu.Lock()
	s := C.quiSpellGuesses((*C.uint16_t)(unsafe.Pointer(&u[0])), C.int(len(u)), clang)
	c.mu.Unlock()
	if s == nil {
		return nil
	}
	joined := C.GoString(s)
	C.free(unsafe.Pointer(s))
	var out []string
	for _, g := range strings.Split(joined, "\n") {
		if g != "" {
			out = append(out, g)
		}
		if len(out) == 8 {
			break
		}
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
		C.quiSpellLearn((*C.uint16_t)(unsafe.Pointer(&u[0])), C.int(len(u)))
		c.mu.Unlock()
	}
}

func (c *darwinSpellChecker) Unlearn(word string) {
	if u, ok := c.word16(word); ok {
		c.mu.Lock()
		C.quiSpellUnlearn((*C.uint16_t)(unsafe.Pointer(&u[0])), C.int(len(u)))
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
	return C.quiSpellHasLearned((*C.uint16_t)(unsafe.Pointer(&u[0])), C.int(len(u))) != 0
}
