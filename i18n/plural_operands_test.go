package i18n

import (
	"math"
	"testing"

	"golang.org/x/text/feature/plural"
)

// Fractions take their CLDR operands from the displayed (minimal) form:
// in Croatian 0.1 is "one" (f % 10 = 1), 0.5 is "other".
func TestPluralOperandsUseDisplayedFraction(t *testing.T) {
	b := NewBundle()
	for n, want := range map[float64]plural.Form{0.1: plural.One, 0.5: plural.Other, 1: plural.One, 2: plural.Few, 5: plural.Other} {
		if got := b.pluralForm("hr", n); got != want {
			t.Errorf("hr %v: %v, want %v", n, got, want)
		}
	}
}

// Non-finite and huge counts pick a form instead of panicking in x/text.
func TestPluralFormNonFiniteAndHuge(t *testing.T) {
	b := NewBundle()
	for _, n := range []float64{math.NaN(), math.Inf(1), math.Inf(-1), math.MaxInt64, 1e300, -1e20} {
		b.pluralForm("en", n)
		b.pluralForm("fr", n)
		b.pluralForm("ru", n)
	}
}
