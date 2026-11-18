package htmlcss

import (
	"strconv"
	"strings"
)

// Media Queries 4 / Conditional Rules / Container Queries evaluation.
// Everything is decided at parse time against the viewport the document
// is laid out for (a host re-renders on resize), for a screen in light
// mode with a fine hovering pointer — a desktop browser window.

// devicePixelRatio answers resolution queries (SetDevicePixelRatio).
var devicePixelRatio float32 = 1

// SetDevicePixelRatio sets the ratio `resolution` / `-webkit-device-pixel-
// ratio` media queries see.
func SetDevicePixelRatio(r float32) {
	if r > 0 {
		devicePixelRatio = r
	}
}

// mediaMatches evaluates a media query list: it matches when any
// comma-separated query does.
func mediaMatches(cond string, viewportW float32) bool {
	cond = strings.ToLower(strings.TrimSpace(cond))
	if cond == "" {
		return true
	}
	for _, q := range splitTopLevel(cond, ',') {
		if mediaQueryMatches(strings.TrimSpace(q), viewportW) {
			return true
		}
	}
	return false
}

// mediaQueryMatches evaluates one query: `[not|only] type [and cond]` or a
// bare condition.
func mediaQueryMatches(q string, viewportW float32) bool {
	if q == "" {
		return true
	}
	negate := false
	if rest, ok := strings.CutPrefix(q, "not "); ok && !strings.HasPrefix(strings.TrimSpace(rest), "(") {
		negate, q = true, strings.TrimSpace(rest)
	} else if rest, ok := strings.CutPrefix(q, "only "); ok {
		q = strings.TrimSpace(rest)
	}
	if !strings.HasPrefix(q, "(") && !strings.HasPrefix(q, "not ") {
		// A media type, optionally followed by `and <condition>`.
		typ, rest, _ := strings.Cut(q, " ")
		typeOK := typ == "screen" || typ == "all"
		rest = strings.TrimSpace(rest)
		if after, ok := strings.CutPrefix(rest, "and "); ok {
			rest = strings.TrimSpace(after)
		} else if rest != "" {
			return negate // malformed
		}
		ok := typeOK && (rest == "" || evalCondition(rest, viewportW, mediaFeature))
		return ok != negate
	}
	return evalCondition(q, viewportW, mediaFeature) != negate
}

// evalCondition evaluates `(a) and (b)`, `(a) or (b)`, `not (a)` with
// nested parentheses; feature answers one parenthesized feature test.
func evalCondition(c string, vw float32, feature func(string, float32) bool) bool {
	c = strings.TrimSpace(c)
	if rest, ok := strings.CutPrefix(c, "not "); ok {
		return !evalCondition(rest, vw, feature)
	}
	parts, ops := splitConditionTerms(c)
	if len(parts) == 0 {
		return false
	}
	result := evalTerm(parts[0], vw, feature)
	for i, op := range ops {
		v := evalTerm(parts[i+1], vw, feature)
		if op == "or" {
			result = result || v
		} else {
			result = result && v
		}
	}
	return result
}

func evalTerm(t string, vw float32, feature func(string, float32) bool) bool {
	t = strings.TrimSpace(t)
	if strings.HasPrefix(t, "(") && strings.HasSuffix(t, ")") {
		inner := strings.TrimSpace(t[1 : len(t)-1])
		if strings.HasPrefix(inner, "(") || strings.HasPrefix(inner, "not ") {
			return evalCondition(inner, vw, feature)
		}
		return feature(inner, vw)
	}
	if rest, ok := strings.CutPrefix(t, "not "); ok {
		return !evalTerm(rest, vw, feature)
	}
	return false
}

// splitConditionTerms splits at top-level " and " / " or ".
func splitConditionTerms(c string) (parts, ops []string) {
	depth, last := 0, 0
	for i := 0; i < len(c); i++ {
		switch c[i] {
		case '(':
			depth++
		case ')':
			depth--
		case ' ':
			if depth != 0 {
				continue
			}
			for _, op := range [...]string{" and ", " or "} {
				if strings.HasPrefix(c[i:], op) {
					parts = append(parts, c[last:i])
					ops = append(ops, strings.TrimSpace(op))
					last = i + len(op)
					i += len(op) - 1
					break
				}
			}
		}
	}
	parts = append(parts, c[last:])
	return parts, ops
}

