package htmlcss

import (
	"testing"

	"github.com/qizhanchan/qui"
	"github.com/qizhanchan/qui/widgets"
)

const longWord = "1231212312123121231212312123121231212312123121231212312"

// inlineRunOf returns the folded InlineBox run of an element, if it has one.
func inlineRunOf(e *El) *widgets.InlineBox {
	for _, ch := range e.ChildList() {
		if b, ok := ch.(*widgets.InlineBox); ok {
			return b
		}
	}
	return nil
}

// overflow-wrap reaches both text assemblies — the plain leaf Label and the
// folded inline run (InlineBox) — and is inherited, so declaring it on a
// container governs the text inside.
func TestOverflowWrapWiring(t *testing.T) {
	res := RenderDoc(
		`<body>
			<p id="normal">`+longWord+`</p>
			<p id="brk">`+longWord+`</p>
			<p id="mixed">x <b>y</b> `+longWord+`</p>
			<div id="outer"><p id="inner">`+longWord+`</p></div>
		</body>`,
		`p { width: 120px }
		 #brk { overflow-wrap: break-word }
		 #mixed { word-break: break-all }
		 #outer { overflow-wrap: anywhere }`,
		Options{},
	)

	normal := res.ByID["normal"].(*El)
	if normal.textLabel == nil {
		t.Fatal("#normal should render as a text leaf")
	}
	if normal.textLabel.Paragraph.BreakLongWords {
		t.Error("#normal: CSS default is overflow-wrap:normal — the Label must not break long words")
	}

	brk := res.ByID["brk"].(*El)
	if !brk.textLabel.Paragraph.BreakLongWords {
		t.Error("#brk: overflow-wrap:break-word did not reach the Label")
	}

	mixed := res.ByID["mixed"].(*El)
	ib := inlineRunOf(mixed)
	if ib == nil {
		t.Fatal("#mixed should fold into an InlineBox run")
	}
	if !ib.BreakLongWords {
		t.Error("#mixed: word-break:break-all did not reach the InlineBox run")
	}

	inner := res.ByID["inner"].(*El)
	if !inner.textLabel.Paragraph.BreakLongWords {
		t.Error("#inner: overflow-wrap must inherit from #outer")
	}
}

// End-to-end geometry: with the default the unbreakable run forms one
// overflowing line; with break-word it wraps inside the content box.
func TestOverflowWrapGeometry(t *testing.T) {
	res := RenderDoc(
		`<body>
			<p id="normal">`+longWord+`</p>
			<p id="brk">`+longWord+`</p>
		</body>`,
		`body{margin:0;padding:0} p{margin:0;width:120px;font-size:16px}
		 #brk{overflow-wrap:break-word}`,
		Options{},
	)
	res.Root.Layout(qui.Rect{X: 0, Y: 0, W: 400, H: 600})

	normal := res.ByID["normal"].(*El)
	brk := res.ByID["brk"].(*El)
	nb, bb := normal.Bounds(), brk.Bounds()
	if nb.H <= 0 || bb.H <= 0 {
		t.Fatalf("empty bounds: normal=%v brk=%v", nb, bb)
	}
	if bb.H <= nb.H {
		t.Errorf("break-word should wrap into more lines: normal H=%v, break-word H=%v", nb.H, bb.H)
	}
	// The wrapped version's laid-out lines must stay inside the 120px box;
	// the overflowing one must not (that is the CSS-normal behavior).
	layoutOf := func(e *El) qui.TextLayout {
		lb := e.textLabel
		w := lb.Bounds().W
		return qui.BuildTextLayout(lb.Text(), lb.Style().Font, lb.Paragraph.LayoutOptions(w))
	}
	lay := layoutOf(brk)
	for i, ln := range lay.Lines {
		if ln.Width > bb.W+0.5 {
			t.Errorf("break-word line %d width %v exceeds the %v box", i, ln.Width, bb.W)
		}
	}
	if nlay := layoutOf(normal); len(nlay.Lines) != 1 {
		t.Errorf("overflow-wrap:normal should keep one line, got %d", len(nlay.Lines))
	}
}
