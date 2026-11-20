package widgets

import (
	"fmt"
	"log"
	"math"
	"os"
	"sort"
	"strings"
	"sync"
	"time"
)

// QUI_DEBUG_GPU_LAYERS=1 logs, once a second, how evenly the GPU layers
// moved: overlay draws, the spread of intervals between them, and the
// histogram of per-draw steps of the first layer, in device pixels.
var debugGPULayers = os.Getenv("QUI_DEBUG_GPU_LAYERS") == "1"

type gpuLayerDebug struct {
	start, last   time.Time
	gaps          []time.Duration
	steps         map[float32]int
	lastX         float32
	haveX         bool
	captures      int
	rasters       int           // child bitmaps rendered (on raster workers)
	rasterTime    time.Duration // spent rendering them
	records       int           // child pictures recorded (UI thread)
	recordTime    time.Duration // spent recording them
	waits         int           // rasters the UI thread waited for
	gpuRasters    int           // child pictures rasterized on the GPU
	gpuRasterTime time.Duration // spent encoding them
	waitTime      time.Duration // spent waiting
}

var gpuDbg = gpuLayerDebug{steps: map[float32]int{}}

// gpuDbgMu: layer draws are noted on the compositor thread too.
var gpuDbgMu sync.Mutex

func noteGPULayerDraw(x float32) {
	gpuDbgMu.Lock()
	defer gpuDbgMu.Unlock()
	d := &gpuDbg
	now := time.Now()
	if !d.last.IsZero() {
		d.gaps = append(d.gaps, now.Sub(d.last))
	}
	d.last = now
	if d.haveX {
		d.steps[float32(math.Round(float64(x-d.lastX)*4)/4)]++ // quarter pixels
	}
	d.lastX, d.haveX = x, true
	if d.start.IsZero() {
		d.start = now
	}
	if now.Sub(d.start) < time.Second {
		return
	}
	sort.Slice(d.gaps, func(i, j int) bool { return d.gaps[i] < d.gaps[j] })
	pct := func(p float64) time.Duration {
		if len(d.gaps) == 0 {
			return 0
		}
		return d.gaps[int(p*float64(len(d.gaps)-1))].Round(100 * time.Microsecond)
	}
	keys := make([]float32, 0, len(d.steps))
	for k := range d.steps {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool { return keys[i] < keys[j] })
	var b strings.Builder
	for _, k := range keys {
		fmt.Fprintf(&b, " %.2fpx:%d", k, d.steps[k])
	}
	log.Printf("qui/gpu-layers: records=%d in %v gpu-rasters=%d in %v rasters=%d in %v (workers) waits=%d in %v draws=%d captures=%d gap p0=%v p50=%v p90=%v max=%v steps:%s",
		d.records, d.recordTime.Round(100*time.Microsecond), d.gpuRasters, d.gpuRasterTime.Round(100*time.Microsecond), d.rasters, d.rasterTime.Round(100*time.Microsecond),
		d.waits, d.waitTime.Round(100*time.Microsecond),
		len(d.gaps)+1, d.captures, pct(0), pct(0.5), pct(0.9), pct(1), b.String())
	*d = gpuLayerDebug{steps: map[float32]int{}, last: d.last, lastX: d.lastX, haveX: d.haveX}
}

var gpuFallbackSeen = map[string]bool{}

// gpuLayerFallback logs, once per distinct reason, why layers fell back to
// the CPU path.
func gpuLayerFallback(reason string) {
	if debugGPULayers && !gpuFallbackSeen[reason] {
		gpuFallbackSeen[reason] = true
		log.Printf("qui/gpu-layers: fallback to CPU: %s", reason)
	}
}
