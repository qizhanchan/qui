package htmlcss

import (
	"regexp"
	"strconv"
	"strings"
	"sync/atomic"
)

// Viewport-percentage lengths (vw, vh, vmin, vmax and the small / large /
// dynamic variants) resolve against the viewport the document was laid
// out for. Like @media, they are evaluated when declarations are parsed:
// a host that lets the viewport change re-renders (a browser does on
// resize), so substituting pixels up front keeps every length parser —
// calc(), min()/max(), shorthands — unaware of viewport units.

// viewportSize is a viewport [width, height] in CSS px.
type viewportSize [2]float32

// defaultViewportUnits is what a parse without a document (ParseCSS, a
// detached element) resolves vw / vh against. Documents use their own
// engine's viewport (StyleEngine.viewport), so pages styled on different
// threads never share it (RFC 0005).
var defaultViewportUnits atomic.Pointer[viewportSize]

func defaultViewport() viewportSize {
	if p := defaultViewportUnits.Load(); p != nil {
		return *p
	}
	return viewportSize{defaultViewportWidth, 800}
}

// SetViewportUnits sets the default viewport size vw / vh units resolve
// against outside a document's style engine.
func SetViewportUnits(w, h float32) {
	v := defaultViewport()
	if w > 0 {
		v[0] = w
	}
	if h > 0 {
		v[1] = h
	}
	defaultViewportUnits.Store(&v)
}

// nodeViewport is the viewport of the document n belongs to.
func nodeViewport(n *Node) viewportSize {
	if n != nil && n.live != nil && n.live.engine != nil {
		return n.live.engine.viewport()
	}
	return defaultViewport()
}

var reViewportUnit = regexp.MustCompile(`(?i)(^|[^a-z0-9_-])([+-]?(?:\d+\.?\d*|\.\d+))(s|l|d)?(vw|vh|vmin|vmax|vi|vb)\b`)

// resolveViewportUnits rewrites viewport units in a declaration value to px.
func resolveViewportUnits(val string, vp viewportSize) string {
	if !strings.ContainsAny(val, "vV") || !reViewportUnit.MatchString(val) {
		return val
	}
	w, h := vp[0], vp[1]
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
