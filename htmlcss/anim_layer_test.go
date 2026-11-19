package htmlcss

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/qizhanchan/qui"
	"github.com/qizhanchan/qui/widgets"
)

// A long transform animation (react.dev's community marquee) is drawn as an
// animated layer over the content cache: its frames carry no content
// damage, and they match a full repaint of the same moment (to within the
// layer's whole-device-pixel snapping).
func TestMarqueeAnimatedLayer(t *testing.T) {
	var sb strings.Builder
	sb.WriteString(`<!doctype html><html><head><style>
@keyframes marquee { from { transform: translateX(0) } to { transform: translateX(-100%) } }
body { margin: 0; background: conic-gradient(from 30deg at 40% 30%, #fde, #def, #efd, #fde) }
nav { position: sticky; top: 0; height: 40px; background: rgba(255,255,255,0.7); z-index: 10 }
.clip { position: relative; display: flex; overflow-x: clip }
.row { display: flex; padding: 30px 0; animation: marquee 1000s linear infinite; white-space: nowrap }
.card { min-width: 160px; display: flex; justify-content: center }
.pic { width: 120px; height: 90px; border-radius: 12px; transform: rotate(3deg); position: relative }
.pic::before { content: ""; position: absolute; inset: 0; border-radius: 12px; box-shadow: 0 8px 20px -6px rgba(0,0,0,0.5) }
</style></head><body><nav>nav</nav><div style="height:60px"></div><div class=clip><div id=row class=row>`)
	for i := 0; i < 8; i++ {
		fmt.Fprintf(&sb, `<div class=card><div class=pic style="background:linear-gradient(135deg,#%02x%02x%02x,#333)"></div></div>`,
			(i*61)%256, (i*97)%256, (i*29)%256)
	}
	sb.WriteString(`</div></div><div style="height:1200px">text below</div></body></html>`)

	prev := webCompat
	SetWebCompat(true)
	t.Cleanup(func() { SetWebCompat(prev) })
	win, err := qui.NewHeadlessWindowForTest(500, 360)
	if err != nil {
		t.Skip(err)
	}
	r := qui.NewCPURenderer()
	win.SetRenderer(r)
	view := widgets.NewScrollView()
	view.FixedViewport = true
	view.OverlayBar = true
	view.PixelScroll = true
	view.Style().Background = qui.Color{R: 1, G: 1, B: 1, A: 1}
	win.SetRoot(view)
	rd := RenderDoc(sb.String(), "", Options{Window: win, ViewportWidth: 500, ViewportHeight: 360, DocumentRoot: true})
	view.SetContentAuto(rd.Root)
	row := rd.ByID["row"].(*El)
	step := func() { time.Sleep(20 * time.Millisecond); win.Step() }
	// Built before it was attached: a restyle registers the animator.
	row.SetAttr("style", "color: black")
	rd.Engine.FlushPendingStyles()
	win.Invalidate()
	for i := 0; i < 4; i++ {
		step()
	}
	if !row.Box.CompositeLayerActive() {
		t.Fatalf("the marquee row is not drawn as an animated layer (anim=%v layer=%v cached=%v seq=%d)",
			row.anim != nil, row.anim != nil && row.anim.layer, view.ContentCached(), win.PaintSeq())
	}
	step() // a frame whose animator damage is composite-only…
	if rects, all := win.ContentDirty(); all || len(rects) != 0 {
		t.Fatalf("animation frame left content damage all=%v %v", all, rects)
	}
	layered := r.Snapshot()

	// …against a full repaint without the cache (and so without layers).
	view.PixelScroll = false
	win.Invalidate()
	step()
	full := r.Snapshot()
	var big, total int
	for i := 0; i+3 < len(layered.Pix); i += 4 {
		d := 0
		for c := 0; c < 4; c++ {
			d = max(d, absInt(int(layered.Pix[i+c])-int(full.Pix[i+c])))
		}
		if d > 40 {
			big++
		}
		total++
	}
	if big > total/200 {
		t.Fatalf("%d of %d pixels differ by more than 40 between the layered frame and a full repaint", big, total)
	}
	if len(row.Box.ChildRasterCountForTest()) == 0 {
		t.Fatal("no card bitmaps were kept")
	}
}

func absInt(v int) int {
	if v < 0 {
		return -v
	}
	return v
}
