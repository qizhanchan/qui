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

// Exercise the React positioning/repeat rules with Wikipedia's ratio-only SVG
// loading and content-box origin, through the single background implementation.
func TestSVGBackgroundPositionRepeatAndOrigin(t *testing.T) {
	src := "data:image/svg+xml;base64," + base64.StdEncoding.EncodeToString([]byte(`<svg viewBox="0 0 20 10" xmlns="http://www.w3.org/2000/svg"><path d="M0 0H20V10H0Z" fill="red"/></svg>`))
	for _, tc := range []struct {
		name, css string
		points    []qui.Point
		painted   []bool
	}{
		{"longhands", `background-image:url("` + src + `");background-size:10px auto;background-position:right bottom;background-repeat:no-repeat;background-origin:content-box`, []qui.Point{{129, 234}, {120, 234}, {129, 229}}, []bool{true, false, false}},
		{"shorthand", `background:url("` + src + `") right bottom / 10px auto no-repeat content-box`, []qui.Point{{129, 234}, {120, 234}, {129, 229}}, []bool{true, false, false}},
		{"repeat-x", `background:url("` + src + `") right bottom / 10px auto repeat-x content-box`, []qui.Point{{109, 234}, {119, 234}, {129, 229}}, []bool{true, true, false}},
		{"repeat-y", `background:url("` + src + `") right bottom / 10px auto repeat-y content-box`, []qui.Point{{129, 209}, {129, 219}, {119, 234}}, []bool{true, true, false}},
		{"initial-position", `background-image:url("` + src + `");background-size:10px auto;background-repeat:no-repeat`, []qui.Point{{103, 203}, {103, 208}, {113, 203}}, []bool{true, false, false}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			res := RenderDoc(`<div id="image"></div>`, `#image{border:2px solid black;padding:3px;`+tc.css+`}`, Options{})
			factory := res.ByID["image"].(*El).Box.BackgroundShader
			if factory == nil {
				t.Fatal("SVG background has no shader")
			}
			shader := factory(qui.Rect{X: 100, Y: 200, W: 40, H: 40})
			for i, p := range tc.points {
				if c := shader.ColorAt(p.X, p.Y); (c.A > .9) != tc.painted[i] {
					t.Errorf("pixel at %+v: %+v, painted=%v", p, c, tc.painted[i])
				}
			}
		})
	}
}
