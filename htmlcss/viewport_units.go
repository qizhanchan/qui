package htmlcss

import (
	"regexp"
	"strconv"
	"strings"
)

// Viewport-percentage lengths (vw, vh, vmin, vmax and the small / large /
// dynamic variants) resolve against the viewport the document was laid
// out for. Like @media, they are evaluated when declarations are parsed:
// a host that lets the viewport change re-renders (a browser does on
// resize), so substituting pixels up front keeps every length parser —
// calc(), min()/max(), shorthands — unaware of viewport units.

// viewportUnits is the viewport [width, height] the next parse resolves
// against; RenderDoc / NewStyleEngineViewportSize set it.
var viewportUnits = [2]float32{defaultViewportWidth, 800}

// SetViewportUnits sets the viewport size vw / vh units resolve against
// in stylesheets and style attributes parsed from now on.
func SetViewportUnits(w, h float32) {
	if w > 0 {
		viewportUnits[0] = w
	}
	if h > 0 {
		viewportUnits[1] = h
	}
}

var reViewportUnit = regexp.MustCompile(`(?i)(^|[^a-z0-9_-])([+-]?(?:\d+\.?\d*|\.\d+))(s|l|d)?(vw|vh|vmin|vmax|vi|vb)\b`)

// resolveViewportUnits rewrites viewport units in a declaration value to px.
func resolveViewportUnits(val string) string {
	if !strings.ContainsAny(val, "vV") || !reViewportUnit.MatchString(val) {
		return val
	}
	w, h := viewportUnits[0], viewportUnits[1]
	return reViewportUnit.ReplaceAllStringFunc(val, func(m string) string {
		sub := reViewportUnit.FindStringSubmatch(m)
		n, err := strconv.ParseFloat(sub[2], 32)
		if err != nil {
			return m
		}
		var ref float32
		switch strings.ToLower(sub[4]) {
		case "vw", "vi":
			ref = w
		case "vh", "vb":
			ref = h
		case "vmin":
			ref = min(w, h)
		case "vmax":
			ref = max(w, h)
		}
		px := float32(n) / 100 * ref
		return sub[1] + strconv.FormatFloat(float64(px), 'f', -1, 32) + "px"
	})
}
