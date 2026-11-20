package htmlcss

import (
	"image"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/qizhanchan/qui"
)

// RFC 0005 P1: pages style, lay out and paint on their own page threads at
// the same time. Every web-* layout fixture runs on several goroutines at
// once; under -race this reports any shared mutable state in the cascade,
// layout or raster paths, and the boxes must match a serial run exactly.
func TestConcurrentPagesStyleLayoutPaint(t *testing.T) {
	files, _ := filepath.Glob("testdata/layout/web-*.html")
	if len(files) == 0 {
		t.Skip("no fixtures")
	}
	if testing.Short() {
		files = files[:min(len(files), 12)]
	}
	SetWebCompat(true)
	defer SetWebCompat(false)
	var pages []string
	for _, f := range files {
		b, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		pages = append(pages, string(b))
	}
	render := func(html string) map[string]domLayoutBox {
		boxes := domLayoutBoxes(html, true)
		return boxes
	}
	paint := func(html string) {
		win := qui.NewTestWindow(domLayoutViewport)
		res := RenderDoc(html, "", Options{ViewportWidth: domLayoutViewport.W, ViewportHeight: domLayoutViewport.H,
			Window: win, DocumentRoot: true, ScriptDOM: strings.Contains(html, "<!-- ScriptDOM -->")})
		win.SetRoot(res.Root)
		sz := res.Root.Measure(qui.Size{W: domLayoutViewport.W})
		res.Root.Layout(qui.Rect{W: domLayoutViewport.W, H: max(sz.H, domLayoutViewport.H)})
		img := image.NewRGBA(image.Rect(0, 0, int(domLayoutViewport.W), int(max(sz.H, domLayoutViewport.H))))
		res.Root.Draw(qui.NewImageCanvas(img))
	}
	want := make([]map[string]domLayoutBox, len(pages))
	for i, p := range pages {
		want[i] = render(p)
	}
	const workers = 4
	var wg sync.WaitGroup
	errs := make(chan string, workers*len(pages))
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func(w int) {
			defer wg.Done()
			for k := range pages {
				i := (k + w*7) % len(pages) // different pages at the same moment
				if got := render(pages[i]); !reflect.DeepEqual(got, want[i]) {
					errs <- files[i]
				}
				paint(pages[i])
			}
		}(w)
	}
	wg.Wait()
	close(errs)
	for f := range errs {
		t.Errorf("%s: boxes differ from the serial run when pages run concurrently", f)
	}
}
