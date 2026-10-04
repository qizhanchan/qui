package widgets

import (
	"testing"
	"time"

	. "github.com/qizhanchan/qui"
)

// ---- Theme / tokens ----

func TestCurrentThemeReturnsStablePointer(t *testing.T) {
	p1 := CurrentTheme()
	p2 := CurrentTheme()
	if p1 != p2 {
		t.Error("CurrentTheme() should return a stable pointer")
	}
}

func TestSetThemeSwapsValues(t *testing.T) {
	orig := *CurrentTheme()
	defer SetTheme(orig)

	SetTheme(LightTheme)
	if CurrentTheme().Surface != LightTheme.Surface {
		t.Errorf("after SetTheme(Light), Surface = %+v, want %+v",
			CurrentTheme().Surface, LightTheme.Surface)
	}

	custom := LightTheme
	custom.Surface = Color{R: 0.1, G: 0.1, B: 0.1, A: 1}
	SetTheme(custom)
	if CurrentTheme().Surface != custom.Surface {
		t.Errorf("after SetTheme(custom), Surface = %+v, want %+v",
			CurrentTheme().Surface, custom.Surface)
	}
}

func TestSetThemeFiresSubscribers(t *testing.T) {
	orig := *CurrentTheme()
	defer SetTheme(orig)

	called := 0
	SubscribeTheme(func() { called++ })
	SetTheme(LightTheme)
	if called == 0 {
		t.Error("SetTheme should fire subscribers at least once")
	}
}

func TestThemesShareSpacingTokens(t *testing.T) {
	// Recoloring a theme must not reflow layout — spacing stays identical.
	custom := LightTheme
	custom.Surface = Color{R: 0.1, G: 0.1, B: 0.1, A: 1}
	if custom.Spacing3 != LightTheme.Spacing3 {
		t.Errorf("Spacing3 mismatch: custom=%v light=%v",
			custom.Spacing3, LightTheme.Spacing3)
	}
	if custom.RadiusMedium != LightTheme.RadiusMedium {
		t.Error("Radius tokens should match across themes")
	}
}

// ---- StateStyle ----

func TestStateStyleResolveFallsBackToBase(t *testing.T) {
	s := &StateStyle{Base: Style{Background: ColorWhite}}
	// No overrides — any state returns base.
	if s.Resolve(StateHover).Background != ColorWhite {
		t.Error("no Hover override, should fall back to Base")
	}
}

func TestStateStylePriorityDisabledWinsOverPressed(t *testing.T) {
	s := &StateStyle{
		Base: Style{Background: ColorWhite},
	}
	s.Pressed = StyleWith(s.Base, Background(ColorBlue))
	s.Disabled = StyleWith(s.Base, Background(ColorGray))

	// Both flags set.
	got := s.Resolve(StatePressed | StateDisabled).Background
	if got != ColorGray {
		t.Errorf("Disabled should win over Pressed; got %+v", got)
	}
}

func TestStateStylePressedWinsOverHover(t *testing.T) {
	s := &StateStyle{Base: Style{Background: ColorWhite}}
	s.Hover = StyleWith(s.Base, Background(ColorBlue))
	s.Pressed = StyleWith(s.Base, Background(ColorGray))
	got := s.Resolve(StatePressed | StateHover).Background
	if got != ColorGray {
		t.Errorf("Pressed should win over Hover; got %+v", got)
	}
}

func TestStyleWithMutatorsComposable(t *testing.T) {
	base := Style{Background: ColorWhite, Foreground: ColorBlack}
	out := StyleWith(base, Background(ColorBlue), Foreground(ColorWhite))
	if out.Background != ColorBlue {
		t.Errorf("Background mutator: got %+v", out.Background)
	}
	if out.Foreground != ColorWhite {
		t.Errorf("Foreground mutator: got %+v", out.Foreground)
	}
	// Base is untouched.
	if base.Background != ColorWhite {
		t.Error("StyleWith must not mutate base")
	}
}

// ---- Color helpers ----

func TestAdjustBrightnessClamps(t *testing.T) {
	c := Color{R: 0.5, G: 0.9, B: 0.1, A: 1}
	// Lighten 0.5 → R=1, G=1, B=0.6
	lighter := AdjustBrightness(c, 0.5)
	if lighter.R != 1 {
		t.Errorf("R should clamp to 1; got %v", lighter.R)
	}
	if lighter.G != 1 {
		t.Errorf("G should clamp to 1; got %v", lighter.G)
	}
	if lighter.B < 0.59 || lighter.B > 0.61 {
		t.Errorf("B = %v, want ~0.6", lighter.B)
	}
	// Darken 0.5 → R=0, G=0.4, B=0 (clamped)
	darker := AdjustBrightness(c, -0.5)
	if darker.R != 0 {
		t.Errorf("R should clamp to 0; got %v", darker.R)
	}
	// Alpha preserved.
	if lighter.A != 1 || darker.A != 1 {
		t.Error("AdjustBrightness should preserve alpha")
	}
}

func TestLerpColorEndpoints(t *testing.T) {
	a := Color{R: 1, G: 0, B: 0, A: 1}
	b := Color{R: 0, G: 0, B: 1, A: 1}

	if LerpColor(a, b, 0) != a {
		t.Error("Lerp(a, b, 0) should return a")
	}
	if LerpColor(a, b, 1) != b {
		t.Error("Lerp(a, b, 1) should return b")
	}
	mid := LerpColor(a, b, 0.5)
	if mid.R < 0.49 || mid.R > 0.51 {
		t.Errorf("mid R = %v, want ~0.5", mid.R)
	}
	if mid.B < 0.49 || mid.B > 0.51 {
		t.Errorf("mid B = %v, want ~0.5", mid.B)
	}
}

