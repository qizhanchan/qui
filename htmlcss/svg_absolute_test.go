package htmlcss

import (
	"github.com/qizhanchan/qui"
	"testing"
)

func TestScriptSVGAbsoluteAutoHeight(t *testing.T) {
	prev := webCompat
	webCompat = true
	defer func() { webCompat = prev }()
	res := RenderDoc(`<body><a id=a>Why Vue<svg id=icon viewBox="0 0 100 125"><title>Play icon</title><path d="M0 0h100v125z"/></svg></a></body>`, `body{margin:0}a{position:relative;display:inline-block;padding:8px 16px 8px 48px;font:16px Arial}svg{position:absolute;width:24px;top:8px;left:15px}`, Options{ScriptDOM: true})
	res.Root.Layout(qui.Rect{W: 640, H: 480})
	e := res.ByID["icon"].(*El)
	if absf(e.Bounds().H-30) > 1 {
		t.Fatalf("SVG height=%v, want 30", e.Bounds().H)
	}
}
