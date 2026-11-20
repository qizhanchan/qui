package wgpu

import (
	"sort"
	"sync"
	"sync/atomic"
)

var uploaded atomic.Int64

func noteUpload(n int64) { uploaded.Add(n) }

// UploadedBytes is the total texture data written through the queue
// (Dawn stages each write in a buffer).
func UploadedBytes() int64 { return uploaded.Load() }

// Texture memory accounting (QUI_DEBUG_MEM): the bytes of the textures
// this process created and has not destroyed or released, by label. An
// estimate — mip chains count their first level only, and a released
// texture still referenced by a pending command or view counts as gone.

var texMem struct {
	sync.Mutex
	bytes map[string]int64
	count map[string]int
}

func noteTexture(label string, n int64, d int) {
	texMem.Lock()
	if texMem.bytes == nil {
		texMem.bytes, texMem.count = map[string]int64{}, map[string]int{}
	}
	texMem.bytes[label] += n * int64(d)
	texMem.count[label] += d
	texMem.Unlock()
}

// TextureMemStat is the live textures under one label.
type TextureMemStat struct {
	Label string
	Bytes int64
	Count int
}

// TextureMemory lists the live textures' bytes by label, largest first.
func TextureMemory() []TextureMemStat {
	texMem.Lock()
	out := make([]TextureMemStat, 0, len(texMem.bytes))
	for l, b := range texMem.bytes {
		if texMem.count[l] != 0 || b != 0 {
			out = append(out, TextureMemStat{Label: l, Bytes: b, Count: texMem.count[l]})
		}
	}
	texMem.Unlock()
	sort.Slice(out, func(i, j int) bool { return out[i].Bytes > out[j].Bytes })
	return out
}
