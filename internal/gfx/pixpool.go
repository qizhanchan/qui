package gfx

import (
	"image"
	"math/bits"
	"sync"
	"time"
)

// Pixel buffer pool for per-frame scratch: layer images, blur passes,
// shadow masks. A scroll repaints the same few regions every frame; fresh
// allocations for them were most of the GC pressure while scrolling.
// Buffers are bucketed by power-of-two capacity and kept on free lists
// the GC does not clear (a sync.Pool is emptied every cycle, and under a
// memory limit cycles come often enough that it never hit); the lists hold
// at most pixPoolBudget bytes in all, so the pool pins a bounded amount.
// Once nothing has been taken or returned for pixPoolIdle (the window went
// idle) the lists are dropped, so a page at rest does not keep its scroll
// scratch.

const (
	maxPooledPix  = 64 << 20
	pixPoolBudget = 24 << 20
)

var pixPoolIdle = 2 * time.Second // var for tests

var pixPool struct {
	sync.Mutex
	free    [bits.UintSize][][]uint8
	bytes   int
	lastUse time.Time
	timer   *time.Timer
	armed   bool
}

// pixTouch notes pool activity and arms the idle trim. Called locked.
func pixTouch() {
	pixPool.lastUse = time.Now()
	if pixPool.bytes == 0 || pixPool.armed {
		return
	}
	pixPool.armed = true
	if pixPool.timer == nil {
		pixPool.timer = time.AfterFunc(pixPoolIdle, pixIdleTrim)
	} else {
		pixPool.timer.Reset(pixPoolIdle)
	}
}

func pixIdleTrim() {
	pixPool.Lock()
	defer pixPool.Unlock()
	if rest := pixPoolIdle - time.Since(pixPool.lastUse); rest > 0 {
		pixPool.timer.Reset(rest)
		return
	}
	pixPool.armed = false
	trimPixLocked()
}

func trimPixLocked() {
	for k := range pixPool.free {
		clear(pixPool.free[k])
		pixPool.free[k] = nil
	}
	pixPool.bytes = 0
}

// TrimPixPool drops every pooled buffer.
func TrimPixPool() {
	pixPool.Lock()
	trimPixLocked()
	pixPool.Unlock()
}

// PixPoolBytes reports the bytes the pool holds (diagnostics, tests).
func PixPoolBytes() int {
	pixPool.Lock()
	defer pixPool.Unlock()
	return pixPool.bytes
}

func pixBucket(n int) int { return bits.Len(uint(n - 1)) }

// GetPix returns a byte slice of length n. Its contents are undefined
// unless zero is set.
func GetPix(n int, zero bool) []uint8 {
	if n <= 0 {
		return nil
	}
	if n <= maxPooledPix {
		k := pixBucket(n)
		pixPool.Lock()
		pixTouch()
		if l := len(pixPool.free[k]); l > 0 {
			b := pixPool.free[k][l-1]
			pixPool.free[k][l-1] = nil
			pixPool.free[k] = pixPool.free[k][:l-1]
			pixPool.bytes -= cap(b)
			pixPool.Unlock()
			b = b[:n]
			if zero {
				clear(b)
			}
			return b
		}
		pixPool.Unlock()
		return make([]uint8, n, 1<<k)
	}
	return make([]uint8, n)
}

// PutPix returns b to the pool. The caller must not use b afterwards.
func PutPix(b []uint8) {
	c := cap(b)
	if c == 0 || c > maxPooledPix || c&(c-1) != 0 {
		return // not one of ours (or not a power of two): let GC take it
	}
	pixPool.Lock()
	if pixPool.bytes+c <= pixPoolBudget {
		k := pixBucket(c)
		pixPool.free[k] = append(pixPool.free[k], b[:c])
		pixPool.bytes += c
	}
	pixTouch()
	pixPool.Unlock()
}

// GetRGBA returns an RGBA image over r backed by a pooled buffer;
// transparent when zero is set, undefined otherwise.
func GetRGBA(r image.Rectangle, zero bool) *image.RGBA {
	w, h := r.Dx(), r.Dy()
	if w <= 0 || h <= 0 {
		return image.NewRGBA(r)
	}
	return &image.RGBA{Pix: GetPix(4*w*h, zero), Stride: 4 * w, Rect: r}
}

// PutRGBA returns img's buffer to the pool. The caller must not use img
// afterwards.
func PutRGBA(img *image.RGBA) {
	if img != nil {
		PutPix(img.Pix)
	}
}
