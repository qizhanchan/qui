//go:build darwin && cgo

package media

import "testing"

// TestPushCVPixelBufferRetainBalance checks the CF reference discipline
// on real IOSurface-backed buffers: a push takes one reference, a frame
// replaced before drawing gives it back, and Destroy returns the rest.
func TestPushCVPixelBufferRetainBalance(t *testing.T) {
	a := newTestPixelBuffer(64, 32)
	b := newTestPixelBuffer(64, 32)
	if a == nil || b == nil {
		t.Skip("CVPixelBufferCreate unavailable")
	}
	defer cfRelease(a)
	defer cfRelease(b)
	base := cfRetainCount(a)

	v := NewFrameView()
	if err := v.PushCVPixelBuffer(a); err != nil {
		t.Fatal(err)
	}
	if got := cfRetainCount(a); got != base+1 {
		t.Fatalf("push: retain count %d, want %d", got, base+1)
	}
	if w, h := v.FrameSize(); w != 64 || h != 32 {
		t.Fatalf("FrameSize = %dx%d", w, h)
	}
	if err := v.PushCVPixelBuffer(b); err != nil {
		t.Fatal(err)
	}
	if got := cfRetainCount(a); got != base {
		t.Fatalf("replaced frame not released: retain count %d, want %d", got, base)
	}
	v.promote()
	if got := cfRetainCount(b); got != base+1 {
		t.Fatalf("current frame: retain count %d, want %d", got, base+1)
	}
	v.Destroy()
	if got := cfRetainCount(b); got != base {
		t.Fatalf("Destroy: retain count %d, want %d", got, base)
	}
	if st := v.Stats(); st.Pushed != 2 || st.Dropped != 1 {
		t.Fatalf("stats = %+v", st)
	}
}

func TestPushCVPixelBufferRejectsNil(t *testing.T) {
	if err := NewFrameView().PushCVPixelBuffer(nil); err == nil {
		t.Fatal("nil buffer accepted")
	}
}
