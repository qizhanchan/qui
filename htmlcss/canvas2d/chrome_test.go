package canvas2d

import (
	"encoding/json"
	"fmt"
	"image"
	"image/draw"
	"image/png"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/image/font/gofont/goregular"

	"github.com/qizhanchan/qui"
)

// Replays testdata/<name>.json through Context and compares with the
// Chrome rendering in <name>.png (widgets/scripts/web/canvas-2d.mjs).
// Anti-aliasing differs between rasterizers, so the comparison allows
// edge pixels to differ: it bounds the mean error and the share of
// pixels that differ a lot.

type fixture struct {
	W, H int
	Ops  [][]any
}

// limits per fixture: mean channel error (0..255) and the fraction of
// pixels whose largest channel error exceeds 64.
var chromeLimits = map[string][2]float64{
	"text": {6, 0.06},
}

func checker() *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, 8, 8))
	for y := 0; y < 8; y++ {
		for x := 0; x < 8; x++ {
			o := img.PixOffset(x, y)
			if ((x>>2)+(y>>2))%2 == 0 {
				copy(img.Pix[o:], []byte{40, 160, 40, 255})
			} else {
				// 255,255,255 at alpha 160, premultiplied.
				copy(img.Pix[o:], []byte{160, 160, 160, 160})
			}
		}
	}
	return img
}

func checkerData() []byte {
	out := make([]byte, 0, 256)
	for y := 0; y < 8; y++ {
		for x := 0; x < 8; x++ {
			if ((x>>2)+(y>>2))%2 == 0 {
				out = append(out, 40, 160, 40, 255)
			} else {
				out = append(out, 255, 255, 255, 160)
			}
		}
	}
	return out
}

func num(v any) float64 {
	switch n := v.(type) {
	case float64:
		return n
	case bool:
		if n {
			return 1
		}
	}
	return 0
}

func replay(t *testing.T, fx fixture) *Context {
	c := New(fx.W, fx.H)
	vars := map[string]any{}
	arg := func(v any) any {
		if s, ok := v.(string); ok && strings.HasPrefix(s, "$") {
			return vars[s]
		}
		return v
	}
	style := func(v any) Style {
		switch s := arg(v).(type) {
		case string:
			col, ok := ParseColor(s)
			if !ok {
				t.Fatalf("color %q", s)
			}
			return Color{C: col}
		case Style:
			return s
		}
		t.Fatalf("style %v", v)
		return nil
	}
	for _, op := range fx.Ops {
		name := op[0].(string)
		a := op[1:]
		f := func(i int) float64 { return num(a[i]) }
		str := func(i int) string { return a[i].(string) }
		switch {
		case strings.HasPrefix(name, "$") && strings.Contains(name, "="):
			parts := strings.SplitN(name, "=", 2)
			switch parts[1] {
			case "createLinearGradient":
				vars[parts[0]] = NewLinearGradient(f(0), f(1), f(2), f(3))
			case "createRadialGradient":
				g, _ := NewRadialGradient(f(0), f(1), f(2), f(3), f(4), f(5))
				vars[parts[0]] = g
			case "createConicGradient":
				vars[parts[0]] = NewConicGradient(f(0), f(1), f(2))
			case "createPattern:checker":
				p, _ := NewPattern(checker(), str(0))
				vars[parts[0]] = p
			default:
				t.Fatalf("op %s", name)
			}
			continue
		case strings.HasPrefix(name, "$") && strings.HasSuffix(name, ".addColorStop"):
			g := vars[strings.TrimSuffix(name, ".addColorStop")].(*Gradient)
			col, _ := ParseColor(str(1))
			g.AddColorStop(f(0), col)
			continue
		}
		switch name {
		case "fillStyle":
			c.SetFillStyle(style(a[0]))
		case "strokeStyle":
			c.SetStrokeStyle(style(a[0]))
		case "lineWidth":
			c.SetLineWidth(f(0))
		case "lineJoin":
			c.SetLineJoin(str(0))
		case "lineCap":
			c.SetLineCap(str(0))
		case "lineDashOffset":
			c.SetLineDashOffset(f(0))
		case "setLineDash":
			var d []float64
			for _, v := range a[0].([]any) {
				d = append(d, num(v))
			}
			c.SetLineDash(d)
		case "globalAlpha":
			c.SetGlobalAlpha(f(0))
		case "globalCompositeOperation":
			c.SetCompositeOp(str(0))
		case "shadowColor":
			col, _ := ParseColor(str(0))
			c.SetShadowColor(col)
		case "shadowBlur":
			c.SetShadowBlur(f(0))
		case "shadowOffsetX":
			c.SetShadowOffsetX(f(0))
		case "shadowOffsetY":
			c.SetShadowOffsetY(f(0))
		case "font":
			c.SetFont(str(0))
		case "textAlign":
			c.SetTextAlign(str(0))
		case "textBaseline":
			c.SetTextBaseline(str(0))
		case "imageSmoothingEnabled":
			c.SetImageSmoothing(a[0].(bool))
		case "fillRect":
			c.FillRect(f(0), f(1), f(2), f(3))
		case "strokeRect":
			c.StrokeRect(f(0), f(1), f(2), f(3))
		case "clearRect":
			c.ClearRect(f(0), f(1), f(2), f(3))
		case "beginPath":
			c.BeginPath()
		case "closePath":
			c.ClosePath()
		case "moveTo":
			c.MoveTo(f(0), f(1))
		case "lineTo":
			c.LineTo(f(0), f(1))
		case "quadraticCurveTo":
			c.QuadraticCurveTo(f(0), f(1), f(2), f(3))
		case "bezierCurveTo":
			c.BezierCurveTo(f(0), f(1), f(2), f(3), f(4), f(5))
		case "arc":
			c.Arc(f(0), f(1), f(2), f(3), f(4), len(a) > 5 && a[5].(bool))
		case "ellipse":
			c.Ellipse(f(0), f(1), f(2), f(3), f(4), f(5), f(6), len(a) > 7 && a[7].(bool))
		case "arcTo":
			c.ArcTo(f(0), f(1), f(2), f(3), f(4))
		case "rect":
			c.Rect(f(0), f(1), f(2), f(3))
		case "roundRect":
			var rs []Radius
			for _, v := range a[4].([]any) {
				rs = append(rs, Radius{num(v), num(v)})
			}
			c.RoundRect(f(0), f(1), f(2), f(3), rs)
		case "fill":
			c.Fill(nil, len(a) > 0 && a[0] == "evenodd")
		case "stroke":
			c.Stroke(nil)
		case "clip":
			c.Clip(nil, len(a) > 0 && a[0] == "evenodd")
		case "save":
			c.Save()
		case "restore":
			c.Restore()
		case "translate":
			c.Translate(f(0), f(1))
		case "rotate":
			c.Rotate(f(0))
		case "scale":
			c.Scale(f(0), f(1))
		case "setTransform":
			c.SetTransform(f(0), f(1), f(2), f(3), f(4), f(5))
		case "resetTransform":
			c.ResetTransform()
		case "fillText", "strokeText":
			var mw *float64
			if len(a) > 3 {
				v := f(3)
				mw = &v
			}
			if name == "fillText" {
				c.FillText(str(0), f(1), f(2), mw)
			} else {
				c.StrokeText(str(0), f(1), f(2), mw)
			}
		case "putImageData:checker":
			c.PutImageData(checkerData(), 8, 8, int(f(0)), int(f(1)), 0, 0, 8, 8)
		case "copyImageData":
			d := c.GetImageData(int(f(0)), int(f(1)), int(f(2)), int(f(3)))
			c.PutImageData(d, int(f(2)), int(f(3)), int(f(4)), int(f(5)), 0, 0, int(f(2)), int(f(3)))
		case "drawImage:checker":
			c.DrawImage(checker(), 0, 0, 8, 8, f(0), f(1), f(2), f(3))
		default:
			t.Fatalf("unknown op %s", name)
		}
	}
	return c
}

