package gfx

import (
	"image"
	"math/bits"
	"sync"
)

// Pixel buffer pool for per-frame scratch: layer images, blur passes,
// shadow masks. A scroll repaints the same few regions every frame; fresh
// allocations for them were most of the GC pressure while scrolling.
// Buffers are bucketed by power-of-two capacity and kept on free lists
// the GC does not clear (a sync.Pool is emptied every cycle, and under a
// memory limit cycles come often enough that it never hit); the lists hold
// at most pixPoolBudget bytes in all, so the pool pins a bounded amount.

const (
	maxPooledPix  = 64 << 20
	pixPoolBudget = 48 << 20
)

var pixPool struct {
	sync.Mutex
	free  [bits.UintSize][][]uint8
	bytes int
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
