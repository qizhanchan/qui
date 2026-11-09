package qui

import (
	"bytes"
	"testing"
)

// stripes is a widget whose content is horizontal bands scrolled by off:
// band n covers content y in [10n, 10n+10) and has a color derived from n,
// so any pixel that ends up in the wrong place shows.
type stripes struct {
	BaseWidget
	off float32
}

func (s *stripes) Draw(c Canvas) {
	b := s.Bounds()
	first := int(s.off / 10)
	for n := first; ; n++ {
		y := b.Y + float32(n)*10 - s.off
		if y >= b.Y+b.H {
			break
		}
		col := Color{R: float32(n%7) / 7, G: float32(n%5) / 5, B: float32(n%3) / 3, A: 1}
		c.FillRect(Rect{X: b.X, Y: y, W: b.W, H: 10}, col)
	}
}

func stripesWindow(t *testing.T) (*Window, *stripes, *CPURenderer) {
	t.Helper()
	f := newFakePlatformWindow() // 800x600 logical, 2x
	w := newWindowOnFake(f)
	w.initUIThreadOwnership()
	r := NewCPURenderer()
	w.SetRenderer(r)
	s := &stripes{}
	s.SetSelf(s)
	w.SetRoot(s)
	w.Invalidate()
	w.Step()
	return w, s, r
}

func TestScrollPixelsMatchesFullRepaint(t *testing.T) {
	for _, dy := range []float32{-30, 30, -7.5} { // 7.5 logical = 15 physical px
		w, s, r := stripesWindow(t)
		if !w.ScrollPixels(s.Bounds(), 0, dy) {
			t.Fatalf("dy=%v: ScrollPixels declined", dy)
		}
		s.off -= dy
		if s.off < 0 {
			// keep the content valid for the reference paint
			s.off += 1000
			w.Invalidate()
		}
		w.Step()
		got := r.Snapshot()
		fr := w.RecentFrames(1)
		if dy == -30 && (len(fr) != 1 || fr[0].Dirty >= fr[0].Full/4) {
			t.Fatalf("dy=%v: frame repainted %d of %d px; want only the exposed strip", dy, fr[0].Dirty, fr[0].Full)
		}

		// Reference: the same state painted from scratch.
		w2, s2, r2 := stripesWindow(t)
		s2.off = s.off
		w2.Invalidate()
		w2.Step()
		want := r2.Snapshot()
		if !bytes.Equal(got.Pix, want.Pix) {
			t.Fatalf("dy=%v: scrolled frame differs from a full repaint", dy)
		}
	}
}

func TestScrollPixelsDeclinesFractionalPixels(t *testing.T) {
	w, s, _ := stripesWindow(t)
	if w.ScrollPixels(s.Bounds(), 0, 0.3) { // 0.6 physical px
		t.Fatal("a sub-pixel move must be declined")
	}
	if w.ScrollPixels(s.Bounds(), 0, 600) {
		t.Fatal("a move past the whole rect must be declined")
	}
}

func TestScrollPixelsCarriesPendingDamage(t *testing.T) {
	w, s, r := stripesWindow(t)
	// Damage at content rows 100..110 that has not been painted yet.
	s.off = 0
	w.InvalidateRect(Rect{X: 0, Y: 100, W: 800, H: 10})
	if !w.ScrollPixels(s.Bounds(), 0, -40) {
		t.Fatal("declined")
	}
	s.off = 40
	w.Step()
	got := r.Snapshot()
	w2, s2, r2 := stripesWindow(t)
	s2.off = 40
	w2.Invalidate()
	w2.Step()
	if !bytes.Equal(got.Pix, r2.Snapshot().Pix) {
		t.Fatal("pending damage was not carried along with the move")
	}
}

func TestDirtyRectsPaintSeparately(t *testing.T) {
	w, _, _ := stripesWindow(t)
	w.InvalidateRect(Rect{X: 0, Y: 0, W: 800, H: 20})
	w.InvalidateRect(Rect{X: 0, Y: 580, W: 800, H: 20})
	w.Step()
	fr := w.RecentFrames(1)
	if len(fr) != 1 {
		t.Fatal("no frame recorded")
	}
	// Two 800x20 strips at 2x: 2*1600*40 px, not the 800x600 bounding box.
	if fr[0].Dirty > 2*1600*44 {
		t.Fatalf("repainted %d px, want only the two strips", fr[0].Dirty)
	}
}
