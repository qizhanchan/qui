package qui

import (
	"strconv"
	"strings"
)

// copyTextSelection aggregates the selected text of every selectable in
// document order and writes it to the clipboard (plain text plus an HTML
// flavor). It returns true (and handles the event) only when more than
// one widget contributes — a single-widget selection is left to that
// widget's own Cmd+C so existing behavior is unchanged.
// ClipboardListItem is implemented by an element/widget that represents a
// list item, so the window can rebuild a real <ol>/<ul> when a copied
// selection spans list items — instead of flattening every row to a <div>.
type ClipboardListItem interface {
	// ClipboardListItem reports the enclosing list tag ("ol" or "ul") and
	// the item's plain-text marker (e.g. "1." or "•"). An empty listTag
	// means the element is not a list item.
	ClipboardListItem() (listTag, marker string)
}

// ClipboardTableCell is implemented by a widget representing a table cell,
// so the window can rebuild a real <table> when a copied selection spans
// table cells — pasting as a genuine table into Google Docs / Word / Sheets
// instead of a flat run of text.
type ClipboardTableCell interface {
	// ClipboardTableCell reports the cell's table membership. table is an
	// opaque identity shared by every cell of the same table (nil = not a
	// table cell); row/col are 0-based grid coordinates; colSpan/rowSpan are
	// >= 1; header is true for a <th>.
	ClipboardTableCell() (table interface{}, row, col, colSpan, rowSpan int, header bool)
}

// ascendClipboardTableCell walks up from w to the nearest ancestor that
// reports itself as a table cell.
func ascendClipboardTableCell(w Widget) ClipboardTableCell {
	for cur := w; cur != nil; cur = cur.Parent() {
		if tc, ok := cur.(ClipboardTableCell); ok {
			if t, _, _, _, _, _ := tc.ClipboardTableCell(); t != nil {
				return tc
			}
		}
	}
	return nil
}

// ClipboardBlock is implemented by an element/widget that knows the
// semantic HTML block tag its text lives in (h1..h6, p, blockquote,
// pre, …) so the window's copy path can rebuild that tag instead of
// flattening every block to a generic <div> — pasting a copied heading
// into Word / Google Docs stays a heading.
type ClipboardBlock interface {
	// ClipboardBlockTag reports the block tag, or "" when the element is
	// not a semantic block (generic <div> handling applies).
	ClipboardBlockTag() string
}

// ascendClipboardBlock walks up from w to the nearest ancestor that
// reports a semantic block tag.
func ascendClipboardBlock(w Widget) ClipboardBlock {
	for cur := w; cur != nil; cur = cur.Parent() {
		if cb, ok := cur.(ClipboardBlock); ok {
			if tag := cb.ClipboardBlockTag(); tag != "" {
				return cb
			}
		}
	}
	return nil
}

// ascendClipboardListItem walks up from w to the nearest ancestor that
// reports itself as a list item (non-empty list tag).
func ascendClipboardListItem(w Widget) ClipboardListItem {
	for cur := w; cur != nil; cur = cur.Parent() {
		if li, ok := cur.(ClipboardListItem); ok {
			if tag, _ := li.ClipboardListItem(); tag != "" {
				return li
			}
		}
	}
	return nil
}

// clipPart is one selectable's contribution to a copy, tagged with its
// list-item / table-cell context so the aggregator can rebuild <ol>/<ul>
// and <table> structure.
type clipPart struct {
	plain, html      string
	li               ClipboardListItem // nil unless inside a list item
	listTag, marker  string
	tc               ClipboardTableCell // nil unless inside a table cell
	tbl              interface{}        // shared table identity
	row, col         int
	colSpan, rowSpan int
	header           bool
	blk              ClipboardBlock // nil unless inside a semantic block
	blkTag           string
}

// clipCell is a merged table cell (all parts sharing one cell), ready to
// emit as a <td>/<th>.
type clipCell struct {
	row, col         int
	colSpan, rowSpan int
	header           bool
	html, plain      string
}

