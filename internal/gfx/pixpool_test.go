package gfx

import (
	"image"
	"testing"
	"time"
)

func TestPixPoolReuseAndZero(t *testing.T) {
	b := GetPix(1000, false)
	if len(b) != 1000 || cap(b) != 1024 {
		t.Fatalf("len %d cap %d", len(b), cap(b))
	}
	for i := range b {
		b[i] = 7
	}
	PutPix(b)
	z := GetPix(900, true)
	for i, v := range z {
		if v != 0 {
			t.Fatalf("zeroed buffer has %d at %d", v, i)
		}
	}
	img := GetRGBA(image.Rect(3, 4, 13, 9), true)
	if img.Bounds() != image.Rect(3, 4, 13, 9) || len(img.Pix) != 4*10*5 || img.Stride != 40 {
		t.Fatalf("bad image %v %d %d", img.Bounds(), len(img.Pix), img.Stride)
	}
	PutRGBA(img)
	PutPix(make([]uint8, 1000)) // foreign buffer: ignored, must not panic
}

func TestPixPoolTrimsWhenIdle(t *testing.T) {
	old := pixPoolIdle
	pixPoolIdle = 20 * time.Millisecond
	defer func() { pixPoolIdle = old }()
	TrimPixPool()
	PutPix(GetPix(4096, false))
	if PixPoolBytes() == 0 {
		t.Fatal("returned buffer not pooled")
	}
	deadline := time.Now().Add(2 * time.Second)
	for PixPoolBytes() != 0 {
		if time.Now().After(deadline) {
			t.Fatalf("pool still holds %d bytes after idle", PixPoolBytes())
		}
		time.Sleep(5 * time.Millisecond)
	}
}
