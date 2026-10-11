package h264enc

import (
	"bytes"
	"math/rand"
	"testing"

	"github.com/qizhanchan/qui/media/internal/h264"
)

// TestQpelMatchesMC pins the half-plane quarter-sample predictions used by
// motion search to the decoder's motion compensation.
func TestQpelMatchesMC(t *testing.T) {
	frames := sourceFrames(t, "vt_main_p8x8.264")
	e, err := New(Config{Width: 160, Height: 96, QP: 20})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.Encode(frames[0]); err != nil {
		t.Fatal(err)
	}
	p := e.refs[0]
	mc := h264.NewMC()
	pw, ph := e.mbW*16+2*lumaPad, e.mbH*16+2*lumaPad
	rng := rand.New(rand.NewSource(1))
	var buf, want [256]byte
	for i := 0; i < 5000; i++ {
		w, h := 16, 16
		if i&1 == 1 {
			w, h = 8, 8
		}
		x0, y0 := rng.Intn(e.mbW*16-w+1), rng.Intn(e.mbH*16-h+1)
		mx, my := rng.Intn(4*80)-160, rng.Intn(4*80)-160
		got, gs := p.qpel(buf[:], x0, y0, mx, my, w, h)
		mc.Luma(want[:], 16, p.y, p.ys, pw, ph, x0+lumaPad, y0+lumaPad, mx, my, w, h)
		for y := 0; y < h; y++ {
			if !bytes.Equal(got[y*gs:y*gs+w], want[y*16:y*16+w]) {
				t.Fatalf("block %dx%d at (%d,%d) mv (%d,%d) row %d differs", w, h, x0, y0, mx, my, y)
			}
		}
	}
}
