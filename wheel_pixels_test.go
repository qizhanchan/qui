package qui

import "testing"

func TestWheelPixels(t *testing.T) {
	notch := MouseEvent{DeltaY: 1}
	if _, dy := notch.WheelPixels(); dy != WheelLinePixels {
		t.Errorf("wheel notch = %v px, want %v", dy, WheelLinePixels)
	}
	pad := MouseEvent{DeltaX: -0.3, DeltaY: 1.2, PreciseScroll: true}
	if dx, dy := pad.WheelPixels(); dx != -3 || dy != 12 {
		t.Errorf("trackpad = %v,%v px, want -3,12", dx, dy)
	}
	phased := MouseEvent{DeltaY: 0.5, ScrollPhase: GesturePhaseChanged}
	if _, dy := phased.WheelPixels(); dy != 5 {
		t.Errorf("phased = %v px, want 5", dy)
	}
}
