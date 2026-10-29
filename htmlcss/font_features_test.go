package htmlcss

import (
	"reflect"
	"testing"

	"github.com/qizhanchan/qui/widgets"
)

// font-variant-ligatures / font-feature-settings reach the text's and the
// controls' qui.Font, inherit, and stack (the low-level setting last, so
// it wins in the shaper).
func TestFontFeaturesWiring(t *testing.T) {
	res := RenderDoc(
		`<body class="root"><p id="p">a != b</p><p id="q">a != b</p><input id="in" value="a != b"><p id="r">x</p></body>`,
		`.root { font-variant-ligatures: none }
		 #q { font-feature-settings: "calt" on, "ss01" 2 }
		 #r { font-variant-ligatures: normal }`,
		Options{},
	)
	none := []string{"-liga", "-clig", "-dlig", "-hlig", "-calt"}
	feat := func(id string) []string {
		lbl := res.ByID[id].(*El).textLabel
		if lbl == nil {
			t.Fatalf("#%s did not fold to a text Label", id)
		}
		return lbl.Style().Font.Features
	}
	if got := feat("p"); !reflect.DeepEqual(got, none) {
		t.Errorf("#p features = %v, want inherited %v", got, none)
	}
	if got, want := feat("q"), append(append([]string(nil), none...), "calt=1", "ss01=2"); !reflect.DeepEqual(got, want) {
		t.Errorf("#q features = %v, want %v", got, want)
	}
	if got := feat("r"); len(got) != 0 {
		t.Errorf("#r features = %v, want none after normal", got)
	}
	in := res.ByID["in"].(*El).backing.(*widgets.Input)
	if got := in.Style().Font.Features; !reflect.DeepEqual(got, none) {
		t.Errorf("input features = %v, want %v", got, none)
	}
}

func TestParseFontFeatureValues(t *testing.T) {
	if f, ok := parseVariantLigatures("no-contextual common-ligatures"); !ok || !reflect.DeepEqual(f, []string{"-calt", "liga", "clig"}) {
		t.Errorf("keywords: %v %v", f, ok)
	}
	for _, bad := range []string{"bogus", "contextual no-contextual", ""} {
		if _, ok := parseVariantLigatures(bad); ok {
			t.Errorf("%q accepted", bad)
		}
	}
	if f, ok := parseFeatureSettings(`'liga' off, "tnum"`); !ok || !reflect.DeepEqual(f, []string{"liga=0", "tnum=1"}) {
		t.Errorf("settings: %v %v", f, ok)
	}
	for _, bad := range []string{`liga`, `"lig" 1`, `"liga" -1`, `"liga" x`} {
		if _, ok := parseFeatureSettings(bad); ok {
			t.Errorf("%q accepted", bad)
		}
	}
}
