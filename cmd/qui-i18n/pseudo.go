package main

import (
	"strings"
	"unicode"
)

// Pseudo-localization.
//
// The generated locale is not a translation — it is a test fixture that
// surfaces three classes of bug without a translator:
//
//  1. Strings that never reach the catalog. Every localized string comes
//     out visibly accented and bracketed; anything still plain ASCII on
//     screen is hard-coded somewhere.
//  2. Layout that only fits English. Padding stretches each message ~40%,
//     which is roughly the German/Finnish expansion factor, so labels
//     that will clip in a real translation clip here first.
//  3. Font coverage gaps. The accented forms exercise the fallback chain;
//     tofu boxes mean the font lacks Latin-1 Supplement / Latin Extended.
//
// Brackets bound each string, so a truncated label is distinguishable
// from a short one: "⟪Sàvé…" is clipped, "⟪Sàvé⟫" is not.
//
// Placeholders and escaped braces pass through untouched — mangling
// {name} would break the very interpolation this is meant to exercise.

// pseudoMap is the accent substitution table. Only characters whose
// accented form is unambiguously the same letter are mapped: the string
// must stay readable enough that a developer can spot the wrong message,
// which is the difference between a useful fixture and noise.
var pseudoMap = map[rune]rune{
	'a': 'à', 'b': 'ƀ', 'c': 'ç', 'd': 'ð', 'e': 'é', 'f': 'ƒ', 'g': 'ĝ',
	'h': 'ĥ', 'i': 'ï', 'j': 'ĵ', 'k': 'ķ', 'l': 'ĺ', 'm': 'ɱ', 'n': 'ñ',
	'o': 'ô', 'p': 'þ', 'q': 'ɋ', 'r': 'ŕ', 's': 'š', 't': 'ţ', 'u': 'ü',
	'v': 'ṽ', 'w': 'ŵ', 'x': 'ჯ', 'y': 'ý', 'z': 'ž',
	'A': 'Å', 'B': 'Ɓ', 'C': 'Ç', 'D': 'Ð', 'E': 'É', 'F': 'Ƒ', 'G': 'Ĝ',
	'H': 'Ĥ', 'I': 'Ï', 'J': 'Ĵ', 'K': 'Ķ', 'L': 'Ĺ', 'M': 'Ṁ', 'N': 'Ñ',
	'O': 'Ô', 'P': 'Þ', 'Q': 'Q', 'R': 'Ŕ', 'S': 'Š', 'T': 'Ţ', 'U': 'Ü',
	'V': 'Ṽ', 'W': 'Ŵ', 'X': 'Ẍ', 'Y': 'Ý', 'Z': 'Ž',
}

// padChar repeats to reach the target length. A distinctive character
// rather than a space so the padding is visibly padding, not a layout
// bug of its own.
const padChar = "·"

func pseudoEntry(e catalogEntry, expand float64) catalogEntry {
	if e.raw != "" {
		return e
	}
	if !e.isPlural() {
		return catalogEntry{text: pseudoString(e.text, expand)}
	}
	forms := make(map[string]string, len(e.forms))
	for cat, v := range e.forms {
		forms[cat] = pseudoString(v, expand)
	}
	return catalogEntry{forms: forms}
}

// pseudoString accents the letters, pads to the expansion factor, and
// wraps the result in bounding brackets.
func pseudoString(s string, expand float64) string {
	if s == "" {
		return ""
	}
	accented, letters := accentOutsidePlaceholders(s)

	var b strings.Builder
	b.WriteString("⟪")
	b.WriteString(accented)

	// Pad by the shortfall in LETTERS, not in total runes: padding a
	// string that is mostly placeholder markup by 40% of its source
	// length would produce absurd results for "{a} {b} {c}".
	if want := int(float64(letters) * (expand - 1)); want > 0 {
		b.WriteString(" ")
		b.WriteString(strings.Repeat(padChar, want))
	}
	b.WriteString("⟫")
	return b.String()
}

// accentOutsidePlaceholders maps letters to their accented forms while
// copying {placeholder} spans and {{escaped}} braces verbatim. Returns
// the result and the number of letters it accented (the basis for
// padding).
func accentOutsidePlaceholders(s string) (string, int) {
	var b strings.Builder
	b.Grow(len(s) * 2)
	letters := 0

	runes := []rune(s)
	for i := 0; i < len(runes); {
		r := runes[i]
		// Escaped braces: copy both runes, do not treat as a placeholder.
		if (r == '{' || r == '}') && i+1 < len(runes) && runes[i+1] == r {
			b.WriteRune(r)
			b.WriteRune(r)
			i += 2
			continue
		}
		if r == '{' {
			if end := indexRune(runes[i:], '}'); end >= 0 {
				b.WriteString(string(runes[i : i+end+1]))
				i += end + 1
				continue
			}
		}
		if mapped, ok := pseudoMap[r]; ok {
			b.WriteRune(mapped)
			letters++
		} else {
			b.WriteRune(r)
			if unicode.IsLetter(r) {
				letters++
			}
		}
		i++
	}
	return b.String(), letters
}

func indexRune(rs []rune, target rune) int {
	for i, r := range rs {
		if r == target {
			return i
		}
	}
	return -1
}
