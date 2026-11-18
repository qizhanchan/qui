package qui

// Draw suppression: a retained layer that renders a subtree into its own
// buffer (ScrollView's content cache) leaves out the boxes it composites
// separately on top (fixed and sticky boxes). Draw implementations that
// can be such a box check DrawSuppressed first. UI thread only.

var drawSuppressed map[Widget]struct{}

// SuppressDraw makes DrawSuppressed report ws until ResumeDraw.
func SuppressDraw(ws []Widget) {
	if len(ws) == 0 {
		drawSuppressed = nil
		return
	}
	m := make(map[Widget]struct{}, len(ws))
	for _, w := range ws {
		m[w] = struct{}{}
	}
	drawSuppressed = m
}

// ResumeDraw ends SuppressDraw.
func ResumeDraw() { drawSuppressed = nil }

// DrawSuppressed reports whether w must not draw now.
func DrawSuppressed(w Widget) bool {
	if len(drawSuppressed) == 0 || w == nil {
		return false
	}
	_, ok := drawSuppressed[w]
	return ok
}
