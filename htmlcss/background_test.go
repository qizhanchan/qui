package htmlcss

import (
	"encoding/base64"
	"testing"

	"github.com/qizhanchan/qui"
)

func TestSVGBackgroundShorthandSizeAndOrigin(t *testing.T) {
	// Two layers as used by CSS image fallbacks: the URL is the top layer.
	src := "data:image/svg+xml;base64," + base64.StdEncoding.EncodeToString([]byte(`<svg width="32" height="32" viewBox="0 0 32 16" xmlns="http://www.w3.org/2000/svg"><path d="m2 27 14-29 14 29z" fill="#999"/></svg>`))
	for _, prop := range []string{"background", "background-image"} {
		t.Run(prop, func(t *testing.T) {
			res := RenderDoc(`<div id="arrow"></div>`, `#arrow{width:30px;height:30px;border:2px solid black;padding:3px;`+prop+`:url("`+src+`"),linear-gradient(transparent,transparent);background-size:10px auto;background-repeat:no-repeat;background-origin:content-box;}`, Options{})
			el := res.ByID["arrow"].(*El)
			if el.Box.BackgroundShader == nil {
				t.Fatal("SVG background did not install a shader")
			}
			shader := el.Box.BackgroundShader(qui.Rect{X: 100, Y: 200, W: 40, H: 40})
			if c := shader.ColorAt(110, 210); c.A < 0.9 {
				t.Fatalf("triangle center not painted: %+v", c)
			}
			for _, p := range []qui.Point{{X: 102, Y: 210}, {X: 120, Y: 210}, {X: 110, Y: 220}} {
				if c := shader.ColorAt(p.X, p.Y); c.A != 0 {
					t.Errorf("outside sized content-origin tile at %+v: %+v", p, c)
				}
			}
		})
	}
}

func TestRatioOnlySVGBackgroundSize(t *testing.T) {
	src := "data:image/svg+xml;base64," + base64.StdEncoding.EncodeToString([]byte(`<svg viewBox="0 0 20 10" xmlns="http://www.w3.org/2000/svg"><path d="M0 0H20V10H0Z" fill="red"/></svg>`))
	res := RenderDoc(`<div id="image"></div>`, `#image{background-image:url("`+src+`");background-size:10px auto;background-repeat:no-repeat}`, Options{})
	factory := res.ByID["image"].(*El).Box.BackgroundShader
	if factory == nil {
		t.Fatal("ratio-only SVG background has no shader")
	}
	shader := factory(qui.Rect{W: 30, H: 30})
	if c := shader.ColorAt(5, 2); c.A < 0.9 {
		t.Fatalf("inside ratio-only SVG: %+v", c)
	}
	if c := shader.ColorAt(5, 6); c.A != 0 {
		t.Fatalf("SVG should be 10 by 5: %+v", c)
	}
}
