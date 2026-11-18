package qui

import "testing"

// imeSpy is an IME client that composes only while active (IMEActivity).
type imeSpy struct {
	BaseWidget
	active  bool
	preedit string
	commits []string
}

func newIMESpy() *imeSpy {
	s := &imeSpy{BaseWidget: NewBaseWidget()}
	s.SetSelf(s)
	return s
}

func (s *imeSpy) Focusable() bool               { return true }
func (s *imeSpy) SetFocused(bool)               {}
func (s *imeSpy) SetPreedit(text string, _ int) { s.preedit = text }
func (s *imeSpy) CommitIME(text string)         { s.commits = append(s.commits, text) }
func (s *imeSpy) CaretRect() Rect               { return Rect{X: 5, Y: 5, W: 1, H: 10} }
func (s *imeSpy) IMEActive() bool               { return s.active }

func TestIMEActivityOptOut(t *testing.T) {
	spy := newIMESpy()
	w := NewTestWindow(Size{W: 200, H: 100})
	w.SetRoot(NewContainer(nil, spy))
	w.SetFocus(spy)

	// Inactive: not an IME client — nothing composes, no caret for the
	// candidate window, and the platform bypasses the input method.
	w.SetPreedit("ni", 2)
	w.CommitIME("你")
	if spy.preedit != "" || len(spy.commits) != 0 {
		t.Fatalf("inactive client received IME input: %q %v", spy.preedit, spy.commits)
	}
	if r := w.CaretRect(); r != (Rect{}) {
		t.Fatalf("inactive client has a caret rect %v", r)
	}
	if _, _, _, _, ok := w.imeCaretRect(); ok {
		t.Fatal("imeCaretRect reported an inactive client")
	}
	if !w.IMEBypassed() {
		t.Fatal("inactive client not bypassed")
	}

	// Active: a normal client.
	spy.active = true
	w.SetPreedit("ni", 2)
	w.CommitIME("你")
	if spy.preedit != "ni" || len(spy.commits) != 1 || w.IMEBypassed() {
		t.Fatalf("active client: preedit %q commits %v bypassed %v", spy.preedit, spy.commits, w.IMEBypassed())
	}
	if r := w.CaretRect(); r.H != 10 {
		t.Fatalf("active caret rect %v", r)
	}

	// A widget that is not an IME client at all is never bypassed (its
	// typed text still goes through the input method as before).
	plain := NewContainer(nil)
	w.SetRoot(NewContainer(nil, plain))
	w.SetFocus(nil)
	if w.IMEBypassed() {
		t.Fatal("no focus reported as bypassed")
	}
}
