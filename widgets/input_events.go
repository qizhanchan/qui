package widgets

// BeforeInputEvent is emitted before a text mutation. Returning false
// from BeforeInput cancels the incoming edit.
type BeforeInputEvent struct {
	Kind      string
	Text      string
	CursorPos int
	SelStart  int
	SelEnd    int
	Value     string
}
