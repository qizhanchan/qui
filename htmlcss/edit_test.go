package htmlcss

import (
	"strings"
	"testing"

	"github.com/qizhanchan/qui"
)

// editFixture renders src (with a contenteditable #ed) into a test window.
type editFixture struct {
	t    *testing.T
	win  *qui.Window
	root *El
	host *El
	eng  *StyleEngine
	log  []string
}

func newEditFixture(t *testing.T, body string) *editFixture {
	t.Helper()
	prev := webCompat
	SetWebCompat(true)
	t.Cleanup(func() { SetWebCompat(prev) })
	src := `<html><body>` + body + `</body></html>`
	css := `body { margin: 0; font-size: 16px; } #ed { width: 300px; padding: 4px; }`
	res := RenderDoc(src, css, Options{ViewportWidth: 400, DocumentRoot: true, ScriptDOM: true})
	root := res.Root.(*El)
	win := qui.NewTestWindow(qui.Size{W: 400, H: 300})
	win.SetRoot(root)
	f := &editFixture{t: t, win: win, root: root, eng: root.Engine()}
	f.host, _ = res.ByID["ed"].(*El)
	if f.host == nil {
		t.Fatal("no #ed")
	}
	f.eng.SetEditHooks(EditHooks{
		BeforeInput: func(h *El, typ, data string) bool {
			f.log = append(f.log, "before:"+typ+":"+data)
			return !strings.Contains(data, "X") // "X" is refused
		},
		Input:       func(h *El, typ, data string) { f.log = append(f.log, "input:"+typ+":"+data) },
		Composition: func(h *El, typ, data string) { f.log = append(f.log, typ+":"+data) },
	})
	f.layout()
	return f
}

func (f *editFixture) layout() {
	f.eng.FlushPendingStyles()
	f.win.LayoutForTest()
}

// clickAt clicks at a window point.
func (f *editFixture) clickAt(x, y float32) {
	f.win.DispatchTestEvent(qui.NewMouseEvent(qui.EventMouseDown, x, y, qui.MouseButtonLeft, 0))
	f.win.DispatchTestEvent(qui.NewMouseEvent(qui.EventMouseUp, x, y, qui.MouseButtonLeft, 0))
	f.layout()
}

// clickEnd clicks to the right of the host's last line.
func (f *editFixture) clickEnd() {
	b := qui.InteractionBoundsOf(f.host)
	f.clickAt(b.X+b.W-2, b.Y+b.H-6)
}

func (f *editFixture) typeText(s string) {
	for _, r := range s {
		f.win.DispatchTestEvent(qui.NewCharEvent(r, 0))
	}
	f.layout()
}

func (f *editFixture) key(k qui.Key, mods qui.Modifiers) {
	f.win.DispatchTestEvent(qui.NewKeyEvent(qui.EventKeyDown, k, mods))
	f.win.DispatchTestEvent(qui.NewKeyEvent(qui.EventKeyUp, k, mods))
	f.layout()
}

func (f *editFixture) html() string { return innerMarkup(f.host) }

func (f *editFixture) want(want string) {
	f.t.Helper()
	if got := f.html(); got != want {
		f.t.Fatalf("markup = %q, want %q", got, want)
	}
}

func (f *editFixture) caret() Pos {
	_, focus, ok := f.eng.Selection()
	if !ok {
		f.t.Fatal("no selection")
	}
	return focus
}

func innerMarkup(e *El) string {
	var b strings.Builder
	for _, k := range e.Kids() {
		c, ok := k.(*El)
		if !ok {
			continue
		}
		switch {
		case c.IsText():
			b.WriteString(c.TextContent())
		case c.tag == "br":
			b.WriteString("<br>")
		default:
			b.WriteString("<" + c.tag + ">" + c.TextContent() + innerMarkup(c) + "</" + c.tag + ">")
		}
	}
	return b.String()
}