func (w *Window) copyTextSelection() bool {
	flat := w.collectTextSelectables()
	var parts []clipPart
	for _, ts := range flat {
		s := ts.SelectedText()
		if s == "" {
			continue
		}
		p := clipPart{plain: s}
		if rts, ok := ts.(RichTextSelectable); ok {
			p.html = spansToHTML(rts.SelectedSpans())
		} else {
			p.html = htmlEscape(s)
		}
		if li := ascendClipboardListItem(ts); li != nil {
			p.li = li
			p.listTag, p.marker = li.ClipboardListItem()
		}
		if tc := ascendClipboardTableCell(ts); tc != nil {
			p.tc = tc
			p.tbl, p.row, p.col, p.colSpan, p.rowSpan, p.header = tc.ClipboardTableCell()
		}
		if blk := ascendClipboardBlock(ts); blk != nil {
			p.blk = blk
			p.blkTag = blk.ClipboardBlockTag()
		}
		parts = append(parts, p)
	}
	// Copy whenever any read-only selectable holds a selection — including
	// a selection confined to a single widget (one Label, or one folded
	// InlineBox paragraph). Editable widgets (Input / TextArea) aren't
	// TextSelectable, so they never appear here and keep their own
	// field-scoped Cmd/Ctrl+C.
	if len(parts) == 0 {
		return false
	}

	// Plain text: one line per block. List items keep their marker so a
	// plain-text paste still reads as a numbered / bulleted list. Parts
	// that share the same list-item element merge into one line.
	var plainLines []string
	for i := 0; i < len(parts); {
		// Table → tab-separated rows (a spreadsheet-friendly plain flavor),
		// with empty cells preserved so columns stay aligned.
		if parts[i].tc != nil {
			j := tableRunEnd(parts, i)
			if tableRunSingleCell(parts[i:j]) {
				plain, _ := mergeCellParts(parts[i:j])
				plainLines = append(plainLines, plain)
			} else {
				plainLines = append(plainLines, tablePlainRows(parts[i:j])...)
			}
			i = j
			continue
		}
		if li := parts[i].li; li != nil {
			var sb strings.Builder
			marker := parts[i].marker
			for i < len(parts) && parts[i].li == li {
				sb.WriteString(parts[i].plain)
				i++
			}
			line := sb.String()
			if marker != "" {
				line = marker + " " + line
			}
			plainLines = append(plainLines, line)
			continue
		}
		// Parts sharing one semantic block (several inline runs of the
		// same paragraph) merge into a single line.
		if blk := parts[i].blk; blk != nil {
			var sb strings.Builder
			for i < len(parts) && parts[i].blk == blk && parts[i].li == nil && parts[i].tc == nil {
				sb.WriteString(parts[i].plain)
				i++
			}
			plainLines = append(plainLines, sb.String())
			continue
		}
		plainLines = append(plainLines, parts[i].plain)
		i++
	}

	// HTML: rebuild <ol>/<ul> from runs of consecutive list items (markers
	// are regenerated by the list element, so they're not emitted as text);
	// every other block stays its own <div> so paragraph breaks survive.
	var b strings.Builder
	b.WriteString(`<meta charset="utf-8">`)
	for i := 0; i < len(parts); {
		// Table → rebuild <table><tr><td/th>, filling gaps so empty cells /
		// rows keep the columns aligned when pasted into Docs / Sheets.
		if parts[i].tc != nil {
			j := tableRunEnd(parts, i)
			if tableRunSingleCell(parts[i:j]) {
				_, html := mergeCellParts(parts[i:j])
				b.WriteString("<div>")
				if html == "" {
					b.WriteString("<br>")
				} else {
					b.WriteString(html)
				}
				b.WriteString("</div>")
			} else {
				writeTableHTML(&b, parts[i:j])
			}
			i = j
			continue
		}
		lt := parts[i].listTag
		if lt == "" {
			// A semantic block (h1..h6 / p / blockquote / pre / …) keeps its
			// tag so the paste stays a heading / paragraph; parts sharing
			// the same block element merge into one. Everything else stays
			// a generic <div>.
			tag := "div"
			var html string
			if blk := parts[i].blk; blk != nil {
				tag = parts[i].blkTag
				var htmls []string
				for i < len(parts) && parts[i].blk == blk &&
					parts[i].listTag == "" && parts[i].tc == nil {
					htmls = append(htmls, parts[i].html)
					i++
				}
				html = strings.Join(htmls, "")
			} else {
				html = parts[i].html
				i++
			}
			b.WriteString("<" + tag + ">")
			if html == "" {
				b.WriteString("<br>")
			} else {
				b.WriteString(html)
			}
			b.WriteString("</" + tag + ">")
			continue
		}
		b.WriteString("<" + lt + ">")
		for i < len(parts) && parts[i].listTag == lt {
			li := parts[i].li
			b.WriteString("<li>")
			for i < len(parts) && parts[i].li == li {
				b.WriteString(parts[i].html)
				i++
			}
			b.WriteString("</li>")
		}
		b.WriteString("</" + lt + ">")
	}

	SetClipboardRich(strings.Join(plainLines, "\n"), b.String())
	return true
}

