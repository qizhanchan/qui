package qui

import (
	"runtime"
	"strings"
	"sync"
)

// Clipboard is the OS clipboard abstraction used by text widgets for
// Cmd+C / Cmd+X / Cmd+V (Ctrl on non-macOS). Window installs a
// platform-backed provider on creation; tests swap in a fake via
// SetClipboardProvider so they don't need a real window.
type Clipboard interface {
	Get() string
	Set(text string)
}

var (
	clipboardMu     sync.RWMutex
	activeClipboard Clipboard = noopClipboard{}
)

// SetClipboardProvider installs a Clipboard implementation. Passing
// nil resets to the no-op provider — useful in tests' cleanup paths.
func SetClipboardProvider(c Clipboard) {
	clipboardMu.Lock()
	defer clipboardMu.Unlock()
	if c == nil {
		activeClipboard = noopClipboard{}
		return
	}
	activeClipboard = c
}

// GetClipboardText returns OS clipboard contents, or "" when no
// provider is installed or the clipboard is empty.
func GetClipboardText() string {
	clipboardMu.RLock()
	defer clipboardMu.RUnlock()
	return activeClipboard.Get()
}

// SetClipboardText writes text to the OS clipboard.
func SetClipboardText(text string) {
	clipboardMu.RLock()
	defer clipboardMu.RUnlock()
	activeClipboard.Set(text)
}

// RichClipboard is an optional capability a Clipboard provider may
// implement to publish formatted content alongside the plain-text
// fallback. `html` is a UTF-8 HTML fragment; consumers like Word /
// Pages / browsers pick it up to preserve bold / italic / size / color.
// The darwin provider implements it via NSPasteboard's public.html
// flavor; platforms without a rich provider fall back to plain text.
type RichClipboard interface {
	SetRich(plain, html string)
}

// SetClipboardRich writes both a plain-text and an HTML representation
// to the OS clipboard when the active provider supports it; otherwise it
// degrades to plain text. An empty html string also degrades to plain.
func SetClipboardRich(plain, html string) {
	clipboardMu.RLock()
	defer clipboardMu.RUnlock()
	if rc, ok := activeClipboard.(RichClipboard); ok && html != "" {
		rc.SetRich(plain, withCharsetMeta(html))
		return
	}
	activeClipboard.Set(plain)
}

// RichClipboardReader is the READ side of RichClipboard: a provider that can
// hand back the HTML flavor whoever last filled the clipboard published.
//
// Without it a rich clipboard is write-only, and an app can only paste
// formatting it put there itself — which means keeping the copied content in
// process memory and pasting rich only while the OS clipboard still looks like
// its own last copy. Every paste from ANOTHER process (another window of the
// same app included) then degrades to plain text, however good the markup on
// the pasteboard is.
//
// It returns BOTH flavors, from one snapshot, because a paste has to choose
// between them: read separately, the clipboard can change hands in between and
// leave a consumer holding one copy's markup beside the next copy's text.
type RichClipboardReader interface {
	GetRich() (plain, html string)
}

// GetClipboardRich returns the OS clipboard's plain-text and HTML flavors as
// they were at ONE moment. html is "" when the clipboard holds no HTML (a
// plain-text copy from a terminal, say) or the provider cannot read one, in
// which case plain still comes back through the ordinary text path.
//
// The HTML is whatever the source app published, so it is UNTRUSTED input: a
// consumer parses it, and must not follow anything in it — a reference in
// pasted markup is the source's choice, not the user's.
func GetClipboardRich() (plain, html string) {
	clipboardMu.RLock()
	defer clipboardMu.RUnlock()
	if rr, ok := activeClipboard.(RichClipboardReader); ok {
		return rr.GetRich()
	}
	return activeClipboard.Get(), ""
}

// GetClipboardHTML returns just the HTML flavor, for a caller that has no
// reason to correlate it with the text. Prefer GetClipboardRich when the two
// are compared against each other.
func GetClipboardHTML() string {
	_, html := GetClipboardRich()
	return html
}

// withCharsetMeta prepends a UTF-8 declaration to a clipboard HTML
// fragment.
//
// The pasteboard flavor itself is written as UTF-8, but a fragment that
// never says so is at the mercy of whatever a consumer assumes when it
// looks at the markup alone — and the fallback assumption is the system
// text encoding, which mangles every multi-byte character. Google Docs'
// clipboard HTML opens with exactly this tag for the same reason.
func withCharsetMeta(html string) string {
	if strings.Contains(strings.ToLower(html), "charset") {
		return html
	}
	return `<meta charset="utf-8">` + html
}

type noopClipboard struct{}

func (noopClipboard) Get() string { return "" }
func (noopClipboard) Set(string)  {}

// platformClipboard routes plain-text clipboard access through the
// platform seam. Windowing libraries tend to hang clipboard access off a
// window for historical reasons even though the OS clipboard is
// process-wide; the seam keeps that detail on the far side.
//
// Note for future backends: on Wayland a clipboard write needs a serial
// from a recent input event, so Set can legitimately do nothing when
// called outside an input handler — see platformCaps.ClipboardNeedsInputSerial.
type platformClipboard struct {
	win platformWindow
}

func (c platformClipboard) Get() string {
	if c.win == nil {
		return ""
	}
	return c.win.clipboardText()
}

func (c platformClipboard) Set(text string) {
	if c.win == nil {
		return
	}
	c.win.setClipboardText(text)
}

// CommandMod is the platform's primary shortcut modifier: ModSuper (⌘) on
// macOS, ModControl elsewhere. "CmdOrCtrl+S" in ParseShortcut resolves to
// it.
func CommandMod() Modifiers {
	if runtime.GOOS == "darwin" {
		return ModSuper
	}
	return ModControl
}

// IsCommandMod reports whether Mods contains the platform's
// "command" modifier — ModSuper (Cmd) on macOS, ModControl (Ctrl)
// elsewhere. Lets widgets write one branch for Cmd+C / Ctrl+C.
func IsCommandMod(m Modifiers) bool {
	if runtime.GOOS == "darwin" {
		return m&ModSuper != 0
	}
	return m&ModControl != 0
}
