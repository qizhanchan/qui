//go:build darwin && cgo

package macos

/*
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
	"unsafe"
)

// The NSSpellChecker bridge. Text is UTF-16; callers serialize access.

// SpellAvailable reports whether NSSpellChecker can be used.
func SpellAvailable() bool { return C.quiSpellAvailable() != 0 }

// SpellCheck returns misspelled ranges as UTF-16 (start, length) pairs.
// ok is false when the checker timed out.
func SpellCheck(u []uint16, lang string) (pairs []int32, ok bool) {
	if len(u) == 0 {
		return nil, true
	}
	clang, free := cStr(lang)
	defer free()
	var out *C.int32_t
	n := C.quiSpellCheck((*C.uint16_t)(unsafe.Pointer(&u[0])), C.int(len(u)), clang, &out)
	if n < 0 {
		return nil, false
	}
	if n == 0 || out == nil {
		return nil, true
	}
	pairs = make([]int32, 2*int(n))
	copy(pairs, unsafe.Slice((*int32)(unsafe.Pointer(out)), 2*int(n)))
	C.free(unsafe.Pointer(out))
	return pairs, true
}

// SpellGuesses returns NSSpellChecker's guesses for a word.
func SpellGuesses(u []uint16, lang string) []string {
	if len(u) == 0 {
		return nil
	}
	clang, free := cStr(lang)
	defer free()
	s := C.quiSpellGuesses((*C.uint16_t)(unsafe.Pointer(&u[0])), C.int(len(u)), clang)
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
	}
	return out
}

// SpellLearn adds a word to the user dictionary.
func SpellLearn(u []uint16) {
	if len(u) > 0 {
		C.quiSpellLearn((*C.uint16_t)(unsafe.Pointer(&u[0])), C.int(len(u)))
	}
}

// SpellUnlearn removes a word from the user dictionary.
func SpellUnlearn(u []uint16) {
	if len(u) > 0 {
		C.quiSpellUnlearn((*C.uint16_t)(unsafe.Pointer(&u[0])), C.int(len(u)))
	}
}

// SpellHasLearned reports whether the user dictionary holds the word.
func SpellHasLearned(u []uint16) bool {
	return len(u) > 0 && C.quiSpellHasLearned((*C.uint16_t)(unsafe.Pointer(&u[0])), C.int(len(u))) != 0
}