func TestMatchesChrome(t *testing.T) {
	if err := qui.LoadFontVariantFromBytes("GoRegular", qui.FontWeightNormal, false, goregular.TTF); err != nil {
		t.Fatal(err)
	}
	files, _ := filepath.Glob("testdata/*.json")
	if len(files) == 0 {
		t.Fatal("no fixtures")
	}
	for _, file := range files {
		name := strings.TrimSuffix(filepath.Base(file), ".json")
		t.Run(name, func(t *testing.T) {
			var fx fixture
			b, err := os.ReadFile(file)
			if err != nil {
				t.Fatal(err)
			}
			if err := json.Unmarshal(b, &fx); err != nil {
				t.Fatal(err)
			}
			got := replay(t, fx).Image()
			wf, err := os.Open(strings.TrimSuffix(file, ".json") + ".png")
			if err != nil {
				t.Fatal(err)
			}
			wimg, err := png.Decode(wf)
			wf.Close()
			if err != nil {
				t.Fatal(err)
			}
			want := image.NewRGBA(wimg.Bounds())
			draw.Draw(want, want.Rect, wimg, image.Point{}, draw.Src)
			mean, bad := diff(got, want)
			lim, ok := chromeLimits[name]
			if !ok {
				lim = [2]float64{3, 0.02}
			}
			if dir := os.Getenv("CANVAS2D_OUT"); dir != "" {
				f, _ := os.Create(filepath.Join(dir, name+".got.png"))
				_ = png.Encode(f, got)
				f.Close()
			}
			msg := fmt.Sprintf("mean error %.2f (limit %.1f), %.2f%% pixels off (limit %.1f%%)", mean, lim[0], bad*100, lim[1]*100)
			if mean > lim[0] || bad > lim[1] {
				t.Errorf("%s — set CANVAS2D_OUT=dir to see the rendering", msg)
			} else {
				t.Log(msg)
			}
		})
	}
}

func diff(a, b *image.RGBA) (mean, bad float64) {
	if a.Rect != b.Rect {
		return math.Inf(1), 1
	}
	var sum float64
	n := a.Rect.Dx() * a.Rect.Dy()
	off := 0
	for i := 0; i < len(a.Pix); i += 4 {
		worst := 0
		for k := 0; k < 4; k++ {
			d := int(a.Pix[i+k]) - int(b.Pix[i+k])
			if d < 0 {
				d = -d
			}
			sum += float64(d)
			worst = max(worst, d)
		}
		if worst > 64 {
			off++
		}
	}
	return sum / float64(4*n), float64(off) / float64(n)
}