// tableRunEnd returns the end index of the run of parts belonging to the
// same table as parts[i] (parts are in document / grid order).
func tableRunEnd(parts []clipPart, i int) int {
	tbl := parts[i].tbl
	j := i
	for j < len(parts) && parts[j].tbl == tbl {
		j++
	}
	return j
}

// groupTableCells merges each cell's parts and buckets the cells by row.
// Returns rows indexed 0..maxRow (sparse rows are empty slices) and the
// column count (max col+colSpan). Multiple parts in one cell (a cell holding
// several blocks / list items) join with a separator — nested block/list
// structure inside a cell flattens to line breaks.
func groupTableCells(tparts []clipPart) (rows [][]clipCell, maxCol int) {
	var cells []clipCell
	for k := 0; k < len(tparts); {
		cell := tparts[k].tc
		c := clipCell{
			row: tparts[k].row, col: tparts[k].col,
			colSpan: tparts[k].colSpan, rowSpan: tparts[k].rowSpan,
			header: tparts[k].header,
		}
		if c.colSpan < 1 {
			c.colSpan = 1
		}
		if c.rowSpan < 1 {
			c.rowSpan = 1
		}
		var htmls, plains []string
		for k < len(tparts) && tparts[k].tc == cell {
			htmls = append(htmls, tparts[k].html)
			plains = append(plains, tparts[k].plain)
			k++
		}
		c.html = strings.Join(htmls, "<br>")
		c.plain = strings.Join(plains, " ")
		cells = append(cells, c)
	}
	// Normalize to the selection's bounding box: subtract the minimum selected
	// row/col so a selection starting mid-table doesn't emit leading empty
	// rows/columns (e.g. copying a single "$99" cell becomes a 1×1 grid, not a
	// full table padded with blanks up to that cell's absolute position).
	if len(cells) > 0 {
		minRow, minCol := cells[0].row, cells[0].col
		for _, c := range cells {
			if c.row < minRow {
				minRow = c.row
			}
			if c.col < minCol {
				minCol = c.col
			}
		}
		for i := range cells {
			cells[i].row -= minRow
			cells[i].col -= minCol
		}
	}
	maxRow := 0
	for _, c := range cells {
		if c.row > maxRow {
			maxRow = c.row
		}
		if c.col+c.colSpan > maxCol {
			maxCol = c.col + c.colSpan
		}
	}
	rows = make([][]clipCell, maxRow+1)
	for _, c := range cells {
		rows[c.row] = append(rows[c.row], c)
	}
	return rows, maxCol
}

// tableRunSingleCell reports whether every part in a table run belongs to the
// same single cell — a selection confined to one <td>/<th>, which should copy
// as plain content rather than a one-cell <table> skeleton.
func tableRunSingleCell(parts []clipPart) bool {
	first := parts[0].tc
	for _, p := range parts {
		if p.tc != first {
			return false
		}
	}
	return true
}

// mergeCellParts joins a single cell's parts into plain / html strings, the
// same way groupTableCells merges multi-block cells (line breaks between blocks).
func mergeCellParts(parts []clipPart) (plain, html string) {
	plains := make([]string, 0, len(parts))
	htmls := make([]string, 0, len(parts))
	for _, p := range parts {
		plains = append(plains, p.plain)
		htmls = append(htmls, p.html)
	}
	return strings.Join(plains, " "), strings.Join(htmls, "<br>")
}