func TestLerpColorClampsOutOfRangeT(t *testing.T) {
	a := Color{R: 1, A: 1}
	b := Color{R: 0, A: 1}
	if LerpColor(a, b, -1) != a {
		t.Error("negative t should clamp to 0")
	}
	if LerpColor(a, b, 2) != b {
		t.Error("t > 1 should clamp to 1")
	}
}

// ---- Transition ----

func TestTransitionNotStartedReturnsTo(t *testing.T) {
	var tr Transition
	tr.To = 0.5
	if got := tr.Value(time.Now()); got != 0.5 {
		t.Errorf("unstarted Value = %v, want To (0.5)", got)
	}
}

func TestTransitionInterpolatesWithEaseOut(t *testing.T) {
	var tr Transition
	tr.Duration = 100 * time.Millisecond
	start := time.Now()
	tr.Begin(0, 1, start)

	// Halfway in time, easeOutCubic(0.5) = 1 - 0.5^3 = 0.875.
	mid := start.Add(50 * time.Millisecond)
	got := tr.Value(mid)
	if got < 0.85 || got > 0.9 {
		t.Errorf("mid Value = %v, want ~0.875 (easeOutCubic)", got)
	}
}

func TestTransitionCompletesAtEnd(t *testing.T) {
	var tr Transition
	tr.Duration = 50 * time.Millisecond
	start := time.Now()
	tr.Begin(0, 1, start)

	after := start.Add(100 * time.Millisecond)
	if got := tr.Value(after); got != 1 {
		t.Errorf("past end Value = %v, want 1", got)
	}
	if tr.Active(after) {
		t.Error("transition should be inactive past duration")
	}
}

func TestTransitionBeginMidFlightReAnchors(t *testing.T) {
	var tr Transition
	tr.Duration = 100 * time.Millisecond
	start := time.Now()
	tr.Begin(0, 1, start)

	// Interrupt at t=50ms (value ~0.875) with a new reverse transition.
	mid := start.Add(50 * time.Millisecond)
	current := tr.Value(mid)
	tr.Begin(current, 0, mid)

	// At mid, value should still be current.
	if got := tr.Value(mid); got != current {
		t.Errorf("just after Begin: Value = %v, want %v", got, current)
	}
	// Later, it should approach 0.
	later := mid.Add(100 * time.Millisecond)
	if got := tr.Value(later); got != 0 {
		t.Errorf("after full reverse: Value = %v, want 0", got)
	}
}

// ---- Button + theme integration ----

func TestButtonHoverTransitionDurationFromTheme(t *testing.T) {
	// The zero-configuration Button pulls its hover-transition duration
	// from Theme.TransitionShort at construction time — this is the
	// last thing left in NewButton that reads Theme (color choices are
	// now theme-independent HTML defaults). If NewButton stopped calling
	// the theme, hover animation would silently break.
	b := NewButton("ok", nil)
	want := time.Duration(CurrentTheme().TransitionShort)
	if b.hoverTrans.Duration != want {
		t.Errorf("hover duration = %v, want %v", b.hoverTrans.Duration, want)
	}
}

func TestButtonHoverEventsDriveTransition(t *testing.T) {
	b := NewButton("ok", nil)
	b.Layout(Rect{X: 0, Y: 0, W: 60, H: 24})

	// MouseEnter should begin a transition to 1.
	b.Handle(NewMouseEvent(EventMouseEnter, 0, 0, MouseButtonLeft, 0))
	// Immediately after, Active should be true (duration > 0 after theme init).
	if !b.hoverTrans.Active(time.Now()) {
		t.Error("MouseEnter did not start an active transition")
	}
	if b.hoverTrans.To != 1 {
		t.Errorf("MouseEnter To = %v, want 1", b.hoverTrans.To)
	}

	b.Handle(NewMouseEvent(EventMouseLeave, 0, 0, MouseButtonLeft, 0))
	if b.hoverTrans.To != 0 {
		t.Errorf("MouseLeave To = %v, want 0", b.hoverTrans.To)
	}
}

func TestButtonTickReportsDirtyWhileTransitioning(t *testing.T) {
	b := NewButton("ok", nil)
	b.Layout(Rect{X: 0, Y: 0, W: 60, H: 24})

	// Begin a transition in the future so it's "active" now.
	now := time.Now()
	b.hoverTrans.Duration = 200 * time.Millisecond
	b.hoverTrans.Begin(0, 1, now)

	dirty := b.Tick(now)
	if dirty.IsEmpty() {
		t.Error("Tick during active transition should return dirty bounds")
	}

	// After duration elapsed, no more dirty.
	future := now.Add(300 * time.Millisecond)
	if !b.Tick(future).IsEmpty() {
		t.Error("Tick after transition complete should return empty")
	}
}

func TestSetThemeInvalidatesSubscribedWindow(t *testing.T) {
	orig := *CurrentTheme()
	defer SetTheme(orig)

	var fired int
	SubscribeTheme(func() { fired++ })

	SetTheme(LightTheme)
	if fired == 0 {
		t.Error("theme subscription fired 0 times")
	}
}
