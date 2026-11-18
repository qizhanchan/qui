package htmlcss

import (
	"strconv"
	"strings"

	"github.com/qizhanchan/qui"
)

// gridLayout builds a qui.GridLayout from the element's grid CSS. Columns
// come from grid-template-columns (px / fr / auto / repeat()), rows from
// grid-template-rows (or the row count grid-template-areas implies). Items
// placed past those tracks — or auto-placed beyond the last row — get
// implicit tracks sized by grid-auto-columns / grid-auto-rows.
func gridLayout(cs *ComputedStyle, itemCount int) qui.Layout {
	// grid-template-areas can imply the track counts when explicit templates
	// are omitted (each area row is a grid row; area columns are grid columns).
	areaRows, areaCols := gridAreaDims(cs.raw["grid-template-areas"])

	cols, autoRepeat := parseGridTrackList(cs.raw["grid-template-columns"], cs.FontSize)
	if len(cols) == 0 && autoRepeat == nil {
		n := areaCols
		if n < 1 {
			n = 1 // single implicit column
		}
		cols = make([]qui.GridTrack, n)
		for i := range cols {
			cols[i] = qui.GridFractionTrack(1)
		}
	}
	rows := parseGridTracks(cs.raw["grid-template-rows"], cs.FontSize)
	if len(rows) == 0 && areaRows > 0 {
		rows = make([]qui.GridTrack, areaRows)
		for i := range rows {
			rows[i] = qui.GridAutoTrack()
		}
	}
	rowGap, colGap := cs.RowGap, cs.ColumnGap
	return qui.GridLayout{
		Columns: cols, Rows: rows,
		AutoColumns: parseGridTracks(cs.raw["grid-auto-columns"], cs.FontSize),
		AutoRows:    parseGridTracks(cs.raw["grid-auto-rows"], cs.FontSize),
		ColGap:      colGap, RowGap: rowGap,
		AutoRepeat:         autoRepeat,
		SizedItemsKeepSize: true,
		AlignContent:       cs.AlignContent,
	}
}

// gridAreaDims returns the row and column counts implied by a
// grid-template-areas value (0,0 when absent).
func gridAreaDims(v string) (rows, cols int) {
	quoted := extractQuoted(v)
	rows = len(quoted)
	for _, q := range quoted {
		if n := len(strings.Fields(q)); n > cols {
			cols = n
		}
	}
	return rows, cols
}

// applyGridItem sets this element's explicit grid placement from grid-column /
// grid-row / grid-area, given its grid parent's computed style. Runs during the
// element's OWN applyComputed (the parent — computed first, top-down — already
// resolved its grid-template-areas). Line numbers are CSS 1-based; GridItem is
// 0-based. When only spans are known (no definite start) the item stays
// auto-placed but keeps its span.
func (e *El) applyGridItem(cs, parentCS *ComputedStyle) {
	colStart, colSpan := -1, 1
	rowStart, rowSpan := -1, 1

	// grid-area: a named area (from parent grid-template-areas) or line numbers
	// row-start / col-start / row-end / col-end.
	if ga := strings.TrimSpace(cs.raw["grid-area"]); ga != "" {
		if !strings.Contains(ga, "/") {
			if area, ok := parseGridAreas(parentCS.raw["grid-template-areas"])[ga]; ok {
				rowStart, colStart = area[0], area[1]
				rowSpan, colSpan = area[2], area[3]
			}
		} else {
			parts := splitTopLevel(ga, '/')
			if len(parts) >= 2 {
				rowStart, rowSpan = gridLineStartSpan(parts[0], parts, 2)
				colStart, colSpan = gridLineStartSpan(parts[1], parts, 3)
			}
		}
	}
	colNames := gridLineNames(parentCS.raw["grid-template-columns"], parentCS.raw["grid-template-areas"], false)
	rowNames := gridLineNames(parentCS.raw["grid-template-rows"], parentCS.raw["grid-template-areas"], true)
	if v := strings.TrimSpace(cs.raw["grid-column"]); v != "" {
		colStart, colSpan = parseGridPlacementNamed(v, colNames)
	}
	if v := strings.TrimSpace(cs.raw["grid-row"]); v != "" {
		rowStart, rowSpan = parseGridPlacementNamed(v, rowNames)
	}
	if v := strings.TrimSpace(cs.raw["grid-column-start"]); v != "" {
		end := strings.TrimSpace(cs.raw["grid-column-end"])
		colStart, colSpan = parseGridPlacementNamed(v+" / "+orAuto(end), colNames)
	} else if v := strings.TrimSpace(cs.raw["grid-column-end"]); v != "" && colStart < 0 {
		if n, ok := resolveGridLine(v, colNames, true); ok && n > 0 {
			colStart = max(n-colSpan, 0)
		}
	}
	if v := strings.TrimSpace(cs.raw["grid-row-start"]); v != "" {
		end := strings.TrimSpace(cs.raw["grid-row-end"])
		rowStart, rowSpan = parseGridPlacementNamed(v+" / "+orAuto(end), rowNames)
	}

	gi := qui.GridItem{ColSpan: colSpan, RowSpan: rowSpan}
	if colStart >= 0 && rowStart >= 0 {
		gi.Col, gi.Row, gi.Explicit = colStart, rowStart, true
	} else if colStart >= 0 {
		// A definite column with an auto row: the next free row in that
		// column — placed at the column on the first row here, which is
		// what the common one-row-per-section layouts (named content
		// columns) need.
		gi.Col, gi.Explicit = colStart, false
		gi.ColOnly = true
	}
	e.SetGridItem(gi)
}

