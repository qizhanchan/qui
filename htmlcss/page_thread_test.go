package htmlcss

import (
	"bytes"
	"fmt"
	"image"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/qizhanchan/qui"
)

// RFC 0005 P2: documents run in PageHosts — qui Windows on the embedded
// backend — on page threads of their own. Every web-* fixture is rendered
// once serially (a sync page thread on the test goroutine) and then again
// with several page threads running pages at the same moment while the UI
// thread steps a host window showing one of them. The committed frames
// must match the serial ones (up to glyph-cache antialiasing noise, see
// qui.FramesClose); under -race this reports any
// state the page threads or the UI thread share.
func TestPageThreadsRenderFixtures(t *testing.T) {
	files, _ := filepath.Glob("testdata/layout/web-*.html")
	if len(files) == 0 {
		t.Skip("no fixtures")
	}
	if testing.Short() {
		files = files[:min(len(files), 10)]
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
	vw, vh := int(domLayoutViewport.W), int(domLayoutViewport.H)
	setup := func(html string) func(w *qui.Window) {
		return func(w *qui.Window) {
			res := RenderDoc(html, "", Options{ViewportWidth: domLayoutViewport.W, ViewportHeight: domLayoutViewport.H,
				Window: w, DocumentRoot: true, ScriptDOM: strings.Contains(html, "<!-- ScriptDOM -->")})
			w.SetRoot(res.Root)
		}
	}

	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	want := make([]*image.RGBA, len(pages))
	for i, p := range pages {
		th := qui.NewSyncPageThread("serial")
		h := th.NewPageHost(qui.PageHostConfig{Width: vw, Height: vh, Setup: setup(p)})
		th.RunUntilIdle(50)
		want[i] = h.Snapshot()
		th.Close()
		if want[i] == nil {
			t.Fatalf("%s: no frame", files[i])
		}
	}

	host, err := qui.NewHeadlessWindowForTest(vw, vh)
	if err != nil {
		t.Skip(err)
	}
	defer host.Destroy()

	const threads = 3
	ths := make([]*qui.PageThread, threads)
	for i := range ths {
		ths[i] = qui.NewPageThread(fmt.Sprintf("page-%d", i))
		defer ths[i].Close()
	}
	hosts := make([]*qui.PageHost, len(pages))
	for i, p := range pages {
		hosts[i] = ths[i%threads].NewPageHost(qui.PageHostConfig{Width: vw, Height: vh, Setup: setup(p)})
	}
	view := qui.NewPageView(hosts[0])
	host.SetRoot(view)

	// Wait until every page is idle, stepping the host window meanwhile.
	idle := make([]bool, len(pages))
	deadline := time.Now().Add(60 * time.Second)
	for {
		all := true
		for i, h := range hosts {
			if idle[i] {
				continue
			}
			all = false
			done := make(chan bool, 1)
			h.Do(func(w *qui.Window) { done <- w.IdleState().IsIdle() })
			select {
			case idle[i] = <-done:
			case <-time.After(10 * time.Second):
				t.Fatalf("%s: page thread did not answer", files[i])
			}
		}
		if all {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("pages did not go idle")
		}
		host.Step()
		time.Sleep(5 * time.Millisecond)
	}
	for i, h := range hosts {
		got := h.Snapshot()
		if got == nil || got.Bounds() != want[i].Bounds() {
			t.Errorf("%s: frame bounds %v, want %v", files[i], boundsOf(got), want[i].Bounds())
			continue
		}
		if !bytes.Equal(got.Pix, want[i].Pix) {
			if bad, ok := qui.FramesClose(got, want[i], 2); !ok {
				t.Errorf("%s: frame rendered on a page thread differs from the serial render (%d px)", files[i], bad)
			}
		}
	}
}

func boundsOf(img *image.RGBA) image.Rectangle {
	if img == nil {
		return image.Rectangle{}
	}
	return img.Bounds()
}
