package htmlcss

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/qizhanchan/qui"
	"github.com/qizhanchan/qui/widgets"
)

// Marquee cards with text, shadows, gradients and rotation are recorded
// on the UI thread and rasterized on the worker pool (RFC 0004 R2): every
// card ends up with a texture, the ones that land after the frame that
// recorded them only cause a GPU redraw — the 2D frame is not repainted —
// and the race detector sees no shared state touched by playback.
func TestMarqueeWorkerRaster(t *testing.T) {
	var sb strings.Builder
	sb.WriteString(`<!doctype html><html><head><style>
@keyframes marquee { from { transform: translateX(0) } to { transform: translateX(-50%) } }
body { margin: 0; background: #fff; font: 14px sans-serif }
.clip { display: flex; overflow-x: clip }
.row { display: flex; padding: 20px 0; animation: marquee 20s linear infinite }
.card { min-width: 180px; margin: 0 10px; padding: 12px; border-radius: 14px;
  background: linear-gradient(135deg, #fde, #def); box-shadow: 0 4px 12px rgba(0,0,0,.25);
  transform: rotate(2deg) }
</style></head><body><div class=clip><div id=row class=row>`)
	for i := 0; i < 16; i++ {
		fmt.Fprintf(&sb, `<div class=card><b>Card %d</b><p>显示列表 · worker raster · %d</p></div>`, i, i*7)
	}
	sb.WriteString(`</div></div></body></html>`)

	prev := webCompat
	SetWebCompat(true)
	t.Cleanup(func() { SetWebCompat(prev) })
	win, err := qui.NewHeadlessWindowForTest(600, 300)
	if err != nil {
		t.Skip(err)
	}
	r := &overlayRenderer{CPURenderer: qui.NewCPURenderer()}
	win.SetRenderer(r)
	view := widgets.NewScrollView()
	view.FixedViewport = true
	view.OverlayBar = true
	view.PixelScroll = true
	win.SetRoot(view)
	rd := RenderDoc(sb.String(), "", Options{Window: win, ViewportWidth: 600, ViewportHeight: 300, DocumentRoot: true})
	view.SetContentAuto(rd.Root)
	row := rd.ByID["row"].(*El)
	step := func() { time.Sleep(20 * time.Millisecond); win.Step() }
	for i := 0; i < 4; i++ {
		step()
	}
	if !row.Box.GPULayerFollows() || r.fn == nil {
		t.Fatalf("the marquee row is not composited on the GPU")
	}
	// Let every scheduled raster land.
	deadline := time.Now().Add(5 * time.Second)
	for {
		step()
		n := len(r.replay())
		if n > 0 && row.Box.GPULayerPendingForTest() == 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("rasters still pending after 5s (%d textures placed)", n)
		}
	}
	seq := win.PaintSeq()
	for i := 0; i < 5; i++ {
		step()
	}
	if win.PaintSeq() != seq {
		t.Fatalf("animation frames repainted: %d -> %d", seq, win.PaintSeq())
	}
}
