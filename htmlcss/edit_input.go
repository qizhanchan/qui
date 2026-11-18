package htmlcss

import (
	"strings"
	"time"

	"github.com/qizhanchan/qui"
)

// Input for editing hosts: keys, characters, the mouse, IME composition
// (El implements qui.IMEClient; only an editing host acts on it) and
// document.execCommand.

// hostFor returns e when it is a focused editing host, else nil.
func (e *El) focusedHost() *editState {
	if e.engine == nil || !e.isEditingHost() || !e.Focused() {
		return nil
	}
	return e.engine.editing()
}

// editFocusChanged runs when an editing host gains or loses focus.
func (e *El) editFocusChanged(focused bool) {
	if e.engine == nil || !e.isEditingHost() {
		return
	}
	st := e.engine.editing()
	if !focused {
		if st.comp != nil && st.comp.host == e {
			st.setPreedit(e, "", 0)
		}
		st.dragging = false
		e.Invalidate()
		return
	}
	if h := st.selHost(); h != e {
		st.materialize(e)
		fd := st.flatten(e)
		st.collapse(fd.pos[0])
	}
	e.Invalidate()
}

// editKeyDown handles an editing key on the focused host.
func (e *El) editKeyDown(ke qui.KeyEvent) bool {
	st := e.focusedHost()
	if st == nil || ke.Target() != qui.Widget(e) {
		return false
	}
	if st.comp != nil {
		return true // the input method owns the keys while composing
	}
	cmd := qui.IsCommandMod(ke.Mods)
	shift := ke.Mods&qui.ModShift != 0
	alt := ke.Mods&qui.ModAlt != 0
	st.materialize(e)
	switch ke.Key {
	case qui.KeyLeft, qui.KeyRight:
		dir := 1
		if ke.Key == qui.KeyLeft {
			dir = -1
		}
		unit := 1
		switch {
		case cmd:
			unit = 3
		case alt:
			unit = 2
		}
		st.moveH(e, dir, unit, shift)
	case qui.KeyUp, qui.KeyDown:
		dir := 1
		if ke.Key == qui.KeyUp {
			dir = -1
		}
		if cmd {
			st.moveDoc(e, dir, shift)
		} else {
			st.moveV(e, dir, shift)
		}
	case qui.KeyHome, qui.KeyEnd:
		dir := 1
		if ke.Key == qui.KeyHome {
			dir = -1
		}
		if ke.Mods&qui.ModControl != 0 {
			st.moveDoc(e, dir, shift)
		} else {
			st.moveH(e, dir, 3, shift)
		}
	case qui.KeyBackspace:
		st.deleteKey(e, -1, alt, cmd)
	case qui.KeyDelete:
		st.deleteKey(e, 1, alt, cmd)
	case qui.KeyEnter:
		if shift || e.plaintextOnly() {
			st.userEdit(e, "insertLineBreak", "", "enter", true, func() { st.insertLineBreak(e) })
		} else {
			st.userEdit(e, "insertParagraph", "", "enter", true, func() { st.insertParagraph(e) })
		}
	case qui.KeyA:
		if !cmd {
			return false
		}
		st.selectAll(e)
	case qui.KeyZ:
		if !cmd {
			return false
		}
		if shift {
			st.redoStep(e)
		} else {
			st.undoStep(e)
		}
	case qui.KeyY:
		if ke.Mods&qui.ModControl == 0 {
			return false
		}
		st.redoStep(e)
	case qui.KeyB, qui.KeyI, qui.KeyU:
		if !cmd || e.plaintextOnly() {
			return false
		}
		tag, typ := "b", "formatBold"
		switch ke.Key {
		case qui.KeyI:
			tag, typ = "i", "formatItalic"
		case qui.KeyU:
			tag, typ = "u", "formatUnderline"
		}
		st.userEdit(e, typ, "", "format", true, func() { st.toggleFormat(e, tag) })
	case qui.KeyC, qui.KeyX:
		if !cmd {
			return false
		}
		if t := e.engine.SelectionText(); t != "" {
			qui.SetClipboardText(t)
			if ke.Key == qui.KeyX {
				st.userEdit(e, "deleteByCut", "", "cut", true, func() { st.deleteSelection(e) })
			}
		}
	case qui.KeyV:
		if !cmd {
			return false
		}
		if t := qui.GetClipboardText(); t != "" {
			st.userEdit(e, "insertFromPaste", t, "paste", true, func() { st.insertPlain(e, t) })
		}
	default:
		return false
	}
	return true
}

