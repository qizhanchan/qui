package widgets

import (
	"strings"
	"time"
	"unicode/utf8"

	. "github.com/qizhanchan/qui"
)

// textEditor is the editing model shared by Input and TextArea: caret and
// selection, the undo history, IME composition, multi-click selection, caret
// blink, change / commit notification, and every key whose meaning does not
// depend on layout. The widgets keep what genuinely differs — chrome,
// geometry, scrolling, and how a visual line maps to rune offsets — and
// answer those questions through textEditorHost.
//
// The document itself stays in the widget's exported Text field (it has
// always been public API). The editor reaches it through
// textEditorHost.editorText on every call and never stores the pointer, so
// a widget copied by value never edits through a stale one, and a direct
// `Text = …` assignment is picked up (and the caret clamped) on the next
// operation.
//
// Selection invariant: selStart == -1 means "no selection", and then selEnd
// is 0. That is the state TextState / OnSelectionChange publish.
type textEditor struct {
	cursor   int // caret, in runes
	selStart int // selection anchor in runes; -1 = no selection
	selEnd   int // selection end (the caret side) in runes

	// IME composition: the in-flight string and the caret inside it.
	preedit       string
	preeditCursor int

	history undoHistory

	// selecting is a mouse-drag selection in progress.
	selecting bool

	// lastChangeUser records whether the latest value change came from the
	// user rather than a programmatic SetText; a SetText that then replaces
	// it is the controlled-value overwrite the debug surface flags.
	lastChangeUser bool
	// commitBaseline is the value as of focus-in / the last commit, so
	// OnCommit fires only when the value actually moved.
	commitBaseline string
	// committing is set while OnCommit runs, so a SetText from it rebases.
	committing bool

	caretVisible bool
	blinkMS      int64

	clickCount  int
	lastClickMS int64
	lastClickX  float32
	lastClickY  float32
}

func newTextEditor() textEditor {
	return textEditor{selStart: -1, caretVisible: true}
}

// editorHooks are the widget's public callbacks, read fresh on each call.
type editorHooks struct {
	beforeInput func(BeforeInputEvent) bool
	onChange    func(string)
	onCommit    func(string)
	onCursor    func(int)
	onSelection func(start, end int)
}

// textEditorHost is the widget side of the editor.
type textEditorHost interface {
	Widget
	Window() *Window
	// editorText is the document (the widget's Text field).
	editorText() *string
	editorHooks() editorHooks
	// editorMultiline reports whether '\n' is content. It decides the paste
	// newline policy and whether a '\n' CharEvent is text.
	editorMultiline() bool
	// editorSecret reports a masked field (Input.Password): copy and cut
	// are no-ops, as browsers make them on type=password.
	editorSecret() bool
	// editorHorizontal is the caret offset one visual step left (dir < 0)
	// or right (dir > 0), following bidi and grapheme clusters.
	editorHorizontal(dir int) int
	// editorLineBounds is the visual line holding the caret (Home / End).
	editorLineBounds() (start, end int)
	// editorVertical is the Up / Down target; ok is false for a control
	// without lines, so the key is left unhandled.
	editorVertical(dir int) (target int, ok bool)
	// editorTextChanged runs after every text change (re-measure if the
	// control sizes to its content).
	editorTextChanged()
	// editorRevealCaret scrolls the caret into view.
	editorRevealCaret()
}

// caretState is the observable caret/selection, for change callbacks.
type caretState struct{ cursor, selStart, selEnd int }

func (e *textEditor) caretState() caretState {
	return caretState{e.cursor, e.selStart, e.selEnd}
}

// editorView is the editor state that changes what is painted. A widget's
// Handle compares it before and after an event to decide on a repaint.
type editorView struct {
	caret         caretState
	preedit       string
	preeditCursor int
	selecting     bool
	caretVisible  bool
}

func (e *textEditor) view() editorView {
	return editorView{e.caretState(), e.preedit, e.preeditCursor, e.selecting, e.caretVisible}
}

