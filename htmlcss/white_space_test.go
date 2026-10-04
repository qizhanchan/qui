package htmlcss

import (
	"strings"
	"testing"
)

// leafLabelText returns the text of an element's internal label.
func leafLabelText(e *El) string {
	if e.textLabel != nil {
		return e.textLabel.Text()
	}
	return ""
}

// P1: white-space:pre preserves runs of spaces and explicit newlines; the
// default (normal) collapses them.
func TestWhiteSpacePrePreservesWhitespace(t *testing.T) {
	src := "<body>" +
		`<pre id="pre">line one` + "\n" + `    indented two` + "\n" + `end</pre>` +
		`<div id="norm">a    b` + "\n" + `c</div>` +
		"</body>"
	res := RenderDoc(src, `#pre { white-space: pre; }`, Options{})

	pre := res.ByID["pre"].(*El)
	got := leafLabelText(pre)
	if !strings.Contains(got, "\n") {
		t.Errorf("<pre> text lost newlines: %q", got)
	}
	if !strings.Contains(got, "    indented") {
		t.Errorf("<pre> text lost the run of spaces: %q", got)
	}
	// pre does not soft-wrap.
	if pre.textLabel.Paragraph.Wrap {
		t.Error("<pre> (white-space:pre) should not soft-wrap")
	}

	// The plain div collapses whitespace to single spaces, no newline.
	norm := res.ByID["norm"].(*El)
	if n := leafLabelText(norm); n != "a b c" {
		t.Errorf("normal div text = %q, want %q", n, "a b c")
	}
}

// pre-wrap keeps whitespace but still soft-wraps.
func TestWhiteSpacePreWrapWraps(t *testing.T) {
	res := RenderDoc(
		`<body><div id="pw">a    b</div></body>`,
		`#pw { white-space: pre-wrap; }`,
		Options{},
	)
	pw := res.ByID["pw"].(*El)
	if got := leafLabelText(pw); !strings.Contains(got, "    ") {
		t.Errorf("pre-wrap lost spaces: %q", got)
	}
	if !pw.textLabel.Paragraph.Wrap {
		t.Error("pre-wrap should still soft-wrap")
	}
}

// pre-line collapses space runs but keeps explicit newlines.
func TestWhiteSpacePreLine(t *testing.T) {
	res := RenderDoc(
		"<body><div id=\"pl\">a    b\nc</div></body>",
		`#pl { white-space: pre-line; }`,
		Options{},
	)
	pl := res.ByID["pl"].(*El)
	got := leafLabelText(pl)
	if got != "a b\nc" {
		t.Errorf("pre-line text = %q, want %q", got, "a b\nc")
	}
}

// F0.2 follow-through: pre content that mixes styled inline children takes
// the InlineBox path (not the plain Label), which historically re-collapsed
// the preserved spaces inside the IFC. The InlineBox now runs the engine's
// PreserveWhitespace mode, so the runs survive end to end.
func TestWhiteSpacePreMixedInlinePreserves(t *testing.T) {
	res := RenderDoc(
		`<body><pre id="p">aa  <b>bb  cc</b></pre></body>`,
		`#p { white-space: pre; }`,
		Options{},
	)
	pre := res.ByID["p"].(*El)
	if len(pre.inlinePool) == 0 {
		t.Fatal("mixed-inline <pre> should render through an InlineBox")
	}
	ib := pre.inlinePool[0]
	if !ib.PreserveWhitespace {
		t.Error("InlineBox under white-space:pre should have PreserveWhitespace set")
	}
	if got := ib.Text(); !strings.Contains(got, "aa  ") || !strings.Contains(got, "bb  cc") {
		t.Errorf("mixed-inline pre lost space runs: %q", got)
	}
}