// editChar inserts a typed character.
func (e *El) editChar(ce qui.CharEvent) bool {
	st := e.focusedHost()
	if st == nil || ce.Raw || ce.Mods&(qui.ModSuper|qui.ModControl) != 0 {
		return false
	}
	r := ce.Rune
	if r < 0x20 || r == 0x7F {
		return false
	}
	if st.comp != nil {
		// The input method committed (macOS delivers the commit as plain
		// characters, sometimes without clearing the marked text first).
		st.commitBuf += string(r)
		if !st.commitQueued {
			st.commitQueued = true
			finish := func() {
				st.commitQueued = false
				buf := st.commitBuf
				st.commitBuf = ""
				if buf != "" {
					st.commitIME(e, buf)
				}
			}
			if w := e.Window(); w != nil {
				w.PostJob(finish)
			} else {
				finish()
			}
		}
		return true
	}
	s := string(r)
	st.userEdit(e, "insertText", s, "type", true, func() { st.insertText(e, s) })
	return true
}

// editMouse places the caret / selects with the mouse inside a host.
func (e *El) editMouse(me qui.MouseEvent) bool {
	if e.engine == nil || !e.isEditingHost() || !e.Enabled() {
		return false
	}
	st := e.engine.editing()
	pt := qui.LocalPointToWindow(e, qui.Point{X: me.X, Y: me.Y})
	switch me.Type() {
	case qui.EventMouseDown:
		if me.Button != qui.MouseButtonLeft {
			return false
		}
		if st.pressCanceled {
			st.pressCanceled = false
			return false
		}
		if w := e.Window(); w != nil && !e.Focused() {
			w.SetFocus(e)
		}
		if st.comp != nil {
			st.setPreedit(e, "", 0)
		}
		clicks := me.Clicks
		now := me.Timestamp()
		if clicks <= 0 {
			if now.Sub(st.lastDown) < 500*time.Millisecond && absf(pt.X-st.lastDownPt.X) < 5 && absf(pt.Y-st.lastDownPt.Y) < 5 {
				st.clickCount++
			} else {
				st.clickCount = 1
			}
			clicks = st.clickCount
		}
		st.lastDown, st.lastDownPt = now, pt
		st.materialize(e)
		st.flushLayout(e)
		p := st.hitTest(e, pt)
		st.haveGoal = false
		switch {
		case clicks == 2:
			fd := st.flatten(e)
			i := fd.index(p)
			s, t := wordAt(fd, i)
			st.setSel(fd.pos[s], fd.pos[t])
			st.dragUnit = 2
		case clicks >= 3:
			fd := st.flatten(e)
			s, t := fd.hardLine(fd.index(p))
			st.setSel(fd.pos[s], fd.pos[t])
			st.dragUnit = 3
		case me.Mods&qui.ModShift != 0 && st.selHost() == e:
			st.setSel(st.anchor, p)
			st.dragUnit = 1
		default:
			st.collapse(p)
			st.dragUnit = 1
		}
		a, b := st.ordered()
		st.dragOrigin = [2]Pos{a, b}
		st.dragging = true
		return true
	case qui.EventMouseMove:
		if !st.dragging {
			return false
		}
		if w := e.Window(); w != nil && !w.PointerCaptured() && !e.Pressed() {
			st.dragging = false
			return false
		}
		st.flushLayout(e)
		p := st.hitTest(e, pt)
		switch st.dragUnit {
		case 2, 3:
			fd := st.flatten(e)
			i := fd.index(p)
			var s, t int
			if st.dragUnit == 2 {
				s, t = wordAt(fd, i)
			} else {
				s, t = fd.hardLine(i)
			}
			o0, o1 := st.dragOrigin[0], st.dragOrigin[1]
			if comparePos(fd.pos[s], o0) < 0 {
				st.setSel(o1, fd.pos[s])
			} else {
				st.setSel(o0, maxPos(fd.pos[t], o1))
			}
		default:
			st.setSel(st.anchor, p)
		}
		return true
	case qui.EventMouseUp:
		st.dragging = false
	}
	return false
}

