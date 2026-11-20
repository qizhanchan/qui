package widgets

import (
	"os"
	"sync"

	. "github.com/qizhanchan/qui"
)

// QUI_DEBUG_MEM=1 (qui/internal/render/mem_debug.go) also reports the
// widgets' caches: content tiles and their recordings, and animated
// layers' child bitmaps. Owners publish their totals from the UI thread
// (memNote); the report sums them from its goroutine.
var debugMem = os.Getenv("QUI_DEBUG_MEM") == "1"

type memCounts struct{ bytes, entries int64 }

var memOwners = struct {
	sync.Mutex
	by map[string]map[any]memCounts
}{by: map[string]map[any]memCounts{}}

// memNote records what owner holds in the named cache (zero: nothing).
func memNote(name string, owner any, bytes, entries int) {
	if !debugMem {
		return
	}
	memOwners.Lock()
	defer memOwners.Unlock()
	m := memOwners.by[name]
	if m == nil {
		m = map[any]memCounts{}
		memOwners.by[name] = m
		RegisterMemSource(name, func() (int64, int) {
			memOwners.Lock()
			defer memOwners.Unlock()
			var b, n int64
			for _, c := range memOwners.by[name] {
				b += c.bytes
				n += c.entries
			}
			return b, int(n)
		})
	}
	if bytes == 0 && entries == 0 {
		delete(m, owner)
		return
	}
	m[owner] = memCounts{int64(bytes), int64(entries)}
}
