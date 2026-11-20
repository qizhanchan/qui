package widgets

import (
	"fmt"
	"log"
	"os"
	"time"

	. "github.com/qizhanchan/qui"
)

// QUI_DEBUG_TILES=1 logs, once a second, what the tiled content cache
// (scroll_cache.go) did: frames it composited, why it was bypassed,
// recordings, tile rasters on the workers, rasters the UI thread waited
// for, and damage repainted in place.
var debugTiles = os.Getenv("QUI_DEBUG_TILES") == "1"

type tileDebug struct {
	start                 time.Time
	frames                int
	bypass                map[string]int
	records               int
	recordTime            time.Duration
	rasters               int
	rasterTime            time.Duration
	waits                 int
	waitTime              time.Duration
	inPlace, staleRasters int
	tiles                 int
}

var tileDbg = tileDebug{bypass: map[string]int{}}

func noteTileBypass(why string) {
	if debugTiles {
		tileDbg.bypass[why]++
		flushTileDebug()
	}
}

func flushTileDebug() {
	d := &tileDbg
	now := time.Now()
	if d.start.IsZero() {
		d.start = now
		return
	}
	if now.Sub(d.start) < time.Second {
		return
	}
	r := func(x time.Duration) time.Duration { return x.Round(100 * time.Microsecond) }
	log.Printf("qui/tiles: frames=%d tiles=%d records=%d in %v rasters=%d in %v (workers) waits=%d in %v inplace=%d stale=%d bypass=%s",
		d.frames, d.tiles, d.records, r(d.recordTime), d.rasters, r(d.rasterTime), d.waits, r(d.waitTime),
		d.inPlace, d.staleRasters, fmt.Sprint(d.bypass))
	*d = tileDebug{start: now, bypass: map[string]int{}}
}

var _ = Rect{}
