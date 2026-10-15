package htmlcss

import (
	"testing"

	"github.com/qizhanchan/qui"
)

// The fuzz targets check that no input panics or hangs the parsers or the
// render pipeline. `go test` runs only the seeds; explore with e.g.
//
//	go test ./htmlcss -run '^$' -fuzz FuzzParseCSS -fuzztime 60s

func FuzzParseCSS(f *testing.F) {
	for _, s := range []string{
		``,
		`.a { color: red }`,
		`:root { --x: 4px } .a > .b + .c ~ .d:nth-child(2n+1):not(.e) { margin: var(--x) }`,
		`@media (max-width: 600px) { .a { display: none } }`,
		`.a:has(> .b:hover) { background: linear-gradient(90deg, #fff 0%, #000 100%) }`,
		`.a { border: 1px solid; padding: 1px 2px 3px; font: bold 12px/1.5 sans-serif }`,
		`.a { color: rgb(`, `}}}{{{`, `/* unterminated`,
	} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, src string) {
		ParseCSS(src)
		ParseCSSViewport(src, 320)
	})
}

func FuzzParseHTML(f *testing.F) {
	for _, s := range []string{
		``,
		`<div class="a"><span>x</span></div>`,
		`<ul><li>a<li>b</ul><p>unclosed`,
		`<input value="v"><select><option>1</option></select><textarea>t</textarea>`,
		`<table><tr><td>1</td></tr></table>`,
		`<!-- c --><br/><img src="">&amp;&lt;&#x1F600;`,
	} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, src string) {
		ParseHTML(src)
	})
}

// FuzzRender runs markup and a stylesheet through the whole pipeline:
// style, measure and layout.
func FuzzRender(f *testing.F) {
	f.Add(`<div class="a"><span>x</span></div>`, `.a { display: flex; gap: 4px }`)
	f.Add(`<div class="g"><p>a</p><p>b</p></div>`, `.g { display: grid; grid-template-columns: 1fr 2fr }`)
	f.Add(`<div style="overflow: auto; height: 20px"><p>long</p></div>`, `p { white-space: pre; transform: rotate(10deg) }`)
	f.Add(`<ol><li>a</li></ol>`, `li::marker { color: red } li:first-child { position: relative; left: 2px }`)
	f.Fuzz(func(t *testing.T, html, css string) {
		if len(html)+len(css) > 4096 {
			t.Skip()
		}
		root := Render(html, css, Options{BaseDir: t.TempDir(), ViewportWidth: 400})
		if root == nil {
			return
		}
		qui.MeasureChild(root, qui.Size{W: 400, H: 300})
		root.Layout(qui.Rect{W: 400, H: 300})
	})
}