// parseGridPlacement parses a grid-column/grid-row value: "3" (start line),
// "2 / 4" (start/end lines), "2 / span 2", or "span 3". Returns a 0-based
// start (-1 = auto) and a span (>=1).
func parseGridPlacement(v string) (start, span int) {
	start, span = -1, 1
	parts := splitTopLevel(v, '/')
	first := strings.TrimSpace(parts[0])
	if strings.HasPrefix(strings.ToLower(first), "span") {
		if n := atoiOr(strings.TrimSpace(first[4:]), 1); n >= 1 {
			span = n
		}
		return -1, span
	}
	if n, ok := atoi(first); ok {
		start = n - 1
	}
	if len(parts) >= 2 {
		second := strings.TrimSpace(parts[1])
		if strings.HasPrefix(strings.ToLower(second), "span") {
			span = atoiOr(strings.TrimSpace(second[4:]), 1)
		} else if end, ok := atoi(second); ok && start >= 0 && end-1 > start {
			span = (end - 1) - start
		}
	}
	if span < 1 {
		span = 1
	}
	return start, span
}

// gridLineStartSpan resolves one axis of a slash-separated grid-area line list
// (row-start/col-start[/row-end/col-end]). idxEnd is the index of the matching
// end line (2 for row-end, 3 for col-end) when present.
func gridLineStartSpan(startTok string, parts []string, idxEnd int) (start, span int) {
	start, span = -1, 1
	if n, ok := atoi(strings.TrimSpace(startTok)); ok {
		start = n - 1
	}
	if len(parts) > idxEnd {
		if end, ok := atoi(strings.TrimSpace(parts[idxEnd])); ok && start >= 0 && end-1 > start {
			span = (end - 1) - start
		}
	}
	return start, span
}

// parseGridAreas parses grid-template-areas — quoted rows of space-separated
// area names ("." = empty) — into name → [rowStart, colStart, rowSpan, colSpan]
// (0-based). A name spanning multiple cells yields its bounding rectangle.
func parseGridAreas(v string) map[string][4]int {
	out := map[string][4]int{}
	if strings.TrimSpace(v) == "" {
		return out
	}
	// Each quoted string is one row.
	type cell struct{ minR, minC, maxR, maxC int }
	acc := map[string]*cell{}
	row := 0
	for _, q := range extractQuoted(v) {
		for col, name := range strings.Fields(q) {
			if name == "." || name == "" {
				continue
			}
			c, ok := acc[name]
			if !ok {
				acc[name] = &cell{row, col, row, col}
				continue
			}
			if row < c.minR {
				c.minR = row
			}
			if col < c.minC {
				c.minC = col
			}
			if row > c.maxR {
				c.maxR = row
			}
			if col > c.maxC {
				c.maxC = col
			}
		}
		row++
	}
	for name, c := range acc {
		out[name] = [4]int{c.minR, c.minC, c.maxR - c.minR + 1, c.maxC - c.minC + 1}
	}
	return out
}

