package htmlcss

import (
	"github.com/qizhanchan/qui"
	"testing"
)

func TestClassicRootScrollbarGutter(t *testing.T) {
	SetWebCompat(true)
	defer SetWebCompat(false)
	for _, tc := range []struct {
		g    float32
		both bool
		want float32
	}{{0, false, 640}, {15, false, 625}, {15, true, 610}} {
		css := "html{scrollbar-gutter:stable}body{margin:0}"
		if tc.both {
			css = "html{scrollbar-gutter:stable both-edges}body{margin:0}"
		}
		res := RenderDoc(`<body><div id="content" style="height:1000px"></div></body>`, css, Options{DocumentRoot: true, ViewportWidth: 640, ClassicScrollbarWidth: tc.g})
		res.Root.Measure(qui.Size{W: 640})
		res.Root.Layout(qui.Rect{W: 640, H: 1000})
		if got := res.ByID["content"].Bounds().W; got != tc.want {
			t.Fatalf("gutter %v both %v: width=%v want %v", tc.g, tc.both, got, tc.want)
		}
	}
}

func TestShadowElMutationRendering(t *testing.T) {
	SetWebCompat(true)
	defer SetWebCompat(false)
	res := RenderDoc(`<body><div id="host"><b id="light" slot="named" style="display:block;height:30px"></b><template shadowrootmode="open"><style>:host{display:block}div{height:20px}</style><div id="inside"></div><slot name="named"><div style="height:10px"></div></slot></template></div></body>`, "body{margin:0}", Options{DocumentRoot: true, ViewportWidth: 640})
	layout := func() float32 {
		res.Engine.Restyle()
		sz := res.Root.Measure(qui.Size{W: 640})
		res.Root.Layout(qui.Rect{W: 640, H: sz.H})
		return res.ByID["host"].Bounds().H
	}
	if got := layout(); got != 50 {
		t.Fatal(got)
	}
	light := res.ByID["light"].(*El)
	light.SetAttr("slot", "unassigned")
	if got := layout(); got != 30 {
		t.Fatal(got)
	}
	light.SetAttr("slot", "named")
	if got := layout(); got != 50 {
		t.Fatal(got)
	}
}

func TestShadowStyleMutationKeepsDocumentViewport(t *testing.T) {
	SetWebCompat(true)
	defer SetWebCompat(false)
	res := RenderDoc(`<body><div id="host"><template shadowrootmode="open"><style>:host{display:block}div{height:50vh}</style><div id="inside"></div></template></div></body>`, "", Options{DocumentRoot: true, ViewportWidth: 640, ViewportHeight: 480})
	host := res.ByID["host"].(*El)
	style := host.ShadowRoot().Kids()[0].(*El)
	SetViewportUnits(900, 800)
	style.SetTextContent(":host{display:block}div{height:25vh}")
	res.Engine.Restyle()
	sz := res.Root.Measure(qui.Size{W: 640})
	res.Root.Layout(qui.Rect{W: 640, H: sz.H})
	if got := res.ByID["inside"].Bounds().H; got != 120 {
		t.Fatalf("shadow stylesheet used another viewport: %v", got)
	}
}