// wordAt returns the word (or run of non-word characters) around flat
// index i.
func wordAt(fd *flatDoc, i int) (int, int) {
	if len(fd.text) == 0 {
		return 0, 0
	}
	if i >= len(fd.text) {
		i = len(fd.text) - 1
	}
	if fd.text[i] == '\n' && i > 0 {
		i--
	}
	return qui.WordRange(string(fd.text), i)
}

// moveH moves the caret by a character (unit 1), word (2) or to the line
// edge (3); extend keeps the anchor.
func (st *editState) moveH(host *El, dir, unit int, extend bool) {
	st.prepare(host)
	st.haveGoal = false
	if !extend && !st.collapsed() && unit == 1 {
		a, b := st.ordered()
		if dir < 0 {
			st.collapse(a)
		} else {
			st.collapse(b)
		}
		return
	}
	var np Pos
	if unit == 3 {
		st.flushLayout(host)
		p, ok := st.lineEdge(st.focus, dir > 0)
		if !ok {
			fd := st.flatten(host)
			s, t := fd.hardLine(fd.index(st.focus))
			if dir < 0 {
				p = fd.pos[s]
			} else {
				p = fd.pos[t]
			}
		}
		np = p
	} else {
		fd := st.flatten(host)
		i := fd.index(st.focus)
		j := i
		switch {
		case unit == 2 && dir < 0:
			j = fd.prevWord(i)
		case unit == 2:
			j = fd.nextWord(i)
		case dir < 0:
			j = fd.prevGrapheme(i)
			if comparePos(fd.pos[i], st.focus) < 0 && i < len(fd.text) && fd.src[i].kind == flatText && fd.src[i].node == st.focus.Node && st.focus.Off > fd.src[i].start {
				j = i // inside a collapsed run: back to its start
			}
		default:
			j = fd.nextGrapheme(i)
		}
		np = fd.pos[j]
	}
	if extend {
		st.setSel(st.anchor, np)
	} else {
		st.collapse(np)
	}
}

// moveDoc moves to the start / end of the host.
func (st *editState) moveDoc(host *El, dir int, extend bool) {
	st.prepare(host)
	fd := st.flatten(host)
	np := fd.pos[0]
	if dir > 0 {
		np = fd.pos[len(fd.pos)-1]
	}
	if extend {
		st.setSel(st.anchor, np)
	} else {
		st.collapse(np)
	}
}

// moveV moves the caret a visual line up or down, keeping its column.
func (st *editState) moveV(host *El, dir int, extend bool) {
	st.prepare(host)
	if !extend && !st.collapsed() {
		a, b := st.ordered()
		if dir < 0 {
			st.collapse(a)
		} else {
			st.collapse(b)
		}
	}
	st.flushLayout(host)
	r, ok := st.caretRectWin(st.focus)
	if !ok {
		st.moveDoc(host, dir, extend)
		return
	}
	x := r.X
	if st.haveGoal {
		x = st.goalX
	}
	step := max(r.H*0.6, 4)
	y := r.Y + r.H + step
	if dir < 0 {
		y = r.Y - step
	}
	p := st.hitTest(host, qui.Point{X: x, Y: y})
	c := comparePos(p, st.focus)
	if r2, ok := st.caretRectWin(p); c == 0 || dir < 0 && c > 0 || dir > 0 && c < 0 || ok && absf(r2.Y-r.Y) < 1 {
		fd := st.flatten(host)
		p = fd.pos[0]
		if dir > 0 {
			p = fd.pos[len(fd.pos)-1]
		}
	}
	if extend {
		st.setSel(st.anchor, p)
	} else {
		st.collapse(p)
	}
	st.goalX, st.haveGoal = x, true
}

