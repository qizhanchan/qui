package htmlcss

import (
	"strings"
	"testing"
)

// SVG presentation attributes are CSS: var() in them resolves against the
// element's custom properties (GitHub's hero grid fills its rects with
// fill="var(--brand-color-canvas-subtle)"; left verbatim it painted black).
func TestInlineSVGAttrVar(t *testing.T) {
	SetWebCompat(true)
	defer SetWebCompat(false)
	res := RenderDoc(`<body style="--canvas: #f6f8fa"><svg id="s" width="10" height="10"><rect id="r" fill="var(--canvas)" stroke="var(--missing)" width="10" height="10"></rect></svg></body>`, "", Options{DocumentRoot: true, ScriptDOM: true, ViewportWidth: 100, ViewportHeight: 100})
	s, _ := res.ByID["s"].(*El)
	if s == nil {
		t.Fatal("no svg")
	}
	var b strings.Builder
	writeSVGEl(&b, s, true)
	got := b.String()
	if !strings.Contains(got, `fill="#f6f8fa"`) {
		t.Fatalf("fill var not resolved: %s", got)
	}
	if strings.Contains(got, `stroke=`) {
		t.Fatalf("unresolvable stroke kept: %s", got)
	}
}
