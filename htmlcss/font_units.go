package htmlcss

import (
	"regexp"
	"strconv"
	"strings"

	"github.com/qizhanchan/qui"
)

// Font-relative lengths that depend on the face, not just its size:
// ch is the advance of "0", ex the x-height (CSS Values 4 §6.1.1). They
// resolve to px once the element's font is known, like rem and viewport
// units, so every length parser stays unaware of them. Headlines on
// stripe.com are `max-width: 14ch`.

var reFontUnit = regexp.MustCompile(`(?i)(^|[^a-z0-9_.-])([+-]?(?:\d+\.?\d*|\.\d+))(ch|ex|ic|cap)\b`)

// resolveFontUnits rewrites ch / ex (and ic / cap, approximated) in a
// declaration value to px for the font f.
func resolveFontUnits(val string, f qui.Font) string {
	if !reFontUnit.MatchString(val) {
		return val
	}
	var ch, ex float32
	measured := false
	measure := func() {
		if measured {
			return
		}
		measured = true
		ch = qui.RuneAdvance('0', f)
		if ch <= 0 {
			ch = f.Size / 2 // the spec's fallback
		}
		if m := qui.FontMetricsFor(f); m.XHeight > 0 {
			ex = float32(m.XHeight) / 64
		} else {
			ex = f.Size / 2
		}
	}
	return reFontUnit.ReplaceAllStringFunc(val, func(m string) string {
		sub := reFontUnit.FindStringSubmatch(m)
		n, err := strconv.ParseFloat(sub[2], 32)
		if err != nil {
			return m
		}
		measure()
		var unit float32
		switch strings.ToLower(sub[3]) {
		case "ch":
			unit = ch
		case "ex":
			unit = ex
		case "ic":
			unit = f.Size // a CJK ideograph is about 1em wide
		case "cap":
			unit = ex * 1.4
		}
		return sub[1] + strconv.FormatFloat(float64(float32(n)*unit), 'f', -1, 32) + "px"
	})
}
