package render

import (
	"fmt"
	"log"
	"os"
	"runtime/metrics"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/qizhanchan/qui/internal/gfx"
	"github.com/qizhanchan/qui/internal/wgpu"
)

// QUI_DEBUG_MEM=1 logs, every two seconds, where the renderer's memory
// is: the Go heap (live, in use, idle but not returned), each cache's
// bytes, and the live GPU textures by label. Other packages add their
// caches with RegisterMemSource. Compare the total with the process
// footprint (`footprint PID`, `vmmap --summary PID`) to see what is not
// accounted for (Dawn / Metal internals, IOSurfaces, malloc).
var debugMem = os.Getenv("QUI_DEBUG_MEM") == "1"

// MemSource reports one cache's bytes and entries. It is called from a
// background goroutine: it must be safe for that.
type MemSource func() (bytes int64, entries int)

var memSources = struct {
	sync.Mutex
	names []string
	fns   map[string]MemSource
}{fns: map[string]MemSource{}}

// RegisterMemSource adds (or replaces) a cache to the QUI_DEBUG_MEM
// report.
func RegisterMemSource(name string, f MemSource) {
	memSources.Lock()
	defer memSources.Unlock()
	if _, ok := memSources.fns[name]; !ok {
		memSources.names = append(memSources.names, name)
	}
	memSources.fns[name] = f
}

// MemStat is one line of MemReport.
type MemStat struct {
	Name    string
	Bytes   int64
	Entries int
}

// MemReport lists the registered caches' bytes, largest first.
func MemReport() []MemStat {
	memSources.Lock()
	names := append([]string(nil), memSources.names...)
	fns := make([]MemSource, len(names))
	for i, n := range names {
		fns[i] = memSources.fns[n]
	}
	memSources.Unlock()
	out := make([]MemStat, 0, len(names))
	for i, f := range fns {
		b, n := f()
		out = append(out, MemStat{Name: names[i], Bytes: b, Entries: n})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Bytes > out[j].Bytes })
	return out
}

func init() {
	RegisterMemSource("shadow-masks", func() (int64, int) {
		shadowMasks.Lock()
		defer shadowMasks.Unlock()
		return int64(shadowMasks.bytes), shadowMasks.lru.Len()
	})
	RegisterMemSource("image-scale", func() (int64, int) {
		scaledCache.Lock()
		defer scaledCache.Unlock()
		return int64(scaledCache.bytes), scaledCache.lru.Len()
	})
	RegisterMemSource("glyph-masks", func() (int64, int) {
		n, b := glyphCache.Stats()
		return int64(b), n
	})
	RegisterMemSource("pix-pool", func() (int64, int) { return int64(gfx.PixPoolBytes()), 0 })
	if debugMem {
		go memDebugLoop()
	}
}

func memDebugLoop() {
	samples := []metrics.Sample{
		{Name: "/gc/heap/live:bytes"},
		{Name: "/memory/classes/heap/objects:bytes"},
		{Name: "/memory/classes/heap/unused:bytes"},
		{Name: "/memory/classes/heap/free:bytes"},
		{Name: "/memory/classes/heap/released:bytes"},
		{Name: "/memory/classes/total:bytes"},
	}
	mb := func(b int64) string { return fmt.Sprintf("%.1fM", float64(b)/(1<<20)) }
	var lastUp int64
	for range time.Tick(2 * time.Second) {
		metrics.Read(samples)
		v := func(i int) int64 { return int64(samples[i].Value.Uint64()) }
		var sb strings.Builder
		fmt.Fprintf(&sb, "qui/mem: go live=%s objects=%s frag=%s idle=%s released=%s rss~%s |",
			mb(v(0)), mb(v(1)), mb(v(2)), mb(v(3)), mb(v(4)), mb(v(5)-v(4)))
		for _, s := range MemReport() {
			if s.Bytes > 0 {
				fmt.Fprintf(&sb, " %s=%s/%d", s.Name, mb(s.Bytes), s.Entries)
			}
		}
		sb.WriteString(" | tex:")
		var tex int64
		for _, t := range wgpu.TextureMemory() {
			tex += t.Bytes
			label := t.Label
			if label == "" {
				label = "?"
			}
			fmt.Fprintf(&sb, " %q=%s/%d", label, mb(t.Bytes), t.Count)
		}
		up := wgpu.UploadedBytes()
		fmt.Fprintf(&sb, " total=%s uploads=%s/2s", mb(tex), mb(up-lastUp))
		lastUp = up
		log.Print(sb.String())
	}
}