func TestEditTypeAndBackspace(t *testing.T) {
	f := newEditFixture(t, `<div id=ed contenteditable>hello</div>`)
	f.clickEnd()
	if f.win.Focused() != qui.Widget(f.host) {
		t.Fatalf("focus = %T, want the editing host", f.win.Focused())
	}
	c := f.caret()
	if !c.Node.IsText() || c.Off != 5 {
		t.Fatalf("caret after click at end = %+v", c)
	}
	r0, _ := f.eng.editing().caretRectWin(c)
	f.typeText("!")
	f.want("hello!")
	r1, _ := f.eng.editing().caretRectWin(f.caret())
	if r1.X <= r0.X {
		t.Fatalf("caret did not advance: %v -> %v", r0, r1)
	}
	f.key(qui.KeyBackspace, 0)
	f.key(qui.KeyBackspace, 0)
	f.want("hell")
	// Typing a space at the end keeps it visible (no-break space), and
	// the next character turns it back into a plain space.
	f.typeText(" ")
	f.want("hell ")
	f.typeText("o")
	f.want("hell o")
}

func TestEditCaretMovementAndSelection(t *testing.T) {
	f := newEditFixture(t, `<div id=ed contenteditable>abc <b>def</b> ghi</div>`)
	f.clickEnd()
	f.key(qui.KeyLeft, 0)
	f.key(qui.KeyLeft, 0)
	f.typeText("1")
	f.want("abc <b>def</b> g1hi")
	f.key(qui.KeyHome, 0)
	f.typeText("2")
	f.want("2abc <b>def</b> g1hi")
	// ⌥→ jumps a word; shift+→ selects.
	f.key(qui.KeyRight, qui.ModAlt)
	f.key(qui.KeyRight, qui.ModShift)
	f.key(qui.KeyRight, qui.ModShift)
	if got := f.eng.SelectionText(); got != " d" {
		t.Fatalf("selection = %q", got)
	}
	f.typeText("Z")
	f.want("2abcZ<b>ef</b> g1hi")
	// Caret geometry inside the bold run sits between the plain runs.
	f.key(qui.KeyRight, 0)
	c := f.caret()
	if c.Node.elParent.tag != "b" {
		t.Fatalf("caret not in <b>: %+v", c)
	}
	st := f.eng.editing()
	rb, _ := st.caretRectWin(c)
	f.key(qui.KeyEnd, 0)
	re, _ := st.caretRectWin(f.caret())
	if !(re.X > rb.X) || re.Y != rb.Y {
		t.Fatalf("caret rects: in bold %v, end %v", rb, re)
	}
	// Select all + delete empties the host.
	f.key(qui.KeyA, qui.ModSuper)
	if got := f.eng.SelectionText(); got != "2abcZef g1hi" {
		t.Fatalf("select all = %q", got)
	}
	f.key(qui.KeyBackspace, 0)
	if got := strings.Trim(f.html(), ""); got != "" {
		t.Fatalf("after deleting everything: %q", got)
	}
	f.typeText("x")
	f.want("x")
}

func TestEditEnterSplitsBlocks(t *testing.T) {
	f := newEditFixture(t, `<div id=ed contenteditable>abc</div>`)
	f.clickEnd()
	f.key(qui.KeyLeft, 0)
	f.key(qui.KeyEnter, 0)
	f.want("ab<div>c</div>")
	f.typeText("x")
	f.want("ab<div>xc</div>")
	f.key(qui.KeyEnd, 0)
	f.key(qui.KeyEnter, 0)
	f.want("ab<div>xc</div><div><br></div>")
	f.typeText("y")
	f.want("ab<div>xc</div><div>y</div>")
	// Up moves to the previous line.
	f.key(qui.KeyUp, 0)
	f.typeText("u")
	f.want("ab<div>xuc</div><div>y</div>")
	// Backspace at a block start merges the blocks.
	f.key(qui.KeyDown, 0)
	f.key(qui.KeyHome, 0)
	f.key(qui.KeyBackspace, 0)
	f.want("ab<div>xucy</div>")
	f.key(qui.KeyHome, 0)
	f.key(qui.KeyBackspace, 0)
	f.want("abxucy")
	// Shift+Enter inserts a <br> (plus a placeholder at a line end).
	f.key(qui.KeyEnd, 0)
	f.key(qui.KeyEnter, qui.ModShift)
	f.want("abxucy<br><br>")
	f.typeText("z")
	f.want("abxucy<br>z")
}

