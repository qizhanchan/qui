package htmlcss

import (
	"fmt"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"

	"golang.org/x/image/font/gofont/goregular"

	"github.com/qizhanchan/qui"
)

// Differential test: qui's htmlcss layout geometry against real Chrome.
//
// Each fixture in testdata/layout is an HTML page (<name>.html).
// widgets/scripts/web/dom-layout.mjs loads it in Chrome and records the
// border box (getBoundingClientRect, page coordinates) of every element with
// an id; the result is committed as <name>.golden.json, so this test never
// needs Chrome.
//
// Here the same page is rendered through htmlcss at the same viewport width,
// each id'd El's bounds are mapped to window coordinates and compared field
// by field (x, y, w, h) within a tolerance: domLayoutTolerance by default,
// or the element's data-tol attribute. Fixtures use font-family GoRegular,
// which both sides load from the same Go Regular TTF, so text-driven sizes
// come from identical metrics.
//
// Refresh the goldens (needs Chrome + puppeteer-core, see the script):
//
//	REFRESH_REF=1 go test ./htmlcss -run DOMLayout
//
// A difference qui knowingly has goes in domLayoutAllowlist with its reason.
// An entry that matches no difference fails the test, so the list shrinks as
// fixes land instead of hiding them.

// domLayoutViewport must match VIEWPORT in dom-layout.mjs.
var domLayoutViewport = qui.Size{W: 640, H: 480}

// domLayoutTolerance is the default per-field slack in px: Chrome lays out
// in 1/64 px units and rounds text metrics differently from qui.
const domLayoutTolerance = 1

// domLayoutAllow excuses one cell — fields ("x", "y", "w", "h", or
// "missing" when qui has no El for the id; "*" for any) of the ids matching
// `id` in the fixtures matching `fixture` (path.Match patterns).
type domLayoutAllow struct {
	fixture, id, field, reason string
}

var domLayoutAllowlist = []domLayoutAllow{
	// Documented limits (COVERAGE.md).
	{"grid", "ar*", "*", "grid-auto-rows is not implemented: auto rows size to (empty) content"},
	{"grid", "autorows", "h", "grid-auto-rows is not implemented"},
	{"grid", "im1", "*", "implicit tracks are not implemented: an item placed past the template is not laid out"},
	{"grid", "implicit", "h", "implicit tracks are not implemented: the row past the template adds no height"},
	{"table", "cs", "w", "columns are equal 1fr when the table has a width: no content-proportional distribution"},
	{"table", "s[3-6]", "*", "columns are equal 1fr when the table has a width: no content-proportional distribution"},

	// Undocumented differences — each a fix candidate.
	{"table", "f[hd][12]", "*", "table-layout: fixed ignores a first-row cell's width: columns are equal"},
	{"table", "sc*", "*", "border-spacing is only applied between cells, not around the table's outer edge"},
	{"table", "spacing", "h", "border-spacing is not applied above the first or below the last row"},
}

type domLayoutGolden struct {
	Fixture  string                `json:"fixture"`
	Viewport [2]float32            `json:"viewport"`
	Boxes    map[string][4]float32 `json:"boxes"`
}

var domLayoutFields = [4]string{"x", "y", "w", "h"}

func TestDOMLayout(t *testing.T) {
	dir := filepath.Join("testdata", "layout")
	if os.Getenv("REFRESH_REF") == "1" {
		node := os.Getenv("NODE_BIN")
		if node == "" {
			node = "node"
		}
		cmd := exec.Command(node, filepath.Join("..", "widgets", "scripts", "web", "dom-layout.mjs"))
		cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
		if err := cmd.Run(); err != nil {
			t.Fatalf("regenerating goldens: %v (first run: npm ci --prefix widgets/scripts/web)", err)
		}
	}
	if err := qui.LoadFontVariantFromBytes("GoRegular", qui.FontWeightNormal, false, goregular.TTF); err != nil {
		t.Fatalf("register GoRegular: %v", err)
	}
	pages, err := filepath.Glob(filepath.Join(dir, "*.html"))
	if err != nil || len(pages) == 0 {
		t.Fatalf("no fixtures in %s (%v)", dir, err)
	}
	used := make([]bool, len(domLayoutAllowlist))
	for _, p := range pages {
		name := strings.TrimSuffix(filepath.Base(p), ".html")
		t.Run(name, func(t *testing.T) { runDOMLayoutFixture(t, dir, name, used) })
	}
	if t.Failed() {
		return
	}
	for i, a := range domLayoutAllowlist {
		if !used[i] {
			t.Errorf("allowlist entry {%s / %s / %s} matches no difference any more — qui agrees with Chrome now, delete it",
				a.fixture, a.id, a.field)
		}
	}
}

