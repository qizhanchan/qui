package qui

import (
	"fmt"
	"log"
	"os"
)

// Measure caching.
//
// Layout engines measure a child several times per pass — the flex
// algorithm alone asks for a natural size, then a cross-axis size, then
// the container's own Measure asks again — and every level of nesting
// multiplies the count. A relayout of a modest htmlcss page made ~38k
// Measure calls for ~1.6k distinct (widget, constraint) pairs.
//
// MeasureChild memoizes a widget's Measure per available size. An entry
// stays valid until something in the widget's subtree invalidates layout
// (markLayoutDirty clears the cache of every widget on the way to the
// root) or a global input to measurement changes (theme, fonts, locale, a
// window resize or Window.InvalidateLayout — see measureEpoch). So one
// text change re-measures only its ancestor chain; every clean sibling
// subtree answers from its cache.
//
// The contract this relies on is the one InvalidateLayout always had: a
// widget whose size-affecting state changes calls InvalidateLayout on
// ITSELF (SetStyle, SetText and AddChild already do). Changing a child
// and invalidating only an ancestor leaves the child's cached size stale;
// use Window.InvalidateLayout for "re-measure everything".
//
// QUI_DEBUG_LAYOUT_CACHE=1 re-measures on every cache hit and logs any
// widget whose cached size disagrees — the way to find one that changes
// size without invalidating.

var layoutCacheVerify = os.Getenv("QUI_DEBUG_LAYOUT_CACHE") == "1"

// measureCacheSize bounds the per-widget entries. A widget is measured
// under a handful of distinct constraints per pass (natural, cross-axis
// stretch, the final slot); more than this and the oldest is replaced.
const measureCacheSize = 6

// layoutEpoch is bumped for changes that can alter any widget's measured
// size without passing through its own InvalidateLayout.
var layoutEpoch uint64

// invalidateAllMeasures discards every cached measurement in the process.
func invalidateAllMeasures() { layoutEpoch++ }

type measureEpoch struct{ layout, theme, font, locale uint64 }

func currentMeasureEpoch() measureEpoch {
	return measureEpoch{layoutEpoch, ThemeGeneration(), FontRegistryGeneration(), LocaleGeneration()}
}

type measureEntry struct{ avail, size Size }

// measureCache is a widget's memo of Measure results. version changes on
// every invalidation, so a Measure that was in flight when its subtree
// was invalidated does not store a result computed from the old state.
type measureCache struct {
	epoch   measureEpoch
	version uint64
	entries []measureEntry
	next    int // ring position once entries is full
}

func (c *measureCache) invalidate() {
	c.version++
	c.entries = c.entries[:0]
	c.next = 0
}

func (c *measureCache) lookup(avail Size, epoch measureEpoch) (Size, bool) {
	if c.epoch != epoch {
		c.epoch = epoch
		c.entries = c.entries[:0]
		c.next = 0
		return Size{}, false
	}
	for _, e := range c.entries {
		if e.avail == avail {
			return e.size, true
		}
	}
	return Size{}, false
}

func (c *measureCache) store(avail, size Size) {
	if len(c.entries) < measureCacheSize {
		c.entries = append(c.entries, measureEntry{avail, size})
		return
	}
	c.entries[c.next] = measureEntry{avail, size}
	c.next = (c.next + 1) % measureCacheSize
}

// measureCacher is satisfied by every widget embedding BaseWidget.
type measureCacher interface {
	measureCacheRef() *measureCache
}

func (b *BaseWidget) measureCacheRef() *measureCache { return &b.measureMemo }

// MeasureChild returns w.Measure(avail), reusing the result of an earlier
// call with the same avail while nothing in w's subtree has invalidated
// layout since. Layout engines and containers measure their children
// through this; calling Measure directly still works but always recomputes.
func MeasureChild(w Widget, avail Size) Size {
	mc, ok := w.(measureCacher)
	if !ok {
		return w.Measure(avail)
	}
	c := mc.measureCacheRef()
	epoch := currentMeasureEpoch()
	if size, hit := c.lookup(avail, epoch); hit {
		if layoutCacheVerify {
			verifyMeasure(w, avail, size)
		}
		return size
	}
	version := c.version
	size := w.Measure(avail)
	// Only keep a result nothing invalidated while it was being computed
	// (a descendant's Measure may adjust state and InvalidateLayout).
	if c.version == version && c.epoch == currentMeasureEpoch() {
		c.store(avail, size)
	}
	return size
}

// verifiedStale remembers which widget types already reported a stale
// cached size, so the log names each culprit once.
var verifiedStale = map[string]bool{}

func verifyMeasure(w Widget, avail, cached Size) {
	fresh := w.Measure(avail)
	if fresh == cached {
		return
	}
	key := fmt.Sprintf("%T", w)
	if verifiedStale[key] {
		return
	}
	verifiedStale[key] = true
	id := ""
	if b, ok := w.(interface{ ID() string }); ok {
		id = b.ID()
	}
	log.Printf("qui/layoutcache: STALE %s id=%q avail=%v cached=%v fresh=%v — it changed size without InvalidateLayout on itself", key, id, avail, cached, fresh)
}
