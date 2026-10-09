package qui

import (
	"errors"
	"testing"
)

type fakeKeyCapture struct {
	prepErr error
	calls   []bool
}

func (f *fakeKeyCapture) prepare() error               { return f.prepErr }
func (f *fakeKeyCapture) setActive(_ *Window, on bool) { f.calls = append(f.calls, on) }

func withFakeKeyCapture(t *testing.T, f *fakeKeyCapture) {
	t.Helper()
	prev := keyCapture
	keyCapture = f
	t.Cleanup(func() { keyCapture = prev })
}

func TestSystemKeyCaptureActivationRules(t *testing.T) {
	fake := &fakeKeyCapture{}
	withFakeKeyCapture(t, fake)
	w, target, other := rawFixture(t)
	w.keyCapture.osFocused = true

	if w.SystemKeyCaptureActive() {
		t.Fatal("capture must be off by default")
	}
	if err := w.SetSystemKeyCapture(true); err != nil {
		t.Fatal(err)
	}
	if !w.SystemKeyCaptureActive() {
		t.Fatal("enabled + key window + raw target should activate")
	}
	// Window resigns key: capture switches off by itself...
	w.onFocus(false)
	if w.SystemKeyCaptureActive() {
		t.Fatal("capture survived the window resigning key")
	}
	// ...and comes back when it is key again.
	w.onFocus(true)
	if !w.SystemKeyCaptureActive() {
		t.Fatal("capture did not resume on becoming key")
	}
	// Focus leaves the raw target.
	w.SetFocus(other)
	if w.SystemKeyCaptureActive() {
		t.Fatal("capture active without a raw target")
	}
	w.SetFocus(target)
	// The target stops wanting raw mode: noticed at the next sync point.
	target.raw = false
	w.syncKeyCapture()
	if w.SystemKeyCaptureActive() {
		t.Fatal("capture active after raw mode ended")
	}
	if err := w.SetSystemKeyCapture(false); err != nil {
		t.Fatal(err)
	}
	want := []bool{true, false, true, false, true, false}
	if len(fake.calls) != len(want) {
		t.Fatalf("platform transitions = %v, want %v", fake.calls, want)
	}
	for i := range want {
		if fake.calls[i] != want[i] {
			t.Fatalf("platform transitions = %v, want %v", fake.calls, want)
		}
	}
}

func TestSystemKeyCaptureErrors(t *testing.T) {
	withFakeKeyCapture(t, &fakeKeyCapture{prepErr: ErrAccessibilityPermission})
	w, _, _ := rawFixture(t)
	if err := w.SetSystemKeyCapture(true); !errors.Is(err, ErrAccessibilityPermission) {
		t.Fatalf("err = %v", err)
	}
	if w.keyCapture.enabled {
		t.Fatal("a refused enable must leave capture off")
	}
	keyCapture = nil
	if err := w.SetSystemKeyCapture(true); !errors.Is(err, ErrSystemKeyCaptureUnsupported) {
		t.Fatalf("err = %v", err)
	}
}

func TestCaptureReleasesHeldKeysOnDeactivate(t *testing.T) {
	withFakeKeyCapture(t, &fakeKeyCapture{})
	w, target, _ := rawFixture(t)
	w.keyCapture.osFocused = true
	_ = w.SetSystemKeyCapture(true)
	w.noteCapturedKey(0x30, KeyTab, true) // Cmd+Tab's Tab held via the tap
	w.noteCapturedKey(0x37, KeyLeftSuper, true)
	w.noteCapturedKey(0x37, KeyLeftSuper, false)
	w.onFocus(false)
	if len(target.keys) != 1 || target.keys[0].Key != KeyTab || target.keys[0].Type() != EventKeyUp {
		t.Fatalf("held keys not released on deactivation: %+v", target.keys)
	}
}