func runDOMLayoutFixture(t *testing.T, dir, name string, used []bool) {
	html, err := os.ReadFile(filepath.Join(dir, name+".html"))
	if err != nil {
		t.Fatal(err)
	}
	var golden domLayoutGolden
	readJSON(t, filepath.Join(dir, name+".golden.json"), &golden)
	if golden.Viewport != [2]float32{domLayoutViewport.W, domLayoutViewport.H} {
		t.Fatalf("golden viewport %v, test viewport %v — regenerate with REFRESH_REF=1", golden.Viewport, domLayoutViewport)
	}

	got := domLayoutBoxes(string(html))

	ids := make([]string, 0, len(golden.Boxes))
	for id := range golden.Boxes {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	var diffs []string
	for _, id := range ids {
		want := golden.Boxes[id]
		box, ok := got[id]
		if !ok {
			if !domLayoutExcuse(name, id, "missing", used) {
				diffs = append(diffs, fmt.Sprintf("#%s: missing (htmlcss built no El for it)", id))
			}
			continue
		}
		var bad []string
		for i, f := range domLayoutFields {
			d := box.r[i] - want[i]
			if d < 0 {
				d = -d
			}
			if d > box.tol && !domLayoutExcuse(name, id, f, used) {
				bad = append(bad, fmt.Sprintf("%s chrome=%g qui=%g", f, want[i], box.r[i]))
			}
		}
		if len(bad) > 0 {
			diffs = append(diffs, fmt.Sprintf("#%s: %s", id, strings.Join(bad, ", ")))
		}
	}
	if len(diffs) > 0 {
		t.Errorf("%d boxes differ from Chrome:\n  %s", len(diffs), strings.Join(diffs, "\n  "))
	}
}

// domLayoutExcuse reports whether an allowlist entry covers the cell,
// marking every entry that does as used.
func domLayoutExcuse(fixture, id, field string, used []bool) bool {
	excused := false
	for i, a := range domLayoutAllowlist {
		fixOK, _ := path.Match(a.fixture, fixture)
		idOK, _ := path.Match(a.id, id)
		if fixOK && idOK && (a.field == "*" || a.field == field) {
			used[i] = true
			excused = true
		}
	}
	return excused
}

type domLayoutBox struct {
	r   [4]float32
	tol float32
}

// domLayoutBoxes renders html through htmlcss at the fixture viewport and
// returns every id'd El's border box in window coordinates.
func domLayoutBoxes(html string) map[string]domLayoutBox {
	win := qui.NewTestWindow(domLayoutViewport)
	res := RenderDoc(html, "", Options{ViewportWidth: domLayoutViewport.W, Window: win})
	win.SetRoot(res.Root)
	sz := res.Root.Measure(qui.Size{W: domLayoutViewport.W, H: 0})
	res.Root.Layout(qui.Rect{W: domLayoutViewport.W, H: max(sz.H, domLayoutViewport.H)})

	out := map[string]domLayoutBox{}
	for id, w := range res.ByID {
		el, ok := w.(*El)
		if !ok {
			continue
		}
		b := el.Bounds()
		p := qui.LocalPointToWindow(el, qui.Point{X: b.X, Y: b.Y})
		tol := float32(domLayoutTolerance)
		if v, ok := el.Attr("data-tol"); ok {
			if f, err := strconv.ParseFloat(v, 32); err == nil {
				tol = float32(f)
			}
		}
		out[id] = domLayoutBox{r: [4]float32{p.X, p.Y, b.W, b.H}, tol: tol}
	}
	return out
}