// extractQuoted returns the contents of each quoted run in s, accepting both
// single and double quotes (CSS strings allow either).
func extractQuoted(s string) []string {
	var out []string
	for {
		i := strings.IndexAny(s, `"'`)
		if i < 0 {
			return out
		}
		q := s[i]
		j := strings.IndexByte(s[i+1:], q)
		if j < 0 {
			return out
		}
		out = append(out, s[i+1:i+1+j])
		s = s[i+2+j:]
	}
}

func atoi(s string) (int, bool) {
	n, err := strconv.Atoi(strings.TrimSpace(s))
	return n, err == nil
}

func atoiOr(s string, def int) int {
	if n, ok := atoi(s); ok {
		return n
	}
	return def
}

// parseGridTracks parses a grid-template-columns/rows value into tracks,
// expanding repeat(n, <track-list>). Line names ([a b]) are skipped.
func parseGridTracks(v string, fs float32) []qui.GridTrack {
	tracks, _ := parseGridTrackList(v, fs)
	return tracks
}

// parseGridTrackList is parseGridTracks that also returns a
// repeat(auto-fill | auto-fit, …) as a qui.GridRepeat positioned among the
// other tracks.
func parseGridTrackList(v string, fs float32) ([]qui.GridTrack, *qui.GridRepeat) {
	v = stripLineNames(strings.TrimSpace(v))
	if v == "" || strings.EqualFold(v, "none") {
		return nil, nil
	}
	var tracks []qui.GridTrack
	var auto *qui.GridRepeat
	for _, tok := range fieldsTopLevel(v) {
		low := strings.ToLower(tok)
		if strings.HasPrefix(low, "repeat(") && strings.HasSuffix(tok, ")") {
			unit, n, fit, ok := parseRepeat(tok[len("repeat("):len(tok)-1], fs)
			if !ok {
				continue
			}
			if n < 0 {
				if auto == nil {
					auto = &qui.GridRepeat{At: len(tracks), Tracks: unit, Fit: fit}
				}
				continue
			}
			for range n {
				tracks = append(tracks, unit...)
			}
			continue
		}
		if tr, ok := parseGridTrack(tok, fs); ok {
			tracks = append(tracks, tr)
		}
	}
	return tracks, auto
}

// stripLineNames drops [line-name] groups from a track list.
func stripLineNames(v string) string {
	if !strings.Contains(v, "[") {
		return v
	}
	var b strings.Builder
	depth := 0
	for _, r := range v {
		switch {
		case r == '[':
			depth++
			b.WriteByte(' ')
		case r == ']':
			if depth > 0 {
				depth--
			}
		case depth == 0:
			b.WriteRune(r)
		}
	}
	return strings.TrimSpace(b.String())
}

// parseRepeat parses the inside of repeat(count, track-list). n is -1 for
// auto-fill / auto-fit (fit reports auto-fit).
func parseRepeat(inner string, fs float32) (unit []qui.GridTrack, n int, fit, ok bool) {
	parts := splitTopLevel(inner, ',')
	if len(parts) < 2 {
		return nil, 0, false, false
	}
	switch c := strings.ToLower(strings.TrimSpace(parts[0])); c {
	case "auto-fill":
		n = -1
	case "auto-fit":
		n, fit = -1, true
	default:
		count, okc := parseFloat(c)
		if !okc || count < 1 {
			return nil, 0, false, false
		}
		n = min(int(count), 10000)
	}
	for _, tok := range fieldsTopLevel(stripLineNames(strings.Join(parts[1:], ","))) {
		if tr, okt := parseGridTrack(tok, fs); okt {
			unit = append(unit, tr)
		}
	}
	return unit, n, fit, len(unit) > 0
}

