package svg

import (
	"strings"
	"testing"
)

// Minified number lists: a sign or a second '.' starts a new number.
func TestParseNumberListCompact(t *testing.T) {
	got, err := parseNumberList("10-5 1.5.5,1e-3-2")
	if err != nil {
		t.Fatal(err)
	}
	want := []float32{10, -5, 1.5, .5, 1e-3, -2}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got %v, want %v", got, want)
		}
	}
	for _, bad := range []string{"NaN", "Inf", "0x1p3", "1,,x"} {
		if _, err := parseNumberList(bad); err == nil {
			t.Errorf("%q parsed", bad)
		}
	}
}

// style="…" on the root <svg> beats a later presentation attribute.
func TestRootStyleBeatsPresentationAttr(t *testing.T) {
	doc, err := Parse(strings.NewReader(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 10 10" style="fill:red" fill="blue"></svg>`))
	if err != nil {
		t.Fatal(err)
	}
	red, _ := parsePaint("red")
	if doc.Root.Style.Fill != red {
		t.Fatalf("root fill = %+v, want red", doc.Root.Style.Fill)
	}
}
