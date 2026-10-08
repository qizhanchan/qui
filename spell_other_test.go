//go:build !darwin || !cgo

package qui

import (
	"errors"
	"testing"
)

func TestSpellUnavailableOffDarwin(t *testing.T) {
	c := SystemSpellChecker()
	if c.Available() {
		t.Fatal("Available() = true without a backend")
	}
	if rs, err := c.Check("teh", ""); rs != nil || !errors.Is(err, ErrSpellUnavailable) {
		t.Fatalf("Check = %v, %v", rs, err)
	}
	if c.Suggestions("teh", "") != nil || c.HasLearned("teh") {
		t.Fatal("expected no-op checker")
	}
}