// emitStateChange fires OnCursorChange / OnSelectionChange for whatever moved
// since prev.
func (e *textEditor) emitStateChange(h textEditorHost, prev caretState) {
	hooks := h.editorHooks()
	if e.cursor != prev.cursor && hooks.onCursor != nil {
		hooks.onCursor(e.cursor)
	}
	if (e.selStart != prev.selStart || e.selEnd != prev.selEnd) && hooks.onSelection != nil {
		hooks.onSelection(e.selStart, e.selEnd)
	}
}

// ----------------------------------------------------------------------
// Caret and selection

// normalize clamps the caret, selection and composition caret to text and
// restores the selection invariant.
func (e *textEditor) normalize(text string) {
	n := utf8.RuneCountInString(text)
	e.cursor = clampInt(e.cursor, 0, n)
	if e.selStart < -1 {
		e.selStart = -1
	}
	if e.selStart > n {
		e.selStart = n
	}
	if e.selStart < 0 {
		e.selEnd = 0
	} else {
		e.selEnd = clampInt(e.selEnd, 0, n)
	}
	e.preeditCursor = clampInt(e.preeditCursor, 0, utf8.RuneCountInString(e.preedit))
}

func (e *textEditor) hasSelection() bool { return e.selStart >= 0 && e.selStart != e.selEnd }

// ordered returns the selection as [lo, hi).
func (e *textEditor) ordered() (int, int) {
	if e.selStart < e.selEnd {
		return e.selStart, e.selEnd
	}
	return e.selEnd, e.selStart
}

func (e *textEditor) clearSelection() {
	e.selStart = -1
	e.selEnd = 0
}

// moveTo moves the caret to pos, extending the selection from the current
// caret when extend is set and collapsing it otherwise.
func (e *textEditor) moveTo(pos int, extend bool) {
	if !extend {
		e.cursor = pos
		e.clearSelection()
		return
	}
	if e.selStart < 0 {
		e.selStart = e.cursor
	}
	e.cursor = pos
	e.selEnd = pos
	if e.selStart == e.selEnd {
		e.clearSelection()
	}
}

// setCursor parks the caret at pos (clamped) with no selection.
func (e *textEditor) setCursor(h textEditorHost, pos int) {
	e.normalize(*h.editorText())
	e.history.breakCoalesce()
	e.cursor = clampInt(pos, 0, utf8.RuneCountInString(*h.editorText()))
	e.clearSelection()
	h.editorRevealCaret()
	e.resetBlink()
}

// selectRange selects [start, end) (clamped) with the caret at end.
func (e *textEditor) selectRange(h textEditorHost, start, end int) {
	n := utf8.RuneCountInString(*h.editorText())
	e.normalize(*h.editorText())
	start, end = clampInt(start, 0, n), clampInt(end, 0, n)
	e.history.breakCoalesce()
	e.cursor = end
	if start == end {
		e.clearSelection()
	} else {
		e.selStart, e.selEnd = start, end
	}
	h.editorRevealCaret()
	e.resetBlink()
}

func (e *textEditor) resetBlink() {
	e.caretVisible = true
	e.blinkMS = time.Now().UnixMilli()
}

// tick flips the caret blink every 500 ms while focused and returns the
// region to repaint.
func (e *textEditor) tick(h textEditorHost, focused bool, now time.Time) Rect {
	if !focused {
		return Rect{}
	}
	nowMS := now.UnixMilli()
	var dirty Rect
	if nowMS-e.blinkMS > 500 {
		e.caretVisible = !e.caretVisible
		e.blinkMS = nowMS
		dirty = PaintBoundsInWindow(h)
	}
	// Wake an idle window for the next flip.
	if w := h.Window(); w != nil {
		w.RequestTickAt(time.UnixMilli(e.blinkMS + 501))
	}
	return dirty
}

func (e *textEditor) textState(text string) TextState {
	return TextState{
		Value:    text,
		Caret:    e.cursor,
		SelStart: e.selStart,
		SelEnd:   e.selEnd,
		Preedit:  e.preedit,
		CanUndo:  len(e.history.undo) > 0,
		CanRedo:  len(e.history.redo) > 0,
	}
}

