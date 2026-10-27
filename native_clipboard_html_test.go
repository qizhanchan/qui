package qui

import "testing"

// htmlClip is a provider that implements BOTH rich interfaces — the shape a
// real platform provider has.
type htmlClip struct{ plain, html string }

func (c *htmlClip) Get() string               { return c.plain }
func (c *htmlClip) Set(s string)              { c.plain, c.html = s, "" }
func (c *htmlClip) SetRich(p, h string)       { c.plain, c.html = p, h }
func (c *htmlClip) GetRich() (string, string) { return c.plain, c.html }

func TestGetClipboardHTMLReadsTheRichFlavor(t *testing.T) {
	fc := &htmlClip{}
	SetClipboardProvider(fc)
	t.Cleanup(func() { SetClipboardProvider(nil) })

	SetClipboardRich("bold", "<b>bold</b>")
	if got := GetClipboardText(); got != "bold" {
		t.Errorf("plain flavor = %q, want %q", got, "bold")
	}
	// The charset declaration SetClipboardRich prepends is part of what a
	// consumer reads back — it is the flavor's own encoding statement.
	if got, want := GetClipboardHTML(), `<meta charset="utf-8"><b>bold</b>`; got != want {
		t.Errorf("HTML flavor = %q, want %q", got, want)
	}

	// A plain-text write clears the HTML: the two flavors always describe the
	// same copy, so a stale fragment must not outlive it.
	SetClipboardText("just text")
	if got := GetClipboardHTML(); got != "" {
		t.Errorf("HTML flavor survived a plain write: %q", got)
	}
}

// The two flavors come back as ONE pair, because a caller chooses between them:
// text from this copy beside HTML from the last one is the failure the snapshot
// exists to prevent.
func TestGetClipboardRichReturnsBothFlavors(t *testing.T) {
	fc := &htmlClip{}
	SetClipboardProvider(fc)
	t.Cleanup(func() { SetClipboardProvider(nil) })

	SetClipboardRich("bold", "<b>bold</b>")
	plain, html := GetClipboardRich()
	if plain != "bold" || html != `<meta charset="utf-8"><b>bold</b>` {
		t.Errorf("GetClipboardRich = (%q, %q)", plain, html)
	}
	SetClipboardText("just text")
	if plain, html := GetClipboardRich(); plain != "just text" || html != "" {
		t.Errorf("after a plain write: (%q, %q), want the text and no HTML", plain, html)
	}
}

// A provider that only implements the write side (every non-darwin platform
// today) reads back no HTML rather than panicking on the type assertion — and
// its plain text still comes through.
func TestGetClipboardHTMLWithoutAReaderIsEmpty(t *testing.T) {
	fc := &richClip{}
	SetClipboardProvider(fc)
	t.Cleanup(func() { SetClipboardProvider(nil) })

	SetClipboardRich("bold", "<b>bold</b>")
	if got := GetClipboardHTML(); got != "" {
		t.Errorf("GetClipboardHTML = %q, want \"\" from a write-only provider", got)
	}
	if plain, html := GetClipboardRich(); plain != "bold" || html != "" {
		t.Errorf("GetClipboardRich = (%q, %q), want the plain flavor and no HTML", plain, html)
	}
	if got := GetClipboardText(); got != "bold" {
		t.Errorf("the plain flavor should still be readable, got %q", got)
	}
}

// The no-op provider (no window, no fake installed) is inert.
func TestGetClipboardHTMLWithNoProvider(t *testing.T) {
	SetClipboardProvider(nil)
	if got := GetClipboardHTML(); got != "" {
		t.Errorf("GetClipboardHTML = %q, want \"\"", got)
	}
}
