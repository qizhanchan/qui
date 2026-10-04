package htmlcss

import (
	"strconv"
	"strings"

	"github.com/qizhanchan/qui"
)

// gridLayout builds a qui.GridLayout from the element's grid CSS. Columns
// come from grid-template-columns (px / fr / auto / repeat()); rows from
// grid-template-rows, or — when omitted — an implicit set of auto rows
// sized to hold all items row-major. Per-item placement (grid-column /
// grid-row spans) is not yet supported: items auto-place in order.
func gridLayout(cs *ComputedStyle, itemCount int) qui.Layout {
	// grid-template-areas can imply the track counts when explicit templates
	// are omitted (each area row is a grid row; area columns are grid columns).
	areaRows, areaCols := gridAreaDims(cs.raw["grid-template-areas"])

	cols := parseGridTracks(cs.raw["grid-template-columns"], cs.FontSize)
	if len(cols) == 0 {
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
	if len(rows) == 0 {
		nRows := areaRows
		if nRows < 1 {
			nRows = (itemCount + len(cols) - 1) / len(cols)
		}
		if nRows < 1 {
			nRows = 1
		}
		rows = make([]qui.GridTrack, nRows)
		for i := range rows {
			rows[i] = qui.GridAutoTrack()
		}
	}
	rowGap, colGap := gridGaps(cs)
	return qui.GridLayout{Columns: cols, Rows: rows, ColGap: colGap, RowGap: rowGap}
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
	if v := strings.TrimSpace(cs.raw["grid-column"]); v != "" {
		colStart, colSpan = parseGridPlacement(v)
	}
	if v := strings.TrimSpace(cs.raw["grid-row"]); v != "" {
		rowStart, rowSpan = parseGridPlacement(v)
	}

	gi := qui.GridItem{ColSpan: colSpan, RowSpan: rowSpan}
	if colStart >= 0 && rowStart >= 0 {
		gi.Col, gi.Row, gi.Explicit = colStart, rowStart, true
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
// expanding repeat(n, <track-list>).
func parseGridTracks(v string, fs float32) []qui.GridTrack {
	v = strings.TrimSpace(v)
	if v == "" || strings.EqualFold(v, "none") {
		return nil
	}
	var tracks []qui.GridTrack
	for _, tok := range fieldsTopLevel(v) {
		low := strings.ToLower(tok)
		if strings.HasPrefix(low, "repeat(") && strings.HasSuffix(tok, ")") {
			tracks = append(tracks, expandRepeat(tok[len("repeat("):len(tok)-1], fs)...)
			continue
		}
		if tr, ok := parseGridTrack(tok, fs); ok {
			tracks = append(tracks, tr)
		}
	}
	return tracks
}

// expandRepeat parses the inside of `repeat(count, track-list)`.
func expandRepeat(inner string, fs float32) []qui.GridTrack {
	parts := splitTopLevel(inner, ',')
	if len(parts) < 2 {
		return nil
	}
	count, ok := parseFloat(strings.TrimSpace(parts[0]))
	if !ok || count < 1 {
		return nil
	}
	var unit []qui.GridTrack
	for _, tok := range fieldsTopLevel(strings.Join(parts[1:], " ")) {
		if tr, ok := parseGridTrack(tok, fs); ok {
			unit = append(unit, tr)
		}
	}
	var out []qui.GridTrack
	for i := 0; i < int(count); i++ {
		out = append(out, unit...)
	}
	return out
}

// parseGridTrack parses one track token: `<n>fr`, `auto`, or a length.
func parseGridTrack(tok string, fs float32) (qui.GridTrack, bool) {
	low := strings.ToLower(strings.TrimSpace(tok))
	switch {
	case low == "auto" || low == "min-content" || low == "max-content":
		return qui.GridAutoTrack(), true
	case strings.HasSuffix(low, "fr"):
		if w, ok := parseFloat(strings.TrimSuffix(low, "fr")); ok {
			return qui.GridFractionTrack(w), true
		}
	default:
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