// ----------------------------------------------------------------------
// Mouse

// registerClick counts consecutive clicks at one spot (double / triple).
func (e *textEditor) registerClick(m MouseEvent) int {
	now := m.When.UnixMilli()
	if now == 0 {
		now = time.Now().UnixMilli()
	}
	if now-e.lastClickMS <= 400 && absf(m.X-e.lastClickX) <= 4 && absf(m.Y-e.lastClickY) <= 4 {
		e.clickCount++
	} else {
		e.clickCount = 1
	}
	if e.clickCount > 3 {
		e.clickCount = 1
	}
	e.lastClickMS = now
	e.lastClickX, e.lastClickY = m.X, m.Y
	return e.clickCount
}

// pressAt starts a pointer selection at pos: one click places the caret,
// two select the word, three the logical line (the whole value of a
// single-line field).
func (e *textEditor) pressAt(h textEditorHost, pos int, m MouseEvent) {
	text := *h.editorText()
	e.normalize(text)
	switch e.registerClick(m) {
	case 2:
		a, b := wordBoundsIn([]rune(text), pos)
		e.selStart, e.selEnd, e.cursor = a, b, b
	case 3:
		a, b := logicalLineBoundsIn([]rune(text), pos)
		e.selStart, e.selEnd, e.cursor = a, b, b
	default:
		// An anchored, still-empty selection; release collapses it.
		e.cursor, e.selStart, e.selEnd = pos, pos, pos
	}
	e.selecting = true
	e.history.breakCoalesce()
	e.resetBlink()
}

// dragTo extends the pointer selection to pos.
func (e *textEditor) dragTo(pos int) {
	if e.selStart < 0 {
		e.selStart = e.cursor
	}
	e.cursor, e.selEnd = pos, pos
	e.resetBlink()
}

// release ends a pointer selection; an empty one collapses to a caret.
func (e *textEditor) release() {
	e.selecting = false
	if e.selStart == e.selEnd {
		e.clearSelection()
	}
}

// ----------------------------------------------------------------------
// Edits

func (e *textEditor) snapshot(text string) undoEntry {
	return undoEntry{text: text, cursorPos: e.cursor, selStart: e.selStart, selEnd: e.selEnd}
}

func (e *textEditor) beforeInput(h textEditorHost, kind, data string) bool {
	hook := h.editorHooks().beforeInput
	if hook == nil {
		return true
	}
	return hook(BeforeInputEvent{
		Kind:      kind,
		Text:      data,
		CursorPos: e.cursor,
		SelStart:  e.selStart,
		SelEnd:    e.selEnd,
		Value:     *h.editorText(),
	})
}

// userEdit runs one user edit: the BeforeInput veto, the undo snapshot, the
// mutation, then change notification and caret reveal. mutate reports
// whether it changed anything. Returns whether the text changed.
func (e *textEditor) userEdit(h textEditorHost, kind, data string, mutate func(text *string) bool) bool {
	text := h.editorText()
	e.normalize(*text)
	if !e.beforeInput(h, kind, data) {
		return false
	}
	before := e.snapshot(*text)
	if !mutate(text) {
		return false
	}
	e.history.push(kind, before, e.snapshot(*text))
	// A word boundary or a new line seals the coalescing group, so undo
	// unwinds one word (or line) at a time.
	if kind == "enter" || (kind == "type" && (data == " " || data == "\t")) {
		e.history.breakCoalesce()
	}
	h.editorTextChanged()
	e.fireChange(h)
	h.editorRevealCaret()
	e.resetBlink()
	return true
}

// replaceSelection splices s in place of the selection (or at the caret)
// and leaves the caret after it.
func (e *textEditor) replaceSelection(text *string, s string) {
	runes := []rune(*text)
	lo, hi := e.cursor, e.cursor
	if e.hasSelection() {
		lo, hi = e.ordered()
	}
	*text = string(runes[:lo]) + s + string(runes[hi:])
	e.cursor = lo + utf8.RuneCountInString(s)
	e.clearSelection()
}

