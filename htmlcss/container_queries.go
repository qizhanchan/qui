package htmlcss

import (
	"github.com/qizhanchan/qui"
	"strings"
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
