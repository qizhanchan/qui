package htmlcss

import (
	"github.com/qizhanchan/qui"
	"image"
	"testing"
)

type lazyImageLoader struct{ calls map[string]int }

func (l *lazyImageLoader) Image(src string, _ func()) ImageResource {
	l.calls[src]++
	return ImageResource{Raster: image.NewRGBA(image.Rect(0, 0, 80, 60))}
}

func TestLazyImagesLoadNearViewportOnlyWithScripting(t *testing.T) {
	SetWebCompat(true)
	defer SetWebCompat(false)
	for _, scripting := range []bool{true, false} {
		win := qui.NewTestWindow(qui.Size{W: 600, H: 480})
		loader := &lazyImageLoader{calls: map[string]int{}}
		r := RenderDoc(`<html><body><img loading="lazy" src="near.png" width="80" height="60"><div style="height:4000px"></div><img id="far" loading="lazy" src="far.png" width="80" height="60"></body></html>`, "body{margin:0}", Options{Window: win, DocumentRoot: true, ScriptDOM: scripting, Resources: loader, ViewportWidth: 600, ViewportHeight: 480})
		win.SetRoot(r.Root)
		win.LayoutForTest()
		if scripting {
			if len(loader.calls) != 0 {
				t.Fatal("lazy resources requested before layout viewport check")
			}
			r.Engine.LoadLazyImages(qui.Rect{W: 600, H: 480})
			if loader.calls["near.png"] != 1 || loader.calls["far.png"] != 0 {
				t.Fatalf("initial calls=%v", loader.calls)
			}
			win.LayoutForTest()
			r.Engine.LoadLazyImages(qui.Rect{Y: 3800, W: 600, H: 480})
			if loader.calls["far.png"] != 1 {
				t.Fatalf("scroll did not load far image: %v", loader.calls)
			}
		} else if loader.calls["near.png"] == 0 || loader.calls["far.png"] == 0 {
			t.Fatal("JS-disabled images must load eagerly")
		}
	}
}