// deleteSelectionOr removes the selection, or else the grapheme cluster
// before (dir < 0) or after (dir > 0) the caret.
func (e *textEditor) deleteSelectionOr(text *string, dir int) bool {
	if e.hasSelection() {
		e.replaceSelection(text, "")
		return true
	}
	runes := []rune(*text)
	switch {
	case dir < 0 && e.cursor > 0:
		// One grapheme cluster: backspacing 👌🏻 removes the whole emoji,
		// not just its skin-tone modifier.
		target := PrevClusterBoundary(runes, e.cursor)
		*text = string(runes[:target]) + string(runes[e.cursor:])
		e.cursor = target
		return true
	case dir > 0 && e.cursor < len(runes):
		target := NextClusterBoundary(runes, e.cursor)
		*text = string(runes[:e.cursor]) + string(runes[target:])
		return true
	}
	return false
}

// insert is the programmatic / IME insertion path (kind "insert" / "ime").
func (e *textEditor) insert(h textEditorHost, kind, s string) bool {
	if s == "" {
		return false
	}
	return e.userEdit(h, kind, s, func(text *string) bool {
		e.replaceSelection(text, s)
		return true
	})
}

// commitIME ends a composition and inserts its result.
func (e *textEditor) commitIME(h textEditorHost, s string) bool {
	e.preedit, e.preeditCursor = "", 0
	return e.insert(h, "ime", s)
}

// setPreedit stores the in-flight composition ("" clears it).
func (e *textEditor) setPreedit(h textEditorHost, s string, cursor int) {
	e.preedit = s
	e.preeditCursor = clampInt(cursor, 0, utf8.RuneCountInString(s))
	h.editorRevealCaret()
	e.resetBlink()
}

// typeRune handles a CharEvent. Control characters (and DEL) are never
// text; '\n' is text only in a multi-line control.
func (e *textEditor) typeRune(h textEditorHost, r rune) bool {
	if r == 0x7F || (r < 0x20 && !(r == '\n' && h.editorMultiline())) {
		return false
	}
	e.userEdit(h, "type", string(r), func(text *string) bool {
		// A CharEvent with a live preedit means the IME just committed;
		// some IMEs (macOS Pinyin) skip unmarkText, so clear it here or the
		// stale composition is drawn over the committed text.
		e.preedit, e.preeditCursor = "", 0
		e.replaceSelection(text, string(r))
		return true
	})
	return true
}

// newline inserts '\n' (multi-line Enter).
func (e *textEditor) newline(h textEditorHost) {
	e.userEdit(h, "enter", "\n", func(text *string) bool {
		e.replaceSelection(text, "\n")
		return true
	})
}

// normalizePaste applies the control's newline policy: a single-line field
// folds line breaks to spaces, a multi-line one normalizes them to '\n'.
func normalizePaste(s string, multiline bool) string {
	if multiline {
		s = strings.ReplaceAll(s, "\r\n", "\n")
		return strings.ReplaceAll(s, "\r", "\n")
	}
	s = strings.ReplaceAll(s, "\r\n", " ")
	return strings.NewReplacer("\n", " ", "\r", " ").Replace(s)
}

// setText replaces the document programmatically. Silent by design (see
// qui.TextChangeNotifier): a programmatic set must not re-enter the app's
// change handler. The caret moves to the end, as assigning an HTML
// control's value does, and the change is undoable. Returns false when the
// text was already s; the caller resets its scroll and reveals the caret.
func (e *textEditor) setText(h textEditorHost, s string) bool {
	text := h.editorText()
	if *text == s {
		return false
	}
	e.normalize(*text)
	before := e.snapshot(*text)
	*text = s
	e.cursor = utf8.RuneCountInString(s)
	e.clearSelection()
	e.history.push("set", before, e.snapshot(s))
	source := "program"
	if e.lastChangeUser {
		// The value the user typed is being overwritten: the
		// controlled-value fight signal.
		source = "program-overwrite"
	}
	e.lastChangeUser = false
	if e.committing {
		// OnCommit normalizing the value ("12" → "12.00"): that is the
		// committed value now, or the next blur would commit it back.
		e.commitBaseline = s
	}
	if w := h.Window(); w != nil {
		w.RecordInputChange(h, before.text, s, source)
	}
	h.editorTextChanged()
	return true
}