// parseGridTrack parses one track token: `<n>fr`, `auto`, min-/max-content,
// a length, minmax(min, max) or fit-content(length).
func parseGridTrack(tok string, fs float32) (qui.GridTrack, bool) {
	low := strings.ToLower(strings.TrimSpace(tok))
	switch {
	case low == "auto" || low == "min-content" || low == "max-content":
		return qui.GridAutoTrack(), true
	case strings.HasPrefix(low, "minmax(") && strings.HasSuffix(low, ")"):
		args := splitTopLevel(low[len("minmax("):len(low)-1], ',')
		if len(args) != 2 {
			return qui.GridTrack{}, false
		}
		lo, hi := strings.TrimSpace(args[0]), strings.TrimSpace(args[1])
		maxT, ok := parseGridTrack(hi, fs)
		if !ok {
			return qui.GridTrack{}, false
		}
		maxT.AutoMin = false
		maxT.MinMax = maxT.Kind == qui.GridFixed
		if px, ok := parseLength(lo, fs, 0); ok {
			maxT.Min = px
			if maxT.Kind == qui.GridAuto {
				maxT.MinMax = true
			}
		} else if maxT.Kind == qui.GridFraction {
			// minmax(auto | min-content | max-content, Nfr)
			maxT.AutoMin = true
		}
		return maxT, true
	case strings.HasPrefix(low, "fit-content(") && strings.HasSuffix(low, ")"):
		t := qui.GridAutoTrack()
		if px, ok := parseLength(strings.TrimSpace(low[len("fit-content("):len(low)-1]), fs, 0); ok {
			t.Max = px
		}
		return t, true
	case strings.HasSuffix(low, "fr"):
		if w, ok := parseFloat(strings.TrimSuffix(low, "fr")); ok {
			t := qui.GridFractionTrack(w)
			t.AutoMin = webCompat
			return t, true
		}
	default:
		if pct, ok := parsePercentFraction(low); ok {
			t := qui.GridFixedTrack(0)
			t.Pct = pct
			return t, true
		}
		if px, ok := parseLength(low, fs, 0); ok {
			return qui.GridFixedTrack(px), true
		}
	}
	return qui.GridTrack{}, false
}

// gridGaps resolves row/column gaps from gap / grid-gap / row-gap /
// column-gap (+ grid-* aliases).
func gridGaps(cs *ComputedStyle) (row, col float32) {
	fs := cs.FontSize
	twoLen := func(v string) (float32, float32) {
		f := strings.Fields(v)
		if len(f) == 0 {
			return 0, 0
		}
		r, _ := parseLength(f[0], fs, 0)
		if len(f) >= 2 {
			c, _ := parseLength(f[1], fs, 0)
			return r, c
		}
		return r, r
	}
	if v, ok := cs.raw["gap"]; ok {
		row, col = twoLen(v)
	}
	if v, ok := cs.raw["grid-gap"]; ok {
		row, col = twoLen(v)
	}
	for _, k := range []string{"row-gap", "grid-row-gap"} {
		if v, ok := cs.raw[k]; ok {
			if px, ok := parseLength(v, fs, 0); ok {
				row = px
			}
		}
	}
	for _, k := range []string{"column-gap", "grid-column-gap"} {
		if v, ok := cs.raw[k]; ok {
			if px, ok := parseLength(v, fs, 0); ok {
				col = px
			}
		}
	}
	return row, col
}

// lastLineKey holds the explicit grid's last line number in a line-name
// map (not a valid CSS ident, so it never collides with a name).
const lastLineKey = " last"

func orAuto(v string) string {
	if v == "" {
		return "auto"
	}
	return v
}

// gridLineNames maps line names of a track list to their 1-based line
// numbers, in order: `[a b] 1fr [c] repeat(2, [d] 60px)` names line 1 a and
// b, line 2 c, lines 2 and 3 d. A grid-template-areas area `x` adds the
// implicit names x-start / x-end. auto-fill / auto-fit repeats contribute
// no names (their count is known only at layout).
func gridLineNames(tracks, areas string, rows bool) map[string][]int {
	names := map[string][]int{}
	line := 1
	addNames := func(group string) {
		for _, n := range strings.Fields(group) {
			names[n] = append(names[n], line)
		}
	}
	var walk func(v string, times int)
	walk = func(v string, times int) {
		for t := 0; t < times; t++ {
			i := 0
			for i < len(v) {
				switch c := v[i]; {
				case c == '[':
					j := strings.IndexByte(v[i:], ']')
					if j < 0 {
						return
					}
					addNames(v[i+1 : i+j])
					i += j + 1
				case c == ' ' || c == '\t' || c == '\n':
					i++
				default:
					// One token: a track, possibly a function with parens.
					j, depth := i, 0
					for j < len(v) {
						if v[j] == '(' {
							depth++
						} else if v[j] == ')' {
							depth--
						} else if depth == 0 && (v[j] == ' ' || v[j] == '[') {
							break
						}
						j++
					}
					tok := v[i:j]
					low := strings.ToLower(tok)
					if strings.HasPrefix(low, "repeat(") && strings.HasSuffix(tok, ")") {
						parts := splitTopLevel(tok[len("repeat("):len(tok)-1], ',')
						if len(parts) >= 2 {
							if n, ok := atoi(strings.TrimSpace(parts[0])); ok && n >= 1 {
								walk(strings.Join(parts[1:], ","), min(n, 1000))
							}
						}
					} else if tok != "" {
						line++
					}
					i = j
				}
			}
		}
	}
	walk(strings.TrimSpace(tracks), 1)
	// The explicit grid's last line, for negative line numbers ("-1").
	lastLine := line
	areaMap := parseGridAreas(areas)
	for _, a := range areaMap {
		end := a[1] + a[3] + 1
		if rows {
			end = a[0] + a[2] + 1
		}
		lastLine = max(lastLine, end)
	}
	names[lastLineKey] = []int{lastLine}
	for name, a := range areaMap {
		start, span := a[1], a[3]
		if rows {
			start, span = a[0], a[2]
		}
		names[name+"-start"] = append(names[name+"-start"], start+1)
		names[name+"-end"] = append(names[name+"-end"], start+span+1)
	}
	return names
}

