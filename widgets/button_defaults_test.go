package widgets

import (
	"testing"

	. "github.com/qizhanchan/qui"
)

// TestButtonDefaultStatesRawHTML asserts the zero-configuration
// widgets.NewButton produces a raw-HTML-like surface: a visible light
// container fill, dark label, thin gray border, and no shadow. If
// someone regresses the default into a designed look this fires — the
// designed look belongs in CSS on the htmlcss layer.
func TestButtonDefaultStatesRawHTML(t *testing.T) {
	b := NewButton("OK", nil)
	if b.States.Base.Background.A == 0 {
		t.Errorf("default Background must be non-transparent (raw HTML), got %+v", b.States.Base.Background)
	}
	if b.States.Base.Border.A == 0 {
		t.Errorf("default Border must be non-transparent (raw HTML), got %+v", b.States.Base.Border)
	}
	if b.States.Hover == nil {
		t.Fatal("default hover state missing")
	}
	if b.States.Hover.Background == b.States.Base.Background {
		t.Error("default hover Background should differ from Base (subtle darken)")
	}
	if b.Elevation != 0 || b.HoverElevation != 0 {
		t.Errorf("default should be flat (Elevation=0, HoverElevation=0), got %d/%d",
			b.Elevation, b.HoverElevation)
	}
}

// TestButtonMeasureUsesStyleHeight confirms Measure honors
// States.Base.Height when set — how a caller pins a button to a fixed
// control height.
func TestButtonMeasureUsesStyleHeight(t *testing.T) {
	b := NewButton("Send", nil)
	b.States.Base.Height = 40
	got := b.Measure(Size{W: 200})
	if got.H != 40 {
		t.Errorf("Measure H = %v, want 40 (from Style.Height)", got.H)
	}
}

func TestStatefulControlsSetStyleUpdatesStateBase(t *testing.T) {
	want := Style{Foreground: Color{R: 1, A: 1}, Padding: Insets{Left: 17}}

	button := NewButton("Button", nil)
	button.SetStyle(want)
	if got := button.States.Base; got.Foreground != want.Foreground || got.Padding != want.Padding {
		t.Fatalf("Button.SetStyle updated wrong storage: %+v", got)
	}

	input := NewInput("Input")
	input.SetStyle(want)
	if got := input.States.Base; got.Foreground != want.Foreground || got.Padding != want.Padding {
		t.Fatalf("Input.SetStyle updated wrong storage: %+v", got)
	}

	selectBox := NewSelect(nil, []string{"A"}, nil)
	selectBox.SetStyle(want)
	if got := selectBox.States.Base; got.Foreground != want.Foreground || got.Padding != want.Padding {
		t.Fatalf("Select.SetStyle updated wrong storage: %+v", got)
	}
}
