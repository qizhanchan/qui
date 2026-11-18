package htmlcss

import (
	"testing"

	"github.com/qizhanchan/qui"
)

// An inline atom of only spaces under white-space:pre (a git diff's
// changed alignment padding, highlighted with a rounded background) is as
// wide as its spaces, so the text after it lines up with an unhighlighted
// row: an unwrapped line used to drop its trailing space's advance.
func TestWhiteSpacePreSpaceAtomWidth(t *testing.T) {
	res := RenderDoc(`<body>`+
		`<div class="t"><span>Org</span><span class="w">    </span><span>string</span><span> </span><span id="a" class="w">x</span></div>`+
		`<div class="t"><span>Org    string </span><span id="b" class="w">x</span></div>`+
		`<div class="t"><span>Org</span><span class="w"> </span><span>   string</span><span class="w"> </span><span id="c" class="w">x</span></div>`+
		`</body>`,
		`body { font-size: 12px } .t { white-space: pre } .w { background: #f00; border-radius: 2px }`, Options{})
	res.Root.Layout(qui.Rect{W: 600, H: 300})
	want := res.ByID["b"].Bounds().X
	for _, id := range []string{"a", "c"} {
		if got := res.ByID[id].Bounds().X; got < want-0.5 || got > want+0.5 {
			t.Errorf("#%s x = %v, want %v (as in the plain row)", id, got, want)
		}
	}
}