// deleteKey handles Backspace (dir < 0) and Delete.
func (st *editState) deleteKey(host *El, dir int, word, line bool) {
	st.prepare(host)
	typ := "deleteContentBackward"
	if dir > 0 {
		typ = "deleteContentForward"
	}
	if !st.collapsed() {
		st.userEdit(host, typ, "", "delete", true, func() { st.deleteSelection(host) })
		return
	}
	fd := st.flatten(host)
	i := fd.index(st.focus)
	j := i
	switch {
	case line:
		st.flushLayout(host)
		if p, ok := st.lineEdge(st.focus, dir > 0); ok {
			j = fd.index(p)
		}
		if dir < 0 {
			typ = "deleteSoftLineBackward"
		} else {
			typ = "deleteSoftLineForward"
		}
		if j == i {
			if dir < 0 {
				j = fd.prevGrapheme(i)
			} else {
				j = fd.nextGrapheme(i)
			}
		}
	case word && dir < 0:
		j, typ = fd.prevWord(i), "deleteWordBackward"
	case word:
		j, typ = fd.nextWord(i), "deleteWordForward"
	case dir < 0:
		j = fd.prevGrapheme(i)
	default:
		j = fd.nextGrapheme(i)
	}
	if j == i {
		return
	}
	from, to := fd.pos[j], st.focus
	if dir > 0 {
		from, to = st.focus, fd.pos[j]
	}
	st.userEdit(host, typ, "", "delete", true, func() { st.deleteRange(host, from, to) })
}

// --- qui.IMEClient ---

// IMEActive implements qui.IMEActivity: only an editing host takes input
// method composition. Any other focused element (a link, a button, a
// tabindex div) opts out, so the page gets its keystrokes as plain keys
// and characters even with a CJK input method on. Form controls focus
// their own backing widgets, which are IME clients of their own.
func (e *El) IMEActive() bool { return e.isEditingHost() }

// CancelEditPress makes the next mouse press inside an editing host leave
// the caret and selection alone — a page's mousedown listener called
// preventDefault (the embedder reports it before the press is dispatched).
func (eng *StyleEngine) CancelEditPress() { eng.editing().pressCanceled = true }

// SetPreedit shows the input method's composition at the caret of a
// focused editing host (the composing text lives in the DOM, underlined).
func (e *El) SetPreedit(text string, cursor int) {
	if st := e.focusedHost(); st != nil {
		st.setPreedit(e, text, cursor)
		return
	}
	if e.engine != nil && e.engine.edit != nil && text == "" {
		if c := e.engine.edit.comp; c != nil && c.host == e {
			e.engine.edit.setPreedit(e, "", 0)
		}
	}
}

// CommitIME inserts the input method's final text.
func (e *El) CommitIME(text string) {
	if st := e.focusedHost(); st != nil && text != "" {
		st.commitIME(e, text)
	}
}

// CaretRect is the caret of a focused editing host in the element's own
// coordinates (where the IME candidate window goes).
func (e *El) CaretRect() qui.Rect {
	st := e.focusedHost()
	if st == nil || !st.hasSel {
		return qui.Rect{}
	}
	p := st.focus
	if st.comp != nil {
		p = Pos{Node: st.comp.seg, Off: st.comp.start}
	}
	r, ok := st.caretRectWin(p)
	if !ok {
		return qui.Rect{}
	}
	tl := qui.WindowPointToLocal(e, qui.Point{X: r.X, Y: r.Y})
	return qui.Rect{X: tl.X, Y: tl.Y, W: 1, H: r.H}
}

// --- execCommand ---