func TestEditEnterInsideParagraphs(t *testing.T) {
	f := newEditFixture(t, `<div id=ed contenteditable><p>one</p><p>two <i>three</i></p></div>`)
	b := f.host.Kids()[1].(*El).Bounds()
	_ = b
	f.clickEnd()
	f.key(qui.KeyLeft, 0)
	f.key(qui.KeyLeft, 0)
	f.key(qui.KeyEnter, 0)
	f.want("<p>one</p><p>two <i>thr</i></p><p><i>ee</i></p>")
	f.typeText("Q")
	f.want("<p>one</p><p>two <i>thr</i></p><p><i>Qee</i></p>")
}

func TestEditUndoRedo(t *testing.T) {
	f := newEditFixture(t, `<div id=ed contenteditable>base</div>`)
	f.clickEnd()
	f.typeText("abc")
	f.key(qui.KeyEnter, 0)
	f.typeText("d")
	f.want("baseabc<div>d</div>")
	f.key(qui.KeyZ, qui.ModSuper)
	f.want("baseabc<div><br></div>")
	f.key(qui.KeyZ, qui.ModSuper)
	f.want("baseabc")
	f.key(qui.KeyZ, qui.ModSuper)
	f.want("base")
	f.key(qui.KeyZ, qui.ModSuper|qui.ModShift)
	f.want("baseabc")
	f.key(qui.KeyZ, qui.ModSuper|qui.ModShift)
	f.key(qui.KeyZ, qui.ModSuper|qui.ModShift)
	f.want("baseabc<div>d</div>")
	if c := f.caret(); !c.Node.IsText() || c.Node.TextContent() != "d" || c.Off != 1 {
		t.Fatalf("caret after redo = %+v", c)
	}
}

func TestEditInputEventsAndCancel(t *testing.T) {
	f := newEditFixture(t, `<div id=ed contenteditable>a</div>`)
	f.clickEnd()
	f.typeText("bX")
	f.key(qui.KeyEnter, 0)
	f.key(qui.KeyBackspace, 0)
	f.want("ab")
	got := strings.Join(f.log, "|")
	want := "before:insertText:b|input:insertText:b|before:insertText:X|before:insertParagraph:|input:insertParagraph:|before:deleteContentBackward:|input:deleteContentBackward:"
	if got != want {
		t.Fatalf("events:\n got %s\nwant %s", got, want)
	}
}

func TestEditExecCommandBold(t *testing.T) {
	f := newEditFixture(t, `<div id=ed contenteditable>hello world</div>`)
	f.clickEnd()
	f.key(qui.KeyLeft, qui.ModAlt|qui.ModShift)
	if !f.eng.ExecCommand("bold", "") {
		t.Fatal("bold did not run")
	}
	f.layout()
	f.want("hello <b>world</b>")
	if !f.eng.QueryCommandState("bold") {
		t.Fatal("queryCommandState(bold) false inside <b>")
	}
	// Undo puts the moved text back where it was.
	f.key(qui.KeyZ, qui.ModSuper)
	f.want("hello world")
	f.key(qui.KeyZ, qui.ModSuper|qui.ModShift)
	f.want("hello <b>world</b>")
	f.eng.ExecCommand("bold", "")
	f.layout()
	f.want("hello world")
	f.eng.ExecCommand("insertHTML", "<i>x</i>")
	f.layout()
	f.want("hello <i>x</i>")
	f.typeText("y")
	f.want("hello <i>x</i>y")
}

func TestEditIMEComposition(t *testing.T) {
	f := newEditFixture(t, `<div id=ed contenteditable>ab</div>`)
	f.clickEnd()
	f.key(qui.KeyLeft, 0)
	f.log = nil
	f.win.SetPreedit("n", 1)
	f.win.SetPreedit("ni", 2)
	f.layout()
	f.want("anib")
	r := f.win.CaretRect()
	if r.H <= 0 || r.X <= 0 {
		t.Fatalf("IME caret rect = %v", r)
	}
	f.win.CommitIME("你")
	f.layout()
	f.want("a你b")
	got := strings.Join(f.log, "|")
	want := "compositionstart:|before:insertCompositionText:n|compositionupdate:n|input:insertCompositionText:n|" +
		"before:insertCompositionText:ni|compositionupdate:ni|input:insertCompositionText:ni|" +
		"before:insertCompositionText:你|compositionupdate:你|input:insertCompositionText:你|compositionend:你"
	if got != want {
		t.Fatalf("events:\n got %s\nwant %s", got, want)
	}
	if c := f.caret(); c.Off != 2 {
		t.Fatalf("caret after commit = %+v", c)
	}
	// One undo step removes the whole composition.
	f.key(qui.KeyZ, qui.ModSuper)
	f.want("ab")
	// Cancelling a composition removes its text.
	f.win.SetPreedit("x", 1)
	f.win.SetPreedit("", 0)
	f.want("ab")
}