// writeTableHTML emits a <table> for one table's parts, inserting empty
// <td> for missing columns (and empty <tr> content for missing rows) so the
// pasted table keeps its column alignment.
func writeTableHTML(b *strings.Builder, tparts []clipPart) {
	rows, maxCol := groupTableCells(tparts)
	b.WriteString("<table>")
	for _, cells := range rows {
		b.WriteString("<tr>")
		expected := 0
		for _, c := range cells {
			for expected < c.col {
				b.WriteString("<td></td>")
				expected++
			}
			tag := "td"
			if c.header {
				tag = "th"
			}
			b.WriteString("<" + tag)
			if c.colSpan > 1 {
				b.WriteString(` colspan="` + strconv.Itoa(c.colSpan) + `"`)
			}
			if c.rowSpan > 1 {
				b.WriteString(` rowspan="` + strconv.Itoa(c.rowSpan) + `"`)
			}
			b.WriteString(">")
			b.WriteString(c.html)
			b.WriteString("</" + tag + ">")
			expected += c.colSpan
		}
		for expected < maxCol {
			b.WriteString("<td></td>")
			expected++
		}
		b.WriteString("</tr>")
	}
	b.WriteString("</table>")
}

// tablePlainRows renders one table's parts as tab-separated rows, empty
// cells preserved (placed by column index) so the TSV stays aligned.
func tablePlainRows(tparts []clipPart) []string {
	rows, maxCol := groupTableCells(tparts)
	if maxCol < 1 {
		maxCol = 1
	}
	var lines []string
	for _, cells := range rows {
		cols := make([]string, maxCol)
		for _, c := range cells {
			if c.col >= 0 && c.col < maxCol {
				cols[c.col] = c.plain
			}
		}
		lines = append(lines, strings.Join(cols, "\t"))
	}
	return lines
}

// spansToHTML renders styled spans into inline HTML.
func spansToHTML(spans []TextSpan) string {
	var b strings.Builder
	for _, sp := range spans {
		if sp.Image != "" {
			b.WriteString(`<img src="`)
			b.WriteString(htmlEscapeAttr(sp.Image))
			b.WriteString(`">`)
			continue
		}
		style := spanCSS(sp.Font, sp.Color)
		inner := htmlEscape(sp.Text)
		if style != "" {
			inner = `<span style="` + style + `">` + inner + `</span>`
		}
		if sp.Href != "" {
			b.WriteString(`<a href="`)
			b.WriteString(htmlEscapeAttr(sp.Href))
			b.WriteString(`">`)
			b.WriteString(inner)
			b.WriteString(`</a>`)
			continue
		}
		b.WriteString(inner)
	}
	return b.String()
}

// spanCSS derives an inline CSS declaration list from a span's font and
// color. Empty when neither carries information.
func spanCSS(font *Font, color *Color) string {
	var decls []string
	if font != nil {
		if font.EffectiveWeight() >= FontWeightSemiBold {
			decls = append(decls, "font-weight:700")
		}
		if font.Italic {
			decls = append(decls, "font-style:italic")
		}
		if font.Family != "" {
			decls = append(decls, "font-family:'"+font.Family+"'")
		}
		if font.Size > 0 {
			decls = append(decls, "font-size:"+trimFloat(font.Size)+"px")
		}
	}
	if color != nil {
		decls = append(decls, "color:"+cssColor(*color))
	}
	return strings.Join(decls, ";")
}

func cssColor(c Color) string {
	to := func(v float32) int {
		n := int(v*255 + 0.5)
		if n < 0 {
			n = 0
		}
		if n > 255 {
			n = 255
		}
		return n
	}
	const hex = "0123456789abcdef"
	r, g, bl := to(c.R), to(c.G), to(c.B)
	return string([]byte{'#',
		hex[r>>4], hex[r&0xf],
		hex[g>>4], hex[g&0xf],
		hex[bl>>4], hex[bl&0xf],
	})
}

func trimFloat(f float32) string {
	s := strconv.FormatFloat(float64(f), 'f', 2, 32)
	s = strings.TrimRight(s, "0")
	s = strings.TrimRight(s, ".")
	return s
}

// htmlEscapeAttr escapes a string for use inside a double-quoted HTML
// attribute (no newline→<br> translation).
func htmlEscapeAttr(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch r {
		case '&':
			b.WriteString("&amp;")
		case '<':
			b.WriteString("&lt;")
		case '>':
			b.WriteString("&gt;")
		case '"':
			b.WriteString("&quot;")
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

// htmlEscape escapes HTML-significant runes and turns newlines into <br>
// so a multi-line plain selection keeps its breaks.
func htmlEscape(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch r {
		case '&':
			b.WriteString("&amp;")
		case '<':
			b.WriteString("&lt;")
		case '>':
			b.WriteString("&gt;")
		case '"':
			b.WriteString("&quot;")
		case '\n':
			b.WriteString("<br>")
		case '\r':
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}