var execCommands = map[string]bool{
	"bold": true, "italic": true, "underline": true, "strikethrough": true, "inserttext": true,
	"delete": true, "forwarddelete": true, "insertparagraph": true, "insertlinebreak": true,
	"inserthtml": true, "selectall": true, "undo": true, "redo": true, "removeformat": true,
}

// QueryCommandSupported reports whether ExecCommand knows cmd.
func (eng *StyleEngine) QueryCommandSupported(cmd string) bool {
	return execCommands[strings.ToLower(cmd)]
}

// QueryCommandEnabled reports whether cmd would act on the selection.
func (eng *StyleEngine) QueryCommandEnabled(cmd string) bool {
	if !eng.QueryCommandSupported(cmd) {
		return false
	}
	return eng.editing().selHost() != nil
}

// QueryCommandState reports a formatting command's state at the
// selection (bold / italic / underline / strikeThrough).
func (eng *StyleEngine) QueryCommandState(cmd string) bool {
	st := eng.editing()
	host := st.selHost()
	if host == nil {
		return false
	}
	if tag := formatTag(cmd); tag != "" {
		return st.formatState(host, tag)
	}
	return false
}

func formatTag(cmd string) string {
	switch strings.ToLower(cmd) {
	case "bold":
		return "b"
	case "italic":
		return "i"
	case "underline":
		return "u"
	case "strikethrough":
		return "s"
	}
	return ""
}

// ExecCommand is document.execCommand on the selection's editing host.
// It fires input (not beforeinput, as in browsers) and reports whether
// the command ran.
func (eng *StyleEngine) ExecCommand(cmd, value string) bool {
	st := eng.editing()
	host := st.selHost()
	if host == nil {
		return false
	}
	run := func(typ, data string, fn func()) bool {
		before := st.hooks.BeforeInput
		st.hooks.BeforeInput = nil
		defer func() { st.hooks.BeforeInput = before }()
		return st.userEdit(host, typ, data, "exec", false, fn)
	}
	switch c := strings.ToLower(cmd); c {
	case "bold", "italic", "underline", "strikethrough":
		if host.plaintextOnly() {
			return false
		}
		typ := map[string]string{"bold": "formatBold", "italic": "formatItalic", "underline": "formatUnderline", "strikethrough": "formatStrikeThrough"}[c]
		return run(typ, "", func() { st.toggleFormat(host, formatTag(c)) })
	case "removeformat":
		return run("formatRemove", "", func() {
			for _, t := range []string{"b", "i", "u", "s"} {
				if !st.collapsed() {
					a, b := st.ordered()
					for _, seg := range segsIn(host, a, b) {
						st.unformat(host, seg, t)
					}
				}
			}
		})
	case "inserttext":
		return run("insertText", value, func() { st.insertText(host, value) })
	case "delete", "forwarddelete":
		dir := -1
		typ := "deleteContentBackward"
		if c == "forwarddelete" {
			dir, typ = 1, "deleteContentForward"
		}
		return run(typ, "", func() {
			if !st.collapsed() {
				st.deleteSelection(host)
				return
			}
			fd := st.flatten(host)
			i := fd.index(st.focus)
			if dir < 0 {
				if j := fd.prevGrapheme(i); j < i {
					st.deleteRange(host, fd.pos[j], st.focus)
				}
			} else if j := fd.nextGrapheme(i); j > i {
				st.deleteRange(host, st.focus, fd.pos[j])
			}
		})
	case "insertparagraph":
		return run("insertParagraph", "", func() { st.insertParagraph(host) })
	case "insertlinebreak":
		return run("insertLineBreak", "", func() { st.insertLineBreak(host) })
	case "inserthtml":
		if host.plaintextOnly() {
			return run("insertText", value, func() { st.insertPlain(host, value) })
		}
		return run("insertHTML", value, func() { st.insertHTML(host, value) })
	case "selectall":
		st.selectAll(host)
		return true
	case "undo":
		return st.undoStep(host)
	case "redo":
		return st.redoStep(host)
	}
	return false
}
