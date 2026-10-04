package htmlcss

import (
	"testing"

	"github.com/qizhanchan/qui"
)

// An El riding in an inline run as an atomic box reports its first-line text
// baseline through InlineBaseline. That prediction must be built from the
// SAME line box the engine actually lays the inner text out with — CSS
// semantics (font-size × line-height) over un-Ceil'd metrics. A stale copy
// of the old formula (Ceil'd natural height × scale) sat ~5 px low at the
// default line-height, dropping styled inline boxes below the surrounding
// text baseline.
func TestInlineBaselineMatchesEngineLineBox(t *testing.T) {
	res := RenderDoc(
		`<body><p>before <span id="s">chip</span> after</p></body>`,
		`#s { padding:2px; border:1px solid #000; font-size:16px; line-height:1.4; }`,
		Options{},
	)
	el := res.ByID["s"].(*El)
	f := fontFrom(el.lastCS)

	// The engine's line box for this style is CSS: 16 × 1.4, not a multiple
	// of the face's natural height.
	lineH := qui.BuildTextLayout("", f, qui.TextLayoutOptions{LineHeightScale: el.lastCS.LineHeight}).LineHeight
	if want := float32(16 * 1.4); lineH != want {
		t.Fatalf("engine line box = %v, want %v (font-size × line-height)", lineH, want)
	}

	// Baseline = padding + border + half-leading + ascent, with the ascent /
	// descent the engine measures (un-Ceil'd) against that line box.
	m := qui.GetFontFaceFor(f).Metrics()
	ascent := float32(m.Ascent) / 64
	descent := float32(m.Descent) / 64
	half := (lineH - (ascent + descent)) / 2
	if half < 0 {
		half = 0
	}
	want := el.Style().Padding.Top + 1 /* border-top */ + half + ascent
	if got := el.InlineBaseline(); got != want {
		t.Errorf("InlineBaseline = %v, want %v (engine line box %v, ascent %v)",
			got, want, lineH, ascent)
	}
}

// An <li> lays out as a [marker | content] row where the content host is an
// ANONYMOUS box: it must contribute no box of its own. The host is a plain
// widgets.Box, and widgets' DefaultStyle ships Padding{4,6,4,6} — leaving it
// in place pushed every list item's text 4px below its own marker and 6px
// past the row gap.
func TestListItemContentHostAddsNoBox(t *testing.T) {
	res := RenderDoc(
		`<body><ol><li id="a">Parse the HTML</li></ol></body>`,
		`li { font-size:16px; }`,
		Options{},
	)
	w := qui.NewTestWindow(qui.Size{W: 600, H: 300})
	w.SetRoot(res.Root)
	res.Root.Layout(qui.Rect{X: 0, Y: 0, W: 600, H: 300})

	el := res.ByID["a"].(*El)
	marker, host := el.markerLabel.Bounds(), el.liContent.Bounds()
	if marker.Y != host.Y {
		t.Errorf("marker top %v != content top %v — marker and text share a baseline", marker.Y, host.Y)
	}
	if marker.H != host.H {
		t.Errorf("marker height %v != content height %v", marker.H, host.H)
	}
	kids := el.liContent.ChildList()
	if len(kids) != 1 {
		t.Fatalf("content host children = %d, want 1", len(kids))
	}
	if got := kids[0].Bounds(); got != host {
		t.Errorf("content %v does not fill its anonymous host %v", got, host)
	}
}