// mediaFeature answers one media feature test (the text inside its
// parentheses): `min-width: 40em`, `width >= 600px`, `hover`, ….
func mediaFeature(f string, vw float32) bool {
	vh := viewportUnits[1]
	if name, val, ok := strings.Cut(f, ":"); ok {
		name, val = strings.TrimSpace(name), strings.TrimSpace(val)
		switch name {
		case "min-width":
			return cmpLen(vw, ">=", val)
		case "max-width":
			return cmpLen(vw, "<=", val)
		case "width":
			return cmpLen(vw, "=", val)
		case "min-height":
			return cmpLen(vh, ">=", val)
		case "max-height":
			return cmpLen(vh, "<=", val)
		case "height":
			return cmpLen(vh, "=", val)
		case "orientation":
			if vw >= vh {
				return val == "landscape"
			}
			return val == "portrait"
		case "prefers-color-scheme":
			return val == "light"
		case "prefers-reduced-motion", "prefers-reduced-transparency", "prefers-contrast", "prefers-reduced-data":
			return val == "no-preference"
		case "forced-colors", "inverted-colors":
			return val == "none"
		case "hover", "any-hover":
			return val == "hover"
		case "pointer", "any-pointer":
			return val == "fine"
		case "scripting":
			return val == "none"
		case "display-mode":
			return val == "browser"
		case "update":
			return val == "fast"
		case "color-gamut":
			return val == "srgb"
		case "dynamic-range", "video-dynamic-range":
			return val == "standard"
		case "min-resolution", "max-resolution", "resolution":
			r := parseResolution(val)
			switch name {
			case "min-resolution":
				return devicePixelRatio >= r
			case "max-resolution":
				return devicePixelRatio <= r
			}
			return devicePixelRatio == r
		case "-webkit-min-device-pixel-ratio", "min--moz-device-pixel-ratio":
			r, _ := strconv.ParseFloat(val, 32)
			return devicePixelRatio >= float32(r)
		case "-webkit-max-device-pixel-ratio":
			r, _ := strconv.ParseFloat(val, 32)
			return devicePixelRatio <= float32(r)
		case "min-aspect-ratio", "max-aspect-ratio", "aspect-ratio":
			ar := parseRatio(val)
			if ar <= 0 {
				return false
			}
			cur := vw / vh
			switch name {
			case "min-aspect-ratio":
				return cur >= ar
			case "max-aspect-ratio":
				return cur <= ar
			}
			return cur == ar
		case "color":
			n, _ := strconv.Atoi(val)
			return n <= 8
		case "monochrome":
			return val == "0"
		}
		return false
	}
	switch strings.TrimSpace(f) {
	case "color", "hover", "any-hover", "pointer", "any-pointer", "width", "height", "grid", "update":
		return f != "grid"
	case "monochrome", "inverted-colors", "prefers-reduced-motion", "prefers-contrast", "forced-colors":
		return false
	}
	return rangeFeature(f, vw, vh)
}

// rangeFeature evaluates range syntax: `width >= 600px`, `600px < width`,
// `400px <= width <= 700px` (also for height and aspect-ratio).
func rangeFeature(f string, vw, vh float32) bool {
	toks := splitRange(f)
	value := func(name string) (float32, bool) {
		switch name {
		case "width", "inline-size":
			return vw, true
		case "height", "block-size":
			return vh, true
		}
		return 0, false
	}
	switch len(toks) {
	case 3:
		if v, ok := value(toks[0]); ok {
			return cmpLen(v, toks[1], toks[2])
		}
		if v, ok := value(toks[2]); ok {
			return cmpLen(v, flipOp(toks[1]), toks[0])
		}
	case 5:
		if v, ok := value(toks[2]); ok {
			return cmpLen(v, flipOp(toks[1]), toks[0]) && cmpLen(v, toks[3], toks[4])
		}
	}
	return false
}