// resolveGridLine resolves one grid line token to a 1-based line number:
// an integer, `<name>`, `<name> <n>` / `<n> <name>` (the nth line of that
// name). A bare name as a start (end) line also finds the implicit
// `<name>-start` (`-end`) lines first, as CSS Grid § 8.3.1 does.
func resolveGridLine(tok string, names map[string][]int, end bool) (int, bool) {
	tok = strings.TrimSpace(tok)
	if tok == "" || strings.EqualFold(tok, "auto") {
		return 0, false
	}
	if n, ok := atoi(tok); ok {
		if n < 0 {
			// Counted from the end of the explicit grid: -1 is its last line.
			last := 1
			if ls := names[lastLineKey]; len(ls) > 0 {
				last = ls[0]
			}
			n = max(last+1+n, 1)
		}
		if n == 0 {
			return 0, false
		}
		return n, true
	}
	f := strings.Fields(tok)
	nth, name := 1, ""
	for _, x := range f {
		if n, ok := atoi(x); ok {
			nth = n
		} else {
			name = x
		}
	}
	if name == "" {
		return 0, false
	}
	suffix := "-start"
	if end {
		suffix = "-end"
	}
	for _, key := range []string{name + suffix, name} {
		if ls := names[key]; len(ls) > 0 {
			i := nth - 1
			if nth < 0 {
				i = len(ls) + nth
			}
			if i >= 0 && i < len(ls) {
				return ls[i], true
			}
		}
	}
	return 0, false
}

// parseGridPlacementNamed is parseGridPlacement with line names: "a / b",
// "content" (content-start … content-end), "a / span 2", "span 2 / b".
func parseGridPlacementNamed(v string, names map[string][]int) (start, span int) {
	start, span = -1, 1
	parts := splitTopLevel(v, '/')
	first := strings.TrimSpace(parts[0])
	second := ""
	if len(parts) >= 2 {
		second = strings.TrimSpace(parts[1])
	} else if _, isNum := atoi(first); !isNum && !strings.HasPrefix(strings.ToLower(first), "span") {
		// A lone name spans its -start to -end lines (end defaults to the
		// same ident).
		second = first
	}
	spanOf := func(tok string) (int, bool) {
		if strings.HasPrefix(strings.ToLower(tok), "span") {
			return max(atoiOr(strings.TrimSpace(tok[4:]), 1), 1), true
		}
		return 0, false
	}
	if n, ok := spanOf(first); ok {
		span = n
		if e, ok := resolveGridLine(second, names, true); ok {
			return max(e-1-n, 0), n
		}
		return -1, n
	}
	s, okS := resolveGridLine(first, names, false)
	if !okS {
		if n, ok := spanOf(second); ok {
			return -1, n
		}
		return -1, 1
	}
	start = s - 1
	if n, ok := spanOf(second); ok {
		return start, n
	}
	if e, ok := resolveGridLine(second, names, true); ok {
		if e-1 > start {
			span = e - 1 - start
		} else if e-1 < start {
			start, span = e-1, start-(e-1)
		}
	}
	return start, max(span, 1)
}
