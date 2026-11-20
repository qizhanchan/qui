package htmlcss

import (
	"fmt"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"

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
	if eng == nil || eng.containerRestyling || !e.node.queryContainer || offer <= 0 || offer >= 1e6 || !used && (e.node.containerLaidOut || qui.IntrinsicWidthMeasurement(e)) {
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
	prevWidth := e.node.containerWidth
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
	if debugStyle {
		t0, st, ap := time.Now(), eng.styled, eng.applied
		defer func() {
			fmt.Fprintf(os.Stderr, "style: container <%s> %.1f->%.1f placed=%v styled=%d applied=%d in %v\n",
				e.tag, prevWidth, width, used, eng.styled-st, eng.applied-ap, time.Since(t0))
		}()
	}
	// Layout probes a container at several widths before placing it (a
	// flex/grid track sized from its content, then the final width), and
	// may go back and forth between them. Two things keep that cheap:
	// descendants whose style comes out unchanged keep their widgets (the
	// scoped walk's skip), and the cascade results for a set of matched
	// conditions are remembered, so returning to a width already seen
	// recomputes nothing — until any other style change (styleGen) or a
	// query container inside the subtree makes them unsafe to reuse.
	sig := containerSignature(eng.sheet.ContainerConditions, width, e.node.containerUnitsUsed)
	prevScoped, prevMust := eng.scoped, eng.mustApply
	defer func() {
		eng.containerRestyling = false
		eng.scoped, eng.mustApply = prevScoped, prevMust
	}()
	prevSig := containerSignature(eng.sheet.ContainerConditions, prevWidth, e.node.containerUnitsUsed)
	if c := e.node.containerStyleCache; c != nil && c.gen == eng.styleGen && c.equiv[[2]string{prevSig, sig}] {
		return // switching between these two changed no style last time
	}
	applied0 := eng.applied
	eng.pass++
	clear(eng.memo)
	eng.memoPass = eng.pass
	if eng.memo == nil {
		eng.memo = map[cascadeMemoKey]*ComputedStyle{}
	}
	cache := e.node.containerStyleCache
	if cache != nil && cache.gen == eng.styleGen && !cache.nested {
		if m, ok := cache.bySig[sig]; ok {
			for k, v := range m {
				eng.memo[k] = v
			}
		}
	} else {
		cache = &containerStyleCache{gen: eng.styleGen, bySig: map[string]map[cascadeMemoKey]*ComputedStyle{}}
		e.node.containerStyleCache = cache
	}
	eng.scoped, eng.mustApply = true, map[*El]bool{}
	for _, kid := range e.elementKids {
		if child, ok := kid.(*El); ok {
			eng.restyleWalk(child, e.lastCS)
		}
	}
	if !cache.nested {
		cache.nested = subtreeHasQueryContainer(e)
	}
	if eng.applied == applied0 && prevSig != sig {
		if cache.equiv == nil {
			cache.equiv = map[[2]string]bool{}
		}
		cache.equiv[[2]string{prevSig, sig}] = true
		cache.equiv[[2]string{sig, prevSig}] = true
	}
	if !cache.nested && len(cache.bySig) < 8 {
		m := make(map[cascadeMemoKey]*ComputedStyle, len(eng.memo))
		for k, v := range eng.memo {
			m[k] = v
		}
		cache.bySig[sig] = m
	}
	e.invalidateStyledLayout()
}

// containerStyleCache remembers, per set of matched container conditions,
// the cascade results of a query container's subtree (updateContainerWidth).
type containerStyleCache struct {
	gen    uint64 // StyleEngine.styleGen it was filled under
	nested bool   // the subtree holds another query container: never reuse
	bySig  map[string]map[cascadeMemoKey]*ComputedStyle
	// equiv holds signature pairs a switch between which re-applied
	// nothing in this generation.
	equiv map[[2]string]bool
}

// containerSignature names what a container's descendants can see of its
// width: which conditions hold, and the width itself when container units
// resolve against it.
func containerSignature(conditions []string, width float32, units bool) string {
	b := make([]byte, 0, len(conditions)+8)
	for _, c := range conditions {
		if containerMatches(c, width) {
			b = append(b, '1')
		} else {
			b = append(b, '0')
		}
	}
	if units {
		b = strconv.AppendFloat(append(b, '@'), float64(width), 'f', 2, 32)
	}
	return string(b)
}

func subtreeHasQueryContainer(e *El) bool {
	for _, kid := range e.elementKids {
		if k, ok := kid.(*El); ok && k.node != nil {
			if k.node.queryContainer || subtreeHasQueryContainer(k) {
				return true
			}
		}
	}
	return false
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
	vp := nodeViewport(n)
	w, h := vp[0], vp[1]
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
