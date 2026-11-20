package htmlcss

import (
	"regexp"
	"strconv"
	"strings"

	"github.com/qizhanchan/qui"
)

func queryContainerStyle(cs *ComputedStyle) (bool, string) {
	kind, name := cs.raw["container-type"], cs.raw["container-name"]
	if value, ok := cs.raw["container"]; ok {
		parts := strings.SplitN(value, "/", 2)
		name = strings.TrimSpace(parts[0])
		if len(parts) == 2 {
			kind = strings.TrimSpace(parts[1])
		}
	}
	return kind == "inline-size" || kind == "size", name
}

func matchesContainers(n *Node, conditions []string) bool {
	for _, condition := range conditions {
		name := ""
		if i := strings.IndexByte(condition, '('); i > 0 {
			name = strings.TrimSpace(condition[:i])
		}
		var container *Node
		for p := n.Parent; p != nil; p = p.Parent {
			if !p.queryContainer {
				continue
			}
			if name != "" {
				found := false
				for _, candidate := range strings.Fields(p.containerNames) {
					if candidate == name {
						found = true
					}
				}
				if !found {
					continue
				}
			}
			container = p
			break
		}
		if container == nil || !containerMatches(condition, container.containerWidth) {
			return false
		}
	}
	return true
}

// Inline-size query containers resolve their descendants against the offered
// content width before measuring their height. This also runs for the final
// flex/grid width, after an intrinsic probe used a different width.
func (e *El) updateContainerWidth(offer float32, placed ...bool) {
	eng := e.engine
	used := len(placed) > 0 && placed[0]
	if eng == nil || eng.containerRestyling || !e.node.queryContainer || offer <= 0 || offer >= 1e6 || !used && (e.node.containerLaidOut || qui.IntrinsicWidthMeasurement()) {
		return
	}
	if used {
		e.node.containerLaidOut = true
	}
	st := e.Style()
	width := offer
	if st.Width > 0 {
		width = st.Width
	}
	if st.MaxWidth > 0 {
		width = min(width, st.MaxWidth)
	}
	width = max(0, width-st.ContentInsets().Horizontal())
	if absf(width-e.node.containerWidth) < .01 {
		return
	}
	changed := false
	for _, condition := range eng.sheet.ContainerConditions {
		if containerMatches(condition, width) != containerMatches(condition, e.node.containerWidth) {
			changed = true
			break
		}
	}
	e.node.containerWidth = width
	// Container units below resolve against this width too.
	changed = changed || e.node.containerUnitsUsed
	if !changed {
		return
	}
	eng.containerRestyling = true
	defer func() { eng.containerRestyling = false }()
	eng.pass++
	clear(eng.memo)
	for _, kid := range e.elementKids {
		if child, ok := kid.(*El); ok {
			eng.restyleWalk(child, e.lastCS)
		}
	}
	e.invalidateStyledLayout()
}

var reContainerUnit = regexp.MustCompile(`(?i)(^|[^a-z0-9_-])([+-]?(?:\d+\.?\d*|\.\d+))(cqw|cqh|cqi|cqb|cqmin|cqmax)\b`)

// resolveContainerUnits rewrites container query length units (cqw, cqi …)
// in n's declared values to px, against the nearest size query container
// above n — its content box, as last laid out. Without one (or before its
// first layout) they fall back to the small viewport units, as in CSS
// Containment 3 §6. GitHub sizes its hero media with
// `min(100cqw - 4rem, 71.25rem)`.
func resolveContainerUnits(n *Node, m map[string]string) map[string]string {
	var out map[string]string
	for k, v := range m {
		if !strings.Contains(v, "cq") || strings.HasPrefix(k, "--") || !reContainerUnit.MatchString(v) {
			continue
		}
		if out == nil {
			out = make(map[string]string, len(m))
			for kk, vv := range m {
				out[kk] = vv
			}
		}
		out[k] = containerUnitsToPx(n, v)
	}
	if out == nil {
		return m
	}
	return out
}

func containerUnitsToPx(n *Node, v string) string {
	w, h := viewportUnits[0], viewportUnits[1]
	for p := n.Parent; p != nil; p = p.Parent {
		if p.queryContainer {
			p.containerUnitsUsed = true
			if p.containerWidth > 0 {
				w = p.containerWidth
				// Only inline-size containers are tracked: block-axis
				// units fall back to the viewport.
			}
			break
		}
	}
	return reContainerUnit.ReplaceAllStringFunc(v, func(m string) string {
		sub := reContainerUnit.FindStringSubmatch(m)
		num, err := strconv.ParseFloat(sub[2], 32)
		if err != nil {
			return m
		}
		var ref float32
		switch strings.ToLower(sub[3]) {
		case "cqw", "cqi":
			ref = w
		case "cqh", "cqb":
			ref = h
		case "cqmin":
			ref = min(w, h)
		case "cqmax":
			ref = max(w, h)
		}
		return sub[1] + strconv.FormatFloat(float64(float32(num)/100*ref), 'f', -1, 32) + "px"
	})
}
