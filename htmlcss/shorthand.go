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
func expandFlex(v string, m map[string]string) {
	v = strings.ToLower(strings.TrimSpace(v))
	switch v {
	case "none":
		setIfAbsent(m, "flex-grow", "0")
		setIfAbsent(m, "flex-shrink", "0")
		return
	case "auto":
		setIfAbsent(m, "flex-grow", "1")
		setIfAbsent(m, "flex-shrink", "1")
		return
	case "initial":
		return
	}
	fields := strings.Fields(v)
	// A single unitless value is flex-grow; a single value with a unit is
	// flex-basis (CSS `flex: <basis>`).
	if len(fields) == 1 {
		if _, ok := parseFloat(fields[0]); ok {
			setIfAbsent(m, "flex-grow", fields[0])
		} else {
			setIfAbsent(m, "flex-basis", fields[0])
		}
		return
	}
	if len(fields) >= 1 {
		if _, ok := parseFloat(fields[0]); ok {
			setIfAbsent(m, "flex-grow", fields[0])
		}
	}
	if len(fields) >= 2 {
		if _, ok := parseFloat(fields[1]); ok {
			setIfAbsent(m, "flex-shrink", fields[1])
		}
	}
	if len(fields) >= 3 {
		setIfAbsent(m, "flex-basis", fields[2])
	}
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
