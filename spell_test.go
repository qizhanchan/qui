package qui

import (
	"reflect"
	"testing"
)

func TestEncodeUTF16RuneMaps(t *testing.T) {
	cases := []struct {
		name    string
		in      []rune
		u       []uint16
		startAt []int
		endAt   []int
	}{
		{"ascii", []rune("ab"), []uint16{'a', 'b'}, []int{0, 1, 2}, []int{0, 1, 2}},
		{"cjk", []rune("中文"), []uint16{0x4E2D, 0x6587}, []int{0, 1, 2}, []int{0, 1, 2}},
		{"emoji", []rune("a😀b"), []uint16{'a', 0xD83D, 0xDE00, 'b'}, []int{0, 1, 1, 2, 3}, []int{0, 1, 2, 2, 3}},
		{"invalid", []rune{'a', 0xD800, -1, 'b'}, []uint16{'a', 0xFFFD, 0xFFFD, 'b'}, []int{0, 1, 2, 3, 4}, []int{0, 1, 2, 3, 4}},
		{"empty", nil, []uint16{}, []int{0}, []int{0}},
	}
	for _, c := range cases {
		u, s, e := encodeUTF16(c.in)
		if !reflect.DeepEqual(u, c.u) || !reflect.DeepEqual(s, c.startAt) || !reflect.DeepEqual(e, c.endAt) {
			t.Errorf("%s: got u=%x start=%v end=%v; want u=%x start=%v end=%v", c.name, u, s, e, c.u, c.startAt, c.endAt)
		}
	}
}

func TestSpellRangesFromUTF16(t *testing.T) {
	// "😀 teh" → units: D83D DE00 ' ' t e h ; "teh" = units 3..6 = runes 2..5.
	_, s, e := encodeUTF16([]rune("😀 teh"))
	got := spellRangesFromUTF16([]int32{3, 3}, s, e)
	if want := []SpellRange{{2, 5}}; !reflect.DeepEqual(got, want) {
		t.Fatalf("after emoji: got %v want %v", got, want)
	}
	// Mid-surrogate start floors to the rune, end clamps to text length;
	// empty and negative ranges are dropped; output sorted.
	got = spellRangesFromUTF16([]int32{3, 1, 1, 99, 2, 0, -5, 1}, s, e)
	if want := []SpellRange{{0, 5}, {2, 3}}; !reflect.DeepEqual(got, want) {
		t.Fatalf("clamp: got %v want %v", got, want)
	}
	// An end inside a surrogate pair rounds up to include the rune.
	got = spellRangesFromUTF16([]int32{0, 1}, s, e)
	if want := []SpellRange{{0, 1}}; !reflect.DeepEqual(got, want) {
		t.Fatalf("clamp: got %v want %v", got, want)
	}
}

type fakeSpell struct{ noSpell }

type noSpell struct{}

func (noSpell) Available() bool                            { return true }
func (noSpell) Check(string, string) ([]SpellRange, error) { return []SpellRange{{0, 1}}, nil }
func (noSpell) Suggestions(string, string) []string        { return []string{"x"} }
func (noSpell) Learn(string)                               {}
func (noSpell) Unlearn(string)                             {}
func (noSpell) HasLearned(string) bool                     { return false }

func TestSetSpellCheckerOverride(t *testing.T) {
	if SystemSpellChecker() == nil {
		t.Fatal("SystemSpellChecker returned nil")
	}
	f := &fakeSpell{}
	SetSpellChecker(f)
	defer SetSpellChecker(nil)
	if CurrentSpellChecker() != SpellChecker(f) {
		t.Fatal("override not returned")
	}
	if !SpellCheckAvailable() || !reflect.DeepEqual(SpellCheck("a", ""), []SpellRange{{0, 1}}) ||
		!reflect.DeepEqual(SpellSuggestions("a", ""), []string{"x"}) {
		t.Fatal("conveniences do not use override")
	}
	SetSpellChecker(nil)
	if CurrentSpellChecker() != SystemSpellChecker() {
		t.Fatal("nil did not restore system checker")
	}
}

func TestDrawDecorationLineWavyStrokesPath(t *testing.T) {
	var rc RecordingCanvas
	DrawDecorationLine(&rc, DecorationWavy, 10, 20, 30, 1, Color{R: 1, A: 1})
	if len(rc.Paths) != 1 || !rc.Paths[0].Stroke {
		t.Fatalf("want one stroked path, got %+v", rc.Paths)
	}
	if p := rc.Paths[0]; p.X < 9.5 || p.X+p.W > 40.5 {
		t.Fatalf("path spans %v, want within [10,40]", p.Rect)
	}
}
