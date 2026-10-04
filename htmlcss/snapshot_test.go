package htmlcss

import (
	"image"
	"image/png"
	"os"
	"testing"

	"github.com/qizhanchan/qui"
)

// TestSnapshotDemoPage renders a representative page to a PNG when
// QUI_HTMLCSS_SNAPSHOT=1, for visual verification of the engine.
func TestSnapshotDemoPage(t *testing.T) {
	if os.Getenv("QUI_HTMLCSS_SNAPSHOT") != "1" {
		t.Skip("set QUI_HTMLCSS_SNAPSHOT=1 to write /tmp/qui-htmlcss/page.png")
	}
	html := `<!DOCTYPE html><body>
	  <header class="topbar">
	    <span class="brand">qui</span>
	    <nav class="nav"><a href="#">Docs</a><a href="#">Blog</a><a href="#">GitHub</a></nav>
	  </header>
	  <h1>A lightweight HTML + CSS engine, in Go</h1>
	  <p class="lead">This page is real <strong>HTML</strong> styled with real
	  <em>CSS</em>, rendered into qui widgets. See the <a href="#">docs</a>.</p>
	  <hr>
	  <h2>Feature cards</h2>
	  <div class="cards">
	    <div class="card"><h3>Block flow</h3><p>Headings and paragraphs stack in normal flow.</p></div>
	    <div class="card"><h3>Inline runs</h3><p>Mixed <strong>bold</strong>, <em>italic</em> and <a href="#">links</a> flow together.</p></div>
	    <div class="card accent"><h3>Flexbox</h3><p>Three cards via display:flex + gap.</p></div>
	  </div>
	  <h2>A little form</h2>
	  <div class="form"><input type="text" placeholder="Your name"><button>Sign up</button></div>
	</body>`
	css := `
	body { background:#fff; padding:28px 40px; color:#2a2a33; }
	.topbar { display:flex; justify-content:space-between; align-items:center; padding:12px 16px; background:#1e2130; border-radius:10px; }
	.brand { color:#fff; font-size:22px; font-weight:bold; }
	.nav { display:flex; gap:20px; }
	.nav a { color:#c7d0ff; }
	h1 { font-size:34px; color:#14161f; margin:24px 0 8px 0; }
	h2 { font-size:24px; color:#14161f; margin:28px 0 12px 0; }
	.lead { font-size:17px; line-height:1.6; color:#4a4f60; }
	code { background:#eef0f5; color:#b0306a; }
	a { color:#2f6fed; }
	hr { border-width:1px; border-color:#e2e5ec; margin:20px 0; }
	.cards { display:flex; gap:16px; }
	.card { background:#f7f8fb; border-width:1px; border-color:#e2e5ec; border-radius:12px; padding:16px; flex-grow:1; }
	.card h3 { font-size:18px; color:#14161f; margin:0 0 6px 0; }
	.card p { color:#555c6e; line-height:1.5; margin:0; }
	.card.accent { background:#eef3ff; border-color:#cddaff; }
	.form { display:flex; gap:12px; align-items:center; }`

	root := Render(html, css, Options{})

	const w = 960
	sz := root.Measure(qui.Size{W: w, H: 0})
	h := int(sz.H) + 20
	if h < 400 {
		h = 700
	}
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	// White page background.
	for i := range img.Pix {
		img.Pix[i] = 255
	}
	canvas := qui.NewImageCanvas(img)
	root.Layout(qui.Rect{X: 0, Y: 0, W: w, H: float32(h)})
	root.Draw(canvas)

	_ = os.MkdirAll("/tmp/qui-htmlcss", 0o755)
	f, err := os.Create("/tmp/qui-htmlcss/page.png")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err := png.Encode(f, img); err != nil {
		t.Fatal(err)
	}
	t.Logf("wrote /tmp/qui-htmlcss/page.png (%dx%d, content H=%.0f)", w, h, sz.H)
}
