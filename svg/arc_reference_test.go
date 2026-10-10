package svg_test

import (
	"fmt"
	"math"
	"strings"
	"testing"
)

// refArcPolyline converts an SVG endpoint arc to its centre form exactly as
// the SVG 1.1 implementation notes (F.6.5 / F.6.6) prescribe and returns the
// arc sampled as points from (x1,y1) to (x2,y2).
func refArcPolyline(x1, y1, rx, ry, phiDeg float64, large, sweep bool, x2, y2 float64) [][2]float64 {
	rx, ry = math.Abs(rx), math.Abs(ry)
	phi := phiDeg * math.Pi / 180
	cosP, sinP := math.Cos(phi), math.Sin(phi)
	dx, dy := (x1-x2)/2, (y1-y2)/2
	x1p := cosP*dx + sinP*dy
	y1p := -sinP*dx + cosP*dy
	// F.6.6: scale radii up when they cannot span the endpoints.
	if l := x1p*x1p/(rx*rx) + y1p*y1p/(ry*ry); l > 1 {
		s := math.Sqrt(l)
		rx, ry = rx*s, ry*s
	}
	num := rx*rx*ry*ry - rx*rx*y1p*y1p - ry*ry*x1p*x1p
	den := rx*rx*y1p*y1p + ry*ry*x1p*x1p
	co := 0.0
	if num > 0 {
		co = math.Sqrt(num / den)
	}
	if large == sweep {
		co = -co
	}
	cxp := co * rx * y1p / ry
	cyp := -co * ry * x1p / rx
	cx := cosP*cxp - sinP*cyp + (x1+x2)/2
	cy := sinP*cxp + cosP*cyp + (y1+y2)/2
	ang := func(ux, uy, vx, vy float64) float64 {
		a := math.Atan2(ux*vy-uy*vx, ux*vx+uy*vy)
		return a
	}
	th1 := ang(1, 0, (x1p-cxp)/rx, (y1p-cyp)/ry)
	dth := ang((x1p-cxp)/rx, (y1p-cyp)/ry, (-x1p-cxp)/rx, (-y1p-cyp)/ry)
	if !sweep && dth > 0 {
		dth -= 2 * math.Pi
	} else if sweep && dth < 0 {
		dth += 2 * math.Pi
	}
	const n = 360
	pts := make([][2]float64, 0, n+1)
	for i := 0; i <= n; i++ {
		t := th1 + dth*float64(i)/n
		ex, ey := rx*math.Cos(t), ry*math.Sin(t)
		pts = append(pts, [2]float64{cosP*ex - sinP*ey + cx, sinP*ex + cosP*ey + cy})
	}
	return pts
}

// alphaDiff rasterises two paths on a 24x24 viewBox and reports how many
// pixels differ by more than a quarter of full coverage.
func alphaDiff(t *testing.T, a, b string, size int) (diff, covered int) {
	t.Helper()
	wrap := func(d string) string {
		return `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24"><path d="` + d + `" fill="black"/></svg>`
	}
	ra, rb := rasterAlpha(t, wrap(a), size), rasterAlpha(t, wrap(b), size)
	for y := 0; y < size; y++ {
		for x := 0; x < size; x++ {
			va, vb := int(ra(x, y)), int(rb(x, y))
			if va > 0x4000 || vb > 0x4000 {
				covered++
			}
			if d := va - vb; d > 0x4000 || d < -0x4000 {
				diff++
			}
		}
	}
	return
}

// TestArcMatchesSpecReference draws the filled segment cut off by an arc
// (M start A ... end Z) and compares it with a polygon sampled from the
// spec's centre parameterisation. It covers all four large-arc/sweep
// combinations, x-axis rotation, radii that are too small for the chord
// (scaled up per F.6.6), the semicircle edge case and a rotated ellipse.
func TestArcMatchesSpecReference(t *testing.T) {
	type arc struct {
		x1, y1, rx, ry, rot float64
		x2, y2              float64
	}
	arcs := []arc{
		{4, 12, 8, 8, 0, 12, 4},    // quarter of a circle
		{6, 18, 9, 5, 0, 19, 8},    // axis-aligned ellipse, off-axis chord
		{6, 18, 9, 5, 30, 19, 8},   // rotated
		{5, 17, 7, 4, -50, 18, 6},  // rotated the other way
		{4, 12, 1, 1, 0, 20, 12},   // radii far too small: exactly a semicircle
		{4, 14, 2, 6, 0, 20, 8},    // too small and anisotropic
		{4, 12, 8, 8, 0, 20, 12},   // semicircle with matching radii
		{7, 7, 6, 6, 0, 17, 17},    // chord shorter than the diameter
		{3, 20, 40, 10, 20, 21, 4}, // large rx, rotated
	}
	for i, a := range arcs {
		for _, large := range []bool{false, true} {
			for _, sweep := range []bool{false, true} {
				l, s := 0, 0
				if large {
					l = 1
				}
				if sweep {
					s = 1
				}
				got := fmt.Sprintf("M%g %gA%g %g %g %d %d %g %gZ", a.x1, a.y1, a.rx, a.ry, a.rot, l, s, a.x2, a.y2)
				var ref strings.Builder
				fmt.Fprintf(&ref, "M%g %g", a.x1, a.y1)
				for _, p := range refArcPolyline(a.x1, a.y1, a.rx, a.ry, a.rot, large, sweep, a.x2, a.y2) {
					fmt.Fprintf(&ref, "L%.4f %.4f", p[0], p[1])
				}
				ref.WriteString("Z")
				diff, covered := alphaDiff(t, got, ref.String(), 96)
				if covered == 0 {
					t.Errorf("arc %d %q: nothing drawn", i, got)
					continue
				}
				// Edge pixels may differ by anti-aliasing; a wrong centre
				// or flag differs by a large fraction of the shape.
				if float64(diff) > 0.01*float64(covered)+4 {
					t.Errorf("arc %d %q: %d of %d pixels differ from the spec reference", i, got, diff, covered)
				}
			}
		}
	}
}

