package htmlcss

import (
	"fmt"
	"math"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/qizhanchan/qui"
)

// Differential test: qui's computed styles against real Chrome.
//
// Each fixture in testdata/style is an HTML page (<name>.html).
// widgets/scripts/web/dom-style.mjs loads it in Chrome and records
// getComputedStyle for every element with an id over a fixed property list;
// the result is committed as <name>.golden.json, so this test never needs
// Chrome.
//
// Here the same page is styled through htmlcss and each id'd element's
// resolved ComputedStyle is serialized the way getComputedStyle formats it
// ("16px", "rgb(26, 102, 230)", "700", "normal"), then compared property by
// property — lengths within 0.05px, colors within 1 per channel. It checks
// the cascade, inheritance, var() and unit resolution; layout is
// TestDOMLayout's job.
//
// Refresh the goldens (needs Chrome + puppeteer-core, see the script):
//
//	REFRESH_REF=1 go test ./htmlcss -run DOMStyle
//
// A difference qui knowingly has goes in domStyleAllowlist with its reason;
// an entry that matches no difference fails the test.

// domStyleAllow excuses the properties matching prop of the ids matching
// id in the fixtures matching fixture (path.Match patterns).
type domStyleAllow struct {
	fixture, id, prop, reason string
}

var domStyleAllowlist = []domStyleAllow{
	// qui models a list item as a block plus a hung marker (listmarker.go)
	// rather than a distinct display type.
	{"*", "li", "display", "list-item computes as block: qui lays a list item out as a block plus its marker"},
}

type domStyleGolden struct {
	Fixture string                       `json:"fixture"`
	Styles  map[string]map[string]string `json:"styles"`
}

func TestDOMStyle(t *testing.T) {
	dir := filepath.Join("testdata", "style")
	if os.Getenv("REFRESH_REF") == "1" {
		node := os.Getenv("NODE_BIN")
		if node == "" {
			node = "node"
		}
		cmd := exec.Command(node, filepath.Join("..", "widgets", "scripts", "web", "dom-style.mjs"))
		cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
		if err := cmd.Run(); err != nil {
			t.Fatalf("regenerating goldens: %v (first run: npm ci --prefix widgets/scripts/web)", err)
		}
	}
	pages, err := filepath.Glob(filepath.Join(dir, "*.html"))
	if err != nil || len(pages) == 0 {
		t.Fatalf("no fixtures in %s (%v)", dir, err)
	}
	used := make([]bool, len(domStyleAllowlist))
	for _, p := range pages {
		name := strings.TrimSuffix(filepath.Base(p), ".html")
		t.Run(name, func(t *testing.T) { runDOMStyleFixture(t, dir, name, used) })
	}
	if t.Failed() {
		return
	}
	for i, a := range domStyleAllowlist {
		if !used[i] {
			t.Errorf("allowlist entry {%s / %s / %s} matches no difference any more — qui agrees with Chrome now, delete it",
				a.fixture, a.id, a.prop)
		}
	}
}

