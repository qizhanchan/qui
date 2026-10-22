package htmlcss

import "strings"

// expandShorthands rewrites a few CSS shorthands into the longhand
// properties the interpreter already understands, so the rest of interpret
// stays simple. Longhands the author set explicitly are never overwritten
// (setIfAbsent), matching CSS shorthand-then-longhand precedence within a
// single declaration block being source-ordered anyway.
func expandShorthands(m map[string]string) {
	if v, ok := m["font"]; ok {
		expandFont(v, m)
	}
	if v, ok := m["flex"]; ok {
		expandFlex(v, m)
	}
	if v, ok := m["inset"]; ok {
		expandInset(v, m)
	}
}

func setIfAbsent(m map[string]string, key, val string) {
	if val == "" {
		return
	}
	if _, has := m[key]; !has {
		m[key] = val
	}
}

// expandFont expands `font: [style] [weight] size[/line-height] family`.
// The size token is the first field that parses as a length; fields before
// it are style/weight, fields after it are the family.
func expandFont(v string, m map[string]string) {
	fields := strings.Fields(v)
	sizeIdx := -1
	for i, f := range fields {
		base := f
		if idx := strings.IndexByte(f, '/'); idx >= 0 {
			base = f[:idx]
		}
		if _, ok := parseLength(base, 16, 0); ok && strings.ContainsAny(base, "0123456789") {
			sizeIdx = i
			break
		}
	}
	if sizeIdx < 0 {
		return // not a valid font shorthand (e.g. a keyword like `inherit`)
	}
	for _, pre := range fields[:sizeIdx] {
		low := strings.ToLower(pre)
		switch low {
		case "italic", "oblique":
			setIfAbsent(m, "font-style", "italic")
		case "bold", "bolder", "lighter", "normal":
			setIfAbsent(m, "font-weight", low)
		case "small-caps", "variant":
			// ignored
		default:
			if _, ok := parseFloat(pre); ok {
				setIfAbsent(m, "font-weight", pre) // numeric weight
			}
		}
	}
	sizeField := fields[sizeIdx]
	size, lh := sizeField, ""
	if idx := strings.IndexByte(sizeField, '/'); idx >= 0 {
		size, lh = sizeField[:idx], sizeField[idx+1:]
	}
	setIfAbsent(m, "font-size", size)
	setIfAbsent(m, "line-height", lh)
	if sizeIdx+1 < len(fields) {
		setIfAbsent(m, "font-family", strings.Join(fields[sizeIdx+1:], " "))
	}
}

// expandFlex expands `flex: <grow> [shrink] [basis]` into its longhands.
// Keyword forms: `none` = 0 0 auto; `auto` = 1 1 auto; `initial` = 0 1 auto.
// Omitted parts take the shorthand's defaults, not the longhands'
// initial values: grow and shrink 1, basis 0 — so `flex: 1` is `1 1 0`
// and `flex: 200px` is `1 1 200px`. The basis may sit in any position.
func expandFlex(v string, m map[string]string) {
	v = strings.ToLower(strings.TrimSpace(v))
	grow, shrink, basis := "", "", ""
	switch v {
	case "none":
		grow, shrink, basis = "0", "0", "auto"
	case "auto":
		grow, shrink, basis = "1", "1", "auto"
	case "initial":
		grow, shrink, basis = "0", "1", "auto"
	default:
		for _, f := range fieldsTopLevel(v) {
			if _, ok := parseFloat(f); ok && (grow == "" || shrink == "") {
				if grow == "" {
					grow = f
				} else {
					shrink = f
				}
				continue
			}
			if basis != "" {
				return // two bases: invalid, ignore the declaration
			}
			basis = f
		}
		if grow == "" && basis == "" {
			return
		}
		if grow == "" {
			grow = "1"
		}
		if shrink == "" {
			shrink = "1"
		}
		if basis == "" {
			basis = "0"
		}
	}
	setIfAbsent(m, "flex-grow", grow)
	setIfAbsent(m, "flex-shrink", shrink)
	setIfAbsent(m, "flex-basis", basis)
}

// expandInset expands `inset: <t> [r] [b] [l]` (CSS logical shorthand for
// top/right/bottom/left) using the standard 1–4 value rule.
func expandInset(v string, m map[string]string) {
	f := strings.Fields(v)
	var t, r, b, l string
	switch len(f) {
	case 1:
		t, r, b, l = f[0], f[0], f[0], f[0]
	case 2:
		t, b, r, l = f[0], f[0], f[1], f[1]
	case 3:
		t, r, l, b = f[0], f[1], f[1], f[2]
	case 4:
		t, r, b, l = f[0], f[1], f[2], f[3]
	default:
		return
	}
	setIfAbsent(m, "top", t)
	setIfAbsent(m, "right", r)
	setIfAbsent(m, "bottom", b)
	setIfAbsent(m, "left", l)
}

// transformText applies CSS text-transform to a string.
func transformText(s, transform string) string {
	switch transform {
	case "uppercase":
		return strings.ToUpper(s)
	case "lowercase":
		return strings.ToLower(s)
	case "capitalize":
		return capitalizeWords(s)
	}
	return s
}

func capitalizeWords(s string) string {
	out := []rune(s)
	atStart := true
	for i, r := range out {
		if r == ' ' || r == '\t' || r == '\n' {
			atStart = true
			continue
		}
		if atStart {
			out[i] = []rune(strings.ToUpper(string(r)))[0]
			atStart = false
		}
	}
	return string(out)
}