func TestEditReadWriteAndIsContentEditable(t *testing.T) {
	f := newEditFixture(t, `<div id=ed contenteditable><span id=s>x</span><span contenteditable=false id=n>y</span></div><p id=p>z</p>`)
	s := f.root.findByID("s")
	n := f.root.findByID("n")
	p := f.root.findByID("p")
	if !s.IsContentEditable() || n.IsContentEditable() || p.IsContentEditable() {
		t.Fatal("isContentEditable inheritance wrong")
	}
	rw := mustSelectors(t, ":read-write")
	if !rw.Matches(s) || rw.Matches(n) || rw.Matches(p) {
		t.Fatal(":read-write wrong")
	}
	f.eng.SetDesignMode(true)
	if !p.IsContentEditable() || !rw.Matches(p) {
		t.Fatal("designMode did not make <p> editable")
	}
}

func TestEditMouseSelection(t *testing.T) {
	f := newEditFixture(t, `<div id=ed contenteditable>one two three</div>`)
	b := qui.InteractionBoundsOf(f.host)
	y := b.Y + b.H/2
	// Double click selects a word.
	x := b.X + 45
	f.win.DispatchTestEvent(qui.NewMouseEvent(qui.EventMouseDown, x, y, qui.MouseButtonLeft, 0))
	f.win.DispatchTestEvent(qui.NewMouseEvent(qui.EventMouseUp, x, y, qui.MouseButtonLeft, 0))
	f.win.DispatchTestEvent(qui.NewMouseEvent(qui.EventMouseDown, x, y, qui.MouseButtonLeft, 0))
	f.win.DispatchTestEvent(qui.NewMouseEvent(qui.EventMouseUp, x, y, qui.MouseButtonLeft, 0))
	if got := f.eng.SelectionText(); got != "two" {
		t.Fatalf("double click selected %q", got)
	}
	f.layout()
	if len(f.eng.editing().highlighted) != 1 {
		t.Fatal("selection not highlighted")
	}
	// Drag selects.
	f.clickAt(b.X+5, y)
	f.win.DispatchTestEvent(qui.NewMouseEvent(qui.EventMouseDown, b.X+5, y, qui.MouseButtonLeft, 0))
	f.win.DispatchTestEvent(qui.NewMouseEvent(qui.EventMouseMove, b.X+b.W-2, y, qui.MouseButtonLeft, 0))
	f.win.DispatchTestEvent(qui.NewMouseEvent(qui.EventMouseUp, b.X+b.W-2, y, qui.MouseButtonLeft, 0))
	if got := f.eng.SelectionText(); got != "one two three" {
		t.Fatalf("drag selected %q", got)
	}
}

func TestEditIMEOnlyInEditingHost(t *testing.T) {
	f := newEditFixture(t, `<button id=lnk>btn</button><div id=tab tabindex=0>plain</div><div id=ed contenteditable>ab</div>`)
	for _, id := range []string{"lnk", "tab"} {
		el := f.root.findByID(id)
		f.win.SetFocus(el)
		if f.win.Focused() != qui.Widget(el) {
			t.Fatalf("#%s not focused", id)
		}
		if el.IMEActive() || !f.win.IMEBypassed() {
			t.Fatalf("#%s takes IME composition", id)
		}
		f.win.SetPreedit("ni", 2)
		if r := f.win.CaretRect(); r != (qui.Rect{}) {
			t.Fatalf("#%s has an IME caret %v", id, r)
		}
	}
	if f.host.TextContent()+f.html() != "ab" {
		t.Fatalf("IME text reached the page: %q", f.host.TextContent()+f.html())
	}
	f.clickEnd()
	if !f.host.IMEActive() || f.win.IMEBypassed() {
		t.Fatal("editing host does not take IME composition")
	}
	// A press the page prevented leaves the caret where it was.
	f.key(qui.KeyHome, 0)
	f.eng.CancelEditPress()
	f.clickEnd()
	if c := f.caret(); c.Off != 0 {
		t.Fatalf("prevented press moved the caret: %+v", c)
	}
}
