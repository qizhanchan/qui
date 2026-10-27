package typeset

import (
	"container/list"
	"sync"
	"unicode/utf8"
	"unsafe"

	"github.com/go-text/typesetting/shaping"

	"github.com/qizhanchan/qui/internal/gfx"
)

// Shaped-line memo.
//
// DrawText shapes its string every frame (ShapeSingleLine), and right-
// aligned / measured cells shape it a second time in TextMetrics
// (MeasureRunes). Once glyph rasterisation is cached, that HarfBuzz +
// bidi + line-wrapper work is most of a text frame and most of its
// garbage. Shaping is pure in the inputs below for a fixed font registry,
// locale and emoji provider, so the results are memoised in a bounded LRU.
//
// Cached *ShapedLine values are shared: callers must treat them as
// immutable (every consumer only reads runs / glyphs). The cache stores
// its own copy of the runes, so a caller reusing its input slice (an
// editor's rune buffer) cannot change a cached line.

type shapeCacheKey struct {
	text          string
	font          fontCacheKey
	letterSpacing float32
	wordSpacing   float32
	smallCaps     bool
	direction     TextDirection
	localeGen     uint64
	fontGen       uint64
	emojiGen      uint64
}

// shapeCacheable reports whether text survives a string round trip
// unchanged (string(runes) replaces invalid runes with U+FFFD).
func shapeCacheable(text []rune) bool {
	for _, r := range text {
		if !utf8.ValidRune(r) {
			return false
		}
	}
	return true
}

func makeShapeCacheKey(text []rune, spec gfx.Font, direction TextDirection) shapeCacheKey {
	if spec.Size <= 0 {
		spec.Size = 14
	}
	return shapeCacheKey{
		text:          string(text),
		font:          makeFontCacheKey(spec),
		letterSpacing: spec.LetterSpacing,
		wordSpacing:   spec.WordSpacing,
		smallCaps:     spec.SmallCaps,
		direction:     direction,
		localeGen:     typesetLocaleGen(),
		fontGen:       FontRegistryGeneration(),
		emojiGen:      emojiGeneration.Load(),
	}
}

// lruCache is a small mutex-guarded LRU bounded by entry count and an
// approximate byte cost.
type lruCache[K comparable, V any] struct {
	mu         sync.Mutex
	ll         *list.List
	m          map[K]*list.Element
	Bytes      int
	maxBytes   int
	maxEntries int
}

type lruEntry[K comparable, V any] struct {
	key  K
	val  V
	cost int
}

func newLRUCache[K comparable, V any](maxEntries, maxBytes int) *lruCache[K, V] {
	return &lruCache[K, V]{ll: list.New(), m: map[K]*list.Element{}, maxBytes: maxBytes, maxEntries: maxEntries}
}

func (c *lruCache[K, V]) Get(k K) (V, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if e, ok := c.m[k]; ok {
		c.ll.MoveToFront(e)
		return e.Value.(*lruEntry[K, V]).val, true
	}
	var zero V
	return zero, false
}

func (c *lruCache[K, V]) Put(k K, v V, cost int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if e, ok := c.m[k]; ok {
		c.ll.MoveToFront(e)
		return
	}
	c.m[k] = c.ll.PushFront(&lruEntry[K, V]{key: k, val: v, cost: cost})
	c.Bytes += cost
	for (c.Bytes > c.maxBytes || c.ll.Len() > c.maxEntries) && c.ll.Len() > 0 {
		e := c.ll.Back()
		old := e.Value.(*lruEntry[K, V])
		c.ll.Remove(e)
		delete(c.m, old.key)
		c.Bytes -= old.cost
	}
}

func (c *lruCache[K, V]) reset() {
	c.mu.Lock()
	c.ll.Init()
	c.m = map[K]*list.Element{}
	c.Bytes = 0
	c.mu.Unlock()
}

func (c *lruCache[K, V]) Len() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.ll.Len()
}

type shapedLineResult struct {
	line *ShapedLine
	ok   bool
}

type paragraphAdvanceResult struct {
	advance float32
	ok      bool
}

const (
	shapeCacheMaxEntries = 4096
	shapeCacheMaxBytes   = 8 << 20
	shapeCacheEntryCost  = 160 // key, list element, map slot
)

var (
	ShapedLineCache       = newLRUCache[shapeCacheKey, shapedLineResult](shapeCacheMaxEntries, shapeCacheMaxBytes)
	paragraphAdvanceCache = newLRUCache[shapeCacheKey, paragraphAdvanceResult](shapeCacheMaxEntries, shapeCacheMaxBytes/8)
)

func invalidateShapeCaches() {
	ShapedLineCache.reset()
	paragraphAdvanceCache.reset()
}

func shapedLineCost(key *shapeCacheKey, line *ShapedLine) int {
	cost := shapeCacheEntryCost + 2*len(key.text)
	if line == nil {
		return cost
	}
	cost += int(unsafe.Sizeof(*line)) + 4*(len(line.Text)+len(line.shapeText))
	for i := range line.Runs {
		cost += int(unsafe.Sizeof(line.Runs[i])) + len(line.Runs[i].Glyphs)*int(unsafe.Sizeof(shaping.Glyph{}))
	}
	return cost
}

// ShapeSingleLine shapes text as one unwrapped line, memoised.
func ShapeSingleLine(text []rune, fontSpec gfx.Font, direction TextDirection) (*ShapedLine, bool) {
	if !shapeCacheable(text) {
		return shapeSingleLineUncached(text, fontSpec, direction)
	}
	key := makeShapeCacheKey(text, fontSpec, direction)
	if r, ok := ShapedLineCache.Get(key); ok {
		return r.line, r.ok
	}
	// Shape an owned copy: the cached line aliases the runes it was
	// shaped from.
	line, ok := shapeSingleLineUncached([]rune(key.text), fontSpec, direction)
	ShapedLineCache.Put(key, shapedLineResult{line: line, ok: ok}, shapedLineCost(&key, line))
	return line, ok
}

// paragraphAdvance is ShapeParagraph(text, fontSpec, TextDirectionAuto)'s
// advance, memoised (MeasureRunes).
func paragraphAdvance(text []rune, fontSpec gfx.Font) (float32, bool) {
	if !shapeCacheable(text) {
		paragraph, ok := ShapeParagraph(text, fontSpec, TextDirectionAuto)
		return paragraph.Advance, ok
	}
	key := makeShapeCacheKey(text, fontSpec, TextDirectionAuto)
	if r, ok := paragraphAdvanceCache.Get(key); ok {
		return r.advance, r.ok
	}
	paragraph, ok := ShapeParagraph(text, fontSpec, TextDirectionAuto)
	paragraphAdvanceCache.Put(key, paragraphAdvanceResult{advance: paragraph.Advance, ok: ok}, shapeCacheEntryCost+2*len(key.text))
	return paragraph.Advance, ok
}