// fireChange publishes the current text as a user change: OnChange plus the
// debug surface's input record. Every user mutation funnels through here.
func (e *textEditor) fireChange(h textEditorHost) {
	e.lastChangeUser = true
	text := *h.editorText()
	if fn := h.editorHooks().onChange; fn != nil {
		fn(text)
	}
	if w := h.Window(); w != nil {
		w.RecordInputChange(h, "", text, "user")
	}
}

// fireCommit reports a finished value and rebases the baseline, so a commit
// followed by a blur fires once, and a blur without an edit not at all.
func (e *textEditor) fireCommit(h textEditorHost) {
	text := *h.editorText()
	fn := h.editorHooks().onCommit
	changed := text != e.commitBaseline
	e.commitBaseline = text
	if fn != nil && changed {
		e.committing = true
		defer func() { e.committing = false }()
		fn(text)
	}
}

// focusChanged runs on a focus transition (wasFocused → focused). Gaining
// focus sets the commit baseline; losing it commits an edited value. The
// selection is kept across a blur, as a browser keeps it for refocus.
func (e *textEditor) focusChanged(h textEditorHost, wasFocused, focused bool) {
	if focused && !wasFocused {
		e.commitBaseline = *h.editorText()
	} else if !focused && wasFocused {
		e.fireCommit(h)
	}
	e.selecting = false
	// Focus transitions seal the coalescing group: typing after a refocus
	// starts a new undo step.
	e.history.breakCoalesce()
	if focused {
		e.resetBlink()
	}
}

// ----------------------------------------------------------------------
// Undo / redo

func (e *textEditor) applyUndoEntry(h textEditorHost, u undoEntry) {
	*h.editorText() = u.text
	e.cursor, e.selStart, e.selEnd = u.cursorPos, u.selStart, u.selEnd
	// The restored text may differ in length; a stale composition pinned to
	// the old caret would mis-render.
	e.preedit, e.preeditCursor = "", 0
	e.normalize(u.text)
	h.editorTextChanged()
	h.editorRevealCaret()
	e.resetBlink()
}

// undo restores the previous snapshot; reports whether one was consumed.
func (e *textEditor) undo(h textEditorHost) bool {
	text := *h.editorText()
	prev := e.history.popUndo(e.snapshot(text))
	if prev == nil {
		return false
	}
	e.applyUndoEntry(h, *prev)
	if prev.text != text {
		e.fireChange(h)
	}
	return true
}

func (e *textEditor) redo(h textEditorHost) bool {
	text := *h.editorText()
	next := e.history.popRedo(e.snapshot(text))
	if next == nil {
		return false
	}
	e.applyUndoEntry(h, *next)
	if next.text != text {
		e.fireChange(h)
	}
	return true
}

// ----------------------------------------------------------------------
// Keyboard

