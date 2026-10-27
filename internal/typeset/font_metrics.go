package typeset

import (
	"sync"

	"golang.org/x/image/font"

	"github.com/qizhanchan/qui/internal/gfx"
)

// Font metrics cache.
//
// x/image's opentype.Face.Metrics() is not read-only: the first call
// fills the face's metrics fields (through its shared sfnt scratch
// buffer) without synchronisation. Faces are cached and shared process-
// wide (GetFontFacesFor), so two goroutines rendering offscreen at once —
// an export beside the UI thread, parallel snapshot renders — raced on a
// freshly built face (go test -race). FontMetricsFor serialises the
// underlying call and memoises the result per font spec.
//
// Entries are stamped with FontRegistryGeneration(), so a registry change
// (which swaps the faces) never serves metrics of the previous face, and
// ClearFontFaceCachesUnlocked drops the map outright.

type fontMetricsEntry struct {
	gen uint64
	m   font.Metrics
}

const fontMetricsCacheLimit = 1024

var (
	fontMetricsMu    sync.RWMutex
	fontMetricsCache = map[fontCacheKey]fontMetricsEntry{}
	// faceMetricsCallMu serialises Face.Metrics() calls made through
	// FontMetricsFor (cache misses).
	faceMetricsCallMu sync.Mutex
)

// FontMetricsFor returns GetFontFaceFor(spec).Metrics(), cached and safe
// for concurrent use.
func FontMetricsFor(spec gfx.Font) font.Metrics {
	if spec.Size <= 0 {
		spec.Size = 14
	}
	key := makeFontCacheKey(spec)
	gen := FontRegistryGeneration()
	fontMetricsMu.RLock()
	e, ok := fontMetricsCache[key]
	fontMetricsMu.RUnlock()
	if ok && e.gen == gen {
		return e.m
	}
	face := GetFontFaceFor(spec)
	faceMetricsCallMu.Lock()
	m := face.Metrics()
	faceMetricsCallMu.Unlock()
	fontMetricsMu.Lock()
	if len(fontMetricsCache) >= fontMetricsCacheLimit {
		fontMetricsCache = map[fontCacheKey]fontMetricsEntry{}
	}
	fontMetricsCache[key] = fontMetricsEntry{gen: gen, m: m}
	fontMetricsMu.Unlock()
	return m
}

func invalidateFontMetricsCache() {
	fontMetricsMu.Lock()
	fontMetricsCache = map[fontCacheKey]fontMetricsEntry{}
	fontMetricsMu.Unlock()
}
