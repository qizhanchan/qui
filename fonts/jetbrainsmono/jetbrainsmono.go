// Package jetbrainsmono bundles the JetBrains Mono typeface (v2.304) and
// a one-liner that installs it as qui's global default font.
//
// JetBrains Mono is the default editor font in GoLand and the other
// JetBrains IDEs. It is a MONOSPACE face designed for code: every glyph
// occupies an equal-width cell, so a narrow letter like lowercase "l"
// gets roomy side-bearing instead of crowding its neighbours the way it
// does in a proportional font (qui's bundled Go font). The design also
// disambiguates the commonly-confused "1", "l" and "I". This is what
// makes "ll123" read cleanly the way it does in VS Code / GoLand.
//
// Usage — one line at startup, before building widgets:
//
//	import "github.com/qizhanchan/qui/fonts/jetbrainsmono"
//
//	func main() {
//	    jetbrainsmono.Use() // whole UI now renders in JetBrains Mono
//	    // ... NewApp / NewWindow / SetRoot ...
//	}
//
// Use replaces the global default font (all weights), so buttons,
// labels, dialogs and text inputs all switch together. If you only want
// monospace in specific widgets, set their Style().Font.Family to
// jetbrainsmono.Family instead and skip Use.
//
// The font is licensed under the SIL Open Font License 1.1 (see OFL.txt);
// free for commercial and non-commercial use.
package jetbrainsmono

import (
	_ "embed"

	"github.com/qizhanchan/qui"
)

// Family is the family name JetBrains Mono is registered under. Target it
// via qui.Font{Family: jetbrainsmono.Family} to use the face for a single
// widget without changing the global default.
const Family = "JetBrains Mono"

var (
	//go:embed ttf/JetBrainsMono-Regular.ttf
	regular []byte
	//go:embed ttf/JetBrainsMono-Medium.ttf
	medium []byte
	//go:embed ttf/JetBrainsMono-Bold.ttf
	bold []byte
	//go:embed ttf/JetBrainsMono-Italic.ttf
	italic []byte
	//go:embed ttf/JetBrainsMono-MediumItalic.ttf
	mediumItalic []byte
	//go:embed ttf/JetBrainsMono-BoldItalic.ttf
	boldItalic []byte
)

// Use installs JetBrains Mono as the global default font for every widget,
// including the weighted (Medium/Bold) Material typescales. Returns an
// error only if an embedded face fails to parse (should never happen with
// the bundled bytes). Safe to call after widgets have been drawn — the
// face cache is invalidated, so the next frame re-rasterizes.
func Use() error {
	return qui.SetDefaultFont(Family, regular,
		qui.FontFace{Weight: qui.FontWeightMedium, Data: medium},
		qui.FontFace{Weight: qui.FontWeightBold, Data: bold},
		qui.FontFace{Weight: qui.FontWeightNormal, Italic: true, Data: italic},
		qui.FontFace{Weight: qui.FontWeightMedium, Italic: true, Data: mediumItalic},
		qui.FontFace{Weight: qui.FontWeightBold, Italic: true, Data: boldItalic},
	)
}

// Regular / Medium / Bold return the raw embedded TTF bytes so callers can
// register the face under their own family name or feed another loader.
func Regular() []byte { return regular }
func Medium() []byte  { return medium }
func Bold() []byte    { return bold }
