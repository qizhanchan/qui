package qui

import "testing"

func TestStateLayerColorPicksHigherPriority(t *testing.T) {
	th := CurrentTheme()
	if _, op := StateLayerColor(ColorBlack, StatePressed|StateHover, th); op != th.PressedOpacity {
		t.Errorf("press should outrank hover, got %v", op)
	}
	if _, op := StateLayerColor(ColorBlack, StateHover, th); op != th.HoverOpacity {
		t.Errorf("hover only: got %v, want %v", op, th.HoverOpacity)
	}
	if _, op := StateLayerColor(ColorBlack, 0, th); op != 0 {
		t.Errorf("idle should give 0 opacity, got %v", op)
	}
}

// ThemeFont resolves off the live theme's font scale, so a widget that
// asks for a label face follows SetTheme instead of a frozen table.
func TestThemeFontFollowsThemeScale(t *testing.T) {
	if f := ThemeFont(TextLabel); f.Size != CurrentTheme().FontBase || f.Weight != FontWeightMedium {
		t.Errorf("TextLabel font wrong: %+v", f)
	}
	if f := ThemeFont(TextBody); f.Weight != FontWeightNormal {
		t.Errorf("TextBody should be normal weight, got %+v", f)
	}

	orig := CurrentTheme().FontBase
	bumped := *CurrentTheme()
	bumped.FontBase = orig + 4
	SetTheme(bumped)
	defer SetTheme(LightTheme)

	if f := ThemeFont(TextBody); f.Size != orig+4 {
		t.Errorf("after SetTheme, TextBody size = %v, want %v", f.Size, orig+4)
	}
}

// An out-of-range role must fall back to body text rather than panic —
// callers cast ints in from config files.
func TestThemeFontOutOfRangeFallsBack(t *testing.T) {
	got, want := ThemeFont(numTextRoles+7), ThemeFont(TextBody)
	if got.Size != want.Size || got.Weight != want.Weight {
		t.Errorf("out-of-range role = %+v, want body %+v", got, want)
	}
}