// TestArcCircleFromTwoArcsMatchesEllipse: the usual "circle as two arcs"
// idiom (and a relative-command, compact-flag spelling of it) renders the
// same as <circle> and <ellipse>, also under x-axis rotation.
func TestArcCircleFromTwoArcsMatchesEllipse(t *testing.T) {
	doc := func(body string) string {
		return `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24">` + body + `</svg>`
	}
	const size = 96
	circle := rasterAlpha(t, doc(`<circle cx="12" cy="12" r="8" fill="black"/>`), size)
	cases := map[string]string{
		"absolute":       `M4 12A8 8 0 1 0 20 12A8 8 0 1 0 4 12Z`,
		"relative":       `M12 4a8 8 0 1 0 0 16a8 8 0 1 0 0-16z`,
		"compact flags":  `M4 12a8 8 0 1016 0a8 8 0 10-16 0z`,
		"small radii":    `M4 12A1 1 0 0 0 20 12A1 1 0 0 0 4 12Z`,
		"rotated circle": `M4 12A8 8 45 1 0 20 12A8 8 45 1 0 4 12Z`,
	}
	for name, d := range cases {
		got := rasterAlpha(t, doc(`<path d="`+d+`" fill="black"/>`), size)
		diff := 0
		for y := 0; y < size; y++ {
			for x := 0; x < size; x++ {
				if dd := int(got(x, y)) - int(circle(x, y)); dd > 0x4000 || dd < -0x4000 {
					diff++
				}
			}
		}
		if diff > 20 {
			t.Errorf("%s: %d pixels differ from <circle>", name, diff)
		}
	}

	// An ellipse (rx 10, ry 4) turned 45 degrees: <ellipse> under a rotate
	// transform vs two arcs carrying the rotation as the x-axis angle.
	// Endpoints are the ellipse's major-axis ends: centre (12,12) +- 10 along 45 deg.
	ell := rasterAlpha(t, doc(`<ellipse cx="12" cy="12" rx="10" ry="4" transform="rotate(45 12 12)" fill="black"/>`), size)
	k := 10 / math.Sqrt2
	arcs := rasterAlpha(t, doc(fmt.Sprintf(`<path d="M%.5f %.5fA10 4 45 1 1 %.5f %.5fA10 4 45 1 1 %.5f %.5fZ" fill="black"/>`,
		12-k, 12-k, 12+k, 12+k, 12-k, 12-k)), size)
	diff := 0
	for y := 0; y < size; y++ {
		for x := 0; x < size; x++ {
			if dd := int(arcs(x, y)) - int(ell(x, y)); dd > 0x4000 || dd < -0x4000 {
				diff++
			}
		}
	}
	if diff > 30 {
		t.Errorf("rotated ellipse from arcs: %d pixels differ from <ellipse>", diff)
	}
}

// A negative radius is its absolute value, not a degenerate arc.
func TestArcNegativeRadiusIsAbsolute(t *testing.T) {
	wrap := func(d string) string {
		return `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24"><path d="` + d + `" fill="black"/></svg>`
	}
	a := rasterAlpha(t, wrap(`M4 12A8 8 0 0 1 20 12Z`), 48)
	b := rasterAlpha(t, wrap(`M4 12A-8 -8 0 0 1 20 12Z`), 48)
	for y := 0; y < 48; y++ {
		for x := 0; x < 48; x++ {
			if a(x, y) != b(x, y) {
				t.Fatalf("pixel (%d,%d) differs: %#x vs %#x", x, y, a(x, y), b(x, y))
			}
		}
	}
}
