package typeset

// Hyphenation is language-dependent and independent of font or width.
var hyphenPointsCache = newLRUCache[string, []int](4096, 512<<10)

func hyphenPoints(word, language string) []int {
	if language == "" {
		language = string(ShapingLanguage())
	}
	key := language + ":" + word
	if p, ok := hyphenPointsCache.Get(key); ok {
		return p
	}
	p := platformHyphenPoints(word, language)
	hyphenPointsCache.Put(key, p, len(key)+len(p)*8)
	return p
}
func splitHyphenatedTok(t inlineTok, available float32) (head, tail inlineTok, ok bool) {
	runes := []rune(t.text)
	for _, cut := range hyphenPoints(t.text, t.language) {
		text := string(runes[:cut]) + "-"
		width := MeasureRunes([]rune(text), t.font, nil) + t.padL
		if width > available {
			continue
		}
		head, tail = t, t
		head.text = text
		head.width = width
		head.padR = 0
		head.srcEnd = t.srcStart + cut
		tail.text = string(runes[cut:])
		tail.width = MeasureRunes(runes[cut:], t.font, nil) + t.padR
		tail.padL = 0
		tail.srcStart = head.srcEnd
		tail.spaceBefore = false
		return head, tail, true
	}
	return head, tail, false
}
