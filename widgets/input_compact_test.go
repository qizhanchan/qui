package widgets

import (
	"testing"

	. "github.com/qizhanchan/qui"
)

// A plain Input (no floating Label) is a raw-HTML-style compact box; the
// tall form-field geometry only kicks in once a floating Label is set
// (which is what the material theme does). This locks in that M3 is an
// opt-in embellishment, not the base widget's default.
func TestInputDefaultCompactUntilLabeled(t *testing.T) {
	plain := NewInput("placeholder")
	labeled := NewInput("placeholder")
	labeled.Label = "Name"

	p := plain.Measure(Size{W: 240, H: 0})
	l := labeled.Measure(Size{W: 240, H: 0})

	if p.H >= l.H {
		t.Fatalf("plain input H=%.0f should be shorter than M3-labeled H=%.0f", p.H, l.H)
	}
	// Sanity: plain input should be in the button-height ballpark, not
	// the 56+ px M3 field.
	if p.H > 48 {
		t.Errorf("plain input H=%.0f is too tall for a raw HTML input", p.H)
	}
}

func TestSelectDefaultCompactUntilLabeled(t *testing.T) {
	plain := NewSelect(nil, []string{"a", "b"}, nil)
	labeled := NewSelect(nil, []string{"a", "b"}, nil)
	labeled.Label = "Pick"

	p := plain.Measure(Size{W: 240, H: 0})
	l := labeled.Measure(Size{W: 240, H: 0})

	if p.H >= l.H {
		t.Fatalf("plain select H=%.0f should be shorter than M3-labeled H=%.0f", p.H, l.H)
	}
}
