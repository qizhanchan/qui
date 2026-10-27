package qui

import (
	"errors"
	"testing"
)

// These tests cover the Go surface — option defaulting, error
// contracts, and that public symbols exist. The panels themselves
// are interactive so end-to-end verification lives in
// examples/filedialog.

func TestAlertStyleConstants(t *testing.T) {
	// Guard against accidental reordering — the darwin bridge maps
	// these values to NSAlertStyle by integer switch.
	if AlertInfo != 0 || AlertWarning != 1 || AlertCritical != 2 {
		t.Errorf("AlertStyle constants drifted: Info=%d Warning=%d Critical=%d",
			AlertInfo, AlertWarning, AlertCritical)
	}
}

func TestErrDialogNotSupportedIsSentinel(t *testing.T) {
	// Callers should be able to branch with errors.Is on the sentinel
	// rather than comparing error strings.
	wrapped := errors.Join(errors.New("ctx"), ErrDialogNotSupported)
	if !errors.Is(wrapped, ErrDialogNotSupported) {
		t.Error("ErrDialogNotSupported must be detectable via errors.Is")
	}
}