// splitRange tokenizes a range expression into operands and operators.
func splitRange(f string) []string {
	var out []string
	i := 0
	for i < len(f) {
		for i < len(f) && f[i] == ' ' {
			i++
		}
		if i >= len(f) {
			break
		}
		if f[i] == '<' || f[i] == '>' || f[i] == '=' {
			j := i + 1
			if j < len(f) && f[j] == '=' {
				j++
			}
			out = append(out, f[i:j])
			i = j
			continue
		}
		j := i
		for j < len(f) && f[j] != ' ' && f[j] != '<' && f[j] != '>' && f[j] != '=' {
			j++
		}
		out = append(out, f[i:j])
		i = j
	}
	return out
}

func flipOp(op string) string {
	switch op {
	case "<":
		return ">"
	case "<=":
		return ">="
	case ">":
		return "<"
	case ">=":
		return "<="
	}
	return op
}

// cmpLen compares v with a CSS length (em/rem against 16px, as media
// queries do).
func cmpLen(v float32, op, length string) bool {
	l, ok := parseLength(length, 16, 0)
	if !ok {
		return false
	}
	switch op {
	case ">=":
		return v >= l
	case "<=":
		return v <= l
	case ">":
		return v > l
	case "<":
		return v < l
	case "=":
		return v == l
	}
	return false
}

func parseResolution(v string) float32 {
	switch {
	case strings.HasSuffix(v, "dppx"):
		f, _ := strconv.ParseFloat(strings.TrimSuffix(v, "dppx"), 32)
		return float32(f)
	case strings.HasSuffix(v, "dpi"):
		f, _ := strconv.ParseFloat(strings.TrimSuffix(v, "dpi"), 32)
		return float32(f / 96)
	case strings.HasSuffix(v, "dpcm"):
		f, _ := strconv.ParseFloat(strings.TrimSuffix(v, "dpcm"), 32)
		return float32(f * 2.54 / 96)
	case strings.HasSuffix(v, "x"):
		f, _ := strconv.ParseFloat(strings.TrimSuffix(v, "x"), 32)
		return float32(f)
	}
	return 0
}

func parseRatio(v string) float32 {
	a, b, ok := strings.Cut(v, "/")
	x, err := strconv.ParseFloat(strings.TrimSpace(a), 32)
	if err != nil {
		return 0
	}
	if !ok {
		return float32(x)
	}
	y, err := strconv.ParseFloat(strings.TrimSpace(b), 32)
	if err != nil || y == 0 {
		return 0
	}
	return float32(x / y)
}

// supportsMatches evaluates an @supports condition. A declaration test
// `(prop: value)` holds when the property is one htmlcss knows or a
// widely supported one; selector() tests hold; `not` negates. Erring
// toward "supported" matches Chrome, which supports nearly everything a
// stylesheet feature-tests.
func supportsMatches(cond string) bool {
	cond = strings.ToLower(strings.TrimSpace(cond))
	return evalCondition(cond, 0, func(f string, _ float32) bool {
		if strings.HasPrefix(f, "selector(") {
			return true
		}
		prop, _, ok := strings.Cut(f, ":")
		if !ok {
			return true
		}
		prop = strings.TrimSpace(prop)
		switch prop {
		case "-webkit-touch-callout", "-ms-ime-align", "-moz-appearance", "-ms-high-contrast":
			return false // engine sniffs for other browsers
		}
		return true
	})
}

// containerMatches evaluates an @container prelude (`[name] (cond)`)
// against the viewport — an upper bound for any container's size.
func containerMatches(prelude string, vw float32) bool {
	p := strings.ToLower(strings.TrimSpace(prelude))
	if i := strings.IndexByte(p, '('); i > 0 {
		p = strings.TrimSpace(p[i:])
	}
	if p == "" {
		return true
	}
	return evalCondition(p, vw, mediaFeature)
}