// handleKey runs the editing keys both controls share. It returns false for
// a key it does not own (Enter, Tab, an Up/Down the control has no lines
// for, Escape with nothing selected) so the widget — or an ancestor — can
// take it. The caller handles the IME swallow and focus checks first.
func (e *textEditor) handleKey(h textEditorHost, k KeyEvent) bool {
	text := h.editorText()
	e.normalize(*text)
	shift := k.Mods&ModShift != 0
	if IsCommandMod(k.Mods) {
		switch k.Key {
		case KeyZ:
			// Cmd/Ctrl+Z undo, +Shift redo; Cmd/Ctrl+Y is the Windows redo.
			if shift {
				e.redo(h)
			} else {
				e.undo(h)
			}
			return true
		case KeyY:
			e.redo(h)
			return true
		case KeyA, KeyC, KeyX, KeyV:
			// Recognized even as a no-op (Cmd+C with nothing selected) so the
			// plain-key bindings below never see it.
			e.clipboardShortcut(h, k.Key)
			return true
		}
	}
	move := func(target int) bool {
		e.moveTo(target, shift)
		e.history.breakCoalesce()
		h.editorRevealCaret()
		e.resetBlink()
		return true
	}
	switch k.Key {
	case KeyBackspace, KeyDelete:
		dir := -1
		if k.Key == KeyDelete {
			dir = 1
		}
		e.userEdit(h, "delete", "", func(text *string) bool { return e.deleteSelectionOr(text, dir) })
		h.editorRevealCaret()
		e.resetBlink()
		return true
	case KeyLeft:
		return move(h.editorHorizontal(-1))
	case KeyRight:
		return move(h.editorHorizontal(1))
	case KeyHome:
		start, _ := h.editorLineBounds()
		return move(start)
	case KeyEnd:
		_, end := h.editorLineBounds()
		return move(end)
	case KeyUp, KeyDown:
		dir := -1
		if k.Key == KeyDown {
			dir = 1
		}
		target, ok := h.editorVertical(dir)
		if !ok {
			return false
		}
		return move(target)
	case KeyEscape:
		// Esc first drops a selection. With none it is not the field's key:
		// it bubbles to whatever dismisses (a dialog, a popup).
		if !e.hasSelection() {
			return false
		}
		e.clearSelection()
		e.history.breakCoalesce()
		e.resetBlink()
		return true
	}
	return false
}

func (e *textEditor) clipboardShortcut(h textEditorHost, k Key) {
	text := h.editorText()
	switch k {
	case KeyA:
		n := utf8.RuneCountInString(*text)
		if n == 0 {
			return
		}
		e.selStart, e.selEnd, e.cursor = 0, n, n
		// A selection move seals the typing group, like every other caret
		// move: typing over the selection is a new undo step.
		e.history.breakCoalesce()
		h.editorRevealCaret()
		e.resetBlink()
	case KeyC:
		if e.hasSelection() && !h.editorSecret() {
			lo, hi := e.ordered()
			SetClipboardText(string([]rune(*text)[lo:hi]))
		}
	case KeyX:
		if !e.hasSelection() || h.editorSecret() {
			return
		}
		lo, hi := e.ordered()
		cut := string([]rune(*text)[lo:hi])
		e.userEdit(h, "cut", "", func(text *string) bool {
			SetClipboardText(cut)
			e.replaceSelection(text, "")
			return true
		})
	case KeyV:
		s := GetClipboardText()
		if s == "" {
			return
		}
		s = normalizePaste(s, h.editorMultiline())
		e.userEdit(h, "paste", s, func(text *string) bool {
			e.replaceSelection(text, s)
			return true
		})
	}
}

// ----------------------------------------------------------------------
// Rune-level helpers

// wordBoundsIn returns the run of word (or non-word) runes around pos,
// never crossing a line break.
func wordBoundsIn(runes []rune, pos int) (int, int) {
	if len(runes) == 0 {
		return 0, 0
	}
	pos = clampInt(pos, 0, len(runes)-1)
	isWord := textWordRune(runes[pos])
	start := pos
	for start > 0 && runes[start-1] != '\n' && textWordRune(runes[start-1]) == isWord {
		start--
	}
	end := pos + 1
	for end < len(runes) && runes[end] != '\n' && textWordRune(runes[end]) == isWord {
		end++
	}
	return start, end
}

// logicalLineBoundsIn returns the '\n'-delimited line around pos.
func logicalLineBoundsIn(runes []rune, pos int) (int, int) {
	if len(runes) == 0 {
		return 0, 0
	}
	pos = clampInt(pos, 0, len(runes))
	start := pos
	if start == len(runes) && start > 0 {
		start--
	}
	for start > 0 && runes[start-1] != '\n' {
		start--
	}
	end := pos
	if end < start {
		end = start
	}
	for end < len(runes) && runes[end] != '\n' {
		end++
	}
	return start, end
}

func clampInt(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}