func runDOMStyleFixture(t *testing.T, dir, name string, used []bool) {
	html, err := os.ReadFile(filepath.Join(dir, name+".html"))
	if err != nil {
		t.Fatal(err)
	}
	var golden domStyleGolden
	readJSON(t, filepath.Join(dir, name+".golden.json"), &golden)
	res := RenderDoc(string(html), "", Options{ViewportWidth: 640})

	ids := make([]string, 0, len(golden.Styles))
	for id := range golden.Styles {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	var diffs []string
	for _, id := range ids {
		el, _ := res.ByID[id].(*El)
		if el == nil || el.lastCS == nil {
			diffs = append(diffs, fmt.Sprintf("#%s: no styled El", id))
			continue
		}
		props := make([]string, 0, len(golden.Styles[id]))
		for p := range golden.Styles[id] {
			props = append(props, p)
		}
		sort.Strings(props)
		var bad []string
		for _, p := range props {
			want := golden.Styles[id][p]
			got, ok := cssValue(el.lastCS, p)
			if !ok || sameCSSValue(p, want, got) {
				continue
			}
			if domStyleExcuse(name, id, p, used) {
				continue
			}
			bad = append(bad, fmt.Sprintf("%s chrome=%q qui=%q", p, want, got))
		}
		if len(bad) > 0 {
			diffs = append(diffs, fmt.Sprintf("#%s: %s", id, strings.Join(bad, ", ")))
		}
	}
	if len(diffs) > 0 {
		t.Errorf("%d elements differ from Chrome:\n  %s", len(diffs), strings.Join(diffs, "\n  "))
	}
}

func domStyleExcuse(fixture, id, prop string, used []bool) bool {
	excused := false
	for i, a := range domStyleAllowlist {
		f, _ := path.Match(a.fixture, fixture)
		d, _ := path.Match(a.id, id)
		p, _ := path.Match(a.prop, prop)
		if f && d && p {
			used[i] = true
			excused = true
		}
	}
	return excused
}

func cssPx(v float32) string { return strconv.FormatFloat(float64(v), 'f', -1, 32) + "px" }

func cssColor(c qui.Color) string {
	r, g, b := math.Round(float64(c.R)*255), math.Round(float64(c.G)*255), math.Round(float64(c.B)*255)
	if c.A >= 1 {
		return fmt.Sprintf("rgb(%d, %d, %d)", int(r), int(g), int(b))
	}
	return fmt.Sprintf("rgba(%d, %d, %d, %s)", int(r), int(g), int(b),
		strconv.FormatFloat(float64(c.A), 'f', -1, 32))
}

// cssValue serializes one property of a resolved ComputedStyle the way
// getComputedStyle does. ok is false for a property it doesn't model.
func cssValue(cs *ComputedStyle, prop string) (string, bool) {
	side := func(in qui.Insets, s string) float32 {
		switch s {
		case "top":
			return in.Top
		case "right":
			return in.Right
		case "bottom":
			return in.Bottom
		}
		return in.Left
	}
	switch prop {
	case "display":
		if cs.Display == "" {
			return "inline", true
		}
		return cs.Display, true
	case "visibility":
		if cs.Hidden {
			return "hidden", true
		}
		return "visible", true
	case "position":
		if cs.Position == "" {
			return "static", true
		}
		return cs.Position, true
	case "opacity":
		if !cs.HasOpacity {
			return "1", true
		}
		return strconv.FormatFloat(float64(cs.Opacity), 'f', -1, 32), true
	case "color":
		return cssColor(cs.Color), true
	case "background-color":
		if !cs.HasBackground {
			return "rgba(0, 0, 0, 0)", true
		}
		return cssColor(cs.Background), true
	case "font-size":
		return cssPx(cs.FontSize), true
	case "font-weight":
		return strconv.Itoa(int(cs.FontWeight)), true
	case "font-style":
		if cs.Italic {
			return "italic", true
		}
		return "normal", true
	case "line-height":
		switch {
		case cs.LineHeightPx > 0:
			return cssPx(cs.LineHeightPx), true
		case cs.LineHeight <= 0:
			return "normal", true
		}
		return cssPx(cs.LineHeight * cs.FontSize), true
	case "text-align":
		switch cs.TextAlign {
		case qui.TextAlignCenter:
			return "center", true
		case qui.TextAlignEnd:
			return "end", true
		case qui.TextAlignJustify:
			return "justify", true
		}
		return "start", true
	case "text-transform":
		if cs.TextTransform == "" {
			return "none", true
		}
		return cs.TextTransform, true
	case "letter-spacing":
		if cs.LetterSpacing == 0 {
			return "normal", true
		}
		return cssPx(cs.LetterSpacing), true
	case "text-decoration-line":
		var parts []string
		if cs.Underline {
			parts = append(parts, "underline")
		}
		if cs.Overline {
			parts = append(parts, "overline")
		}
		if cs.LineThrough {
			parts = append(parts, "line-through")
		}
		if len(parts) == 0 {
			return "none", true
		}
		return strings.Join(parts, " "), true
	case "white-space":
		switch {
		case cs.PreserveWS && cs.NoWrap:
			return "pre", true
		case cs.PreserveWS:
			return "pre-wrap", true
		case cs.PreserveNL:
			return "pre-line", true
		case cs.NoWrap:
			return "nowrap", true
		}
		return "normal", true
	case "box-sizing":
		if cs.BoxSizing == "" {
			return "content-box", true
		}
		return cs.BoxSizing, true
	case "flex-grow":
		return strconv.FormatFloat(float64(cs.FlexGrow), 'f', -1, 32), true
	case "flex-shrink":
		if !cs.HasFlexShrink {
			return "1", true
		}
		return strconv.FormatFloat(float64(cs.FlexShrink), 'f', -1, 32), true
	case "border-top-color":
		c := cs.BorderColor
		if cs.HasSideBorders && cs.SideColors.Top.A > 0 {
			c = cs.SideColors.Top
		}
		if !cs.HasBorder && !cs.HasSideBorders {
			c = cs.Color // initial: currentColor
		}
		return cssColor(c), true
	case "border-top-left-radius":
		r := cs.Radius
		if cs.Corners != (qui.CornerRadii{}) {
			r = cs.Corners.TL
		}
		return cssPx(r), true
	}
	for _, s := range []string{"top", "right", "bottom", "left"} {
		switch prop {
		case "margin-" + s:
			return cssPx(side(cs.Margin, s)), true
		case "padding-" + s:
			return cssPx(side(cs.Padding, s)), true
		case "border-" + s + "-width":
			if cs.HasSideBorders {
				return cssPx(side(cs.SideWidths, s)), true
			}
			return cssPx(cs.BorderWidth), true
		}
	}
	return "", false
}

// sameCSSValue compares a Chrome computed value with qui's serialization:
// lengths within 0.05px, colors within 1 per channel, `left` / `right`
// text-align against qui's start / end, the rest exactly.
func sameCSSValue(prop, want, got string) bool {
	if want == got {
		return true
	}
	if prop == "text-align" {
		return (want == "left" && got == "start") || (want == "right" && got == "end")
	}
	if strings.HasSuffix(want, "px") && strings.HasSuffix(got, "px") {
		w, e1 := strconv.ParseFloat(strings.TrimSuffix(want, "px"), 64)
		g, e2 := strconv.ParseFloat(strings.TrimSuffix(got, "px"), 64)
		return e1 == nil && e2 == nil && math.Abs(w-g) <= 0.05
	}
	if strings.HasPrefix(want, "rgb") && strings.HasPrefix(got, "rgb") {
		wv, gv := colorNums(want), colorNums(got)
		if len(wv) != 4 || len(gv) != 4 {
			return false
		}
		for i := range wv {
			tol := 1.0
			if i == 3 {
				tol = 0.01
			}
			if math.Abs(wv[i]-gv[i]) > tol {
				return false
			}
		}
		return true
	}
	return false
}

// colorNums parses rgb(...) / rgba(...) into r, g, b, a.
func colorNums(s string) []float64 {
	s = strings.TrimSuffix(strings.TrimPrefix(strings.TrimPrefix(s, "rgba("), "rgb("), ")")
	var out []float64
	for _, f := range strings.Split(s, ",") {
		v, err := strconv.ParseFloat(strings.TrimSpace(f), 64)
		if err != nil {
			return nil
		}
		out = append(out, v)
	}
	if len(out) == 3 {
		out = append(out, 1)
	}
	return out
}
