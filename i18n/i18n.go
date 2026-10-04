package i18n

import (
	"embed"
	"io/fs"

	"github.com/qizhanchan/qui"
)

// builtinFS carries the framework's own strings — the handful of labels
// qui itself renders (dialog buttons, default form-control captions,
// empty-state text) under the reserved "qui." key prefix.
//
// Applications override any of them by registering the same key later;
// Register merges key by key, and Install loads the builtins first.
//
//go:embed locales/*.json
var builtinFS embed.FS

// defaultBundle backs the package-level T / TN / In helpers and is what
// Install hands to the engine.
var defaultBundle = NewBundle()

// Default returns the package-level bundle, for apps that want to
// inspect or extend it directly.
func Default() *Bundle { return defaultBundle }

// Install loads the framework's builtin catalogs and registers the
// default bundle as the engine's Translator.
//
// Call once at startup, before constructing widgets. Loading
// application catalogs before or after Install both work — the bundle
// is live, and any window created later resolves through it. Loading
// them AFTER a window exists also works but needs a
// qui.SetDefaultLocale (or SetTranslator) call to force the repaint,
// since merely mutating a catalog does not bump the locale generation.
func Install() error {
	if err := defaultBundle.Load(builtinFS, "locales"); err != nil {
		return err
	}
	qui.SetTranslator(defaultBundle)
	return nil
}

// Load adds application catalogs to the default bundle. See
// Bundle.Load for the file naming rules.
func Load(fsys fs.FS, dir string) error { return defaultBundle.Load(fsys, dir) }

// Register adds one locale's messages to the default bundle.
func Register(loc qui.Locale, msgs map[string]Message) { defaultBundle.Register(loc, msgs) }

// T translates key in the active locale.
//
// Placeholders come as alternating name/value pairs or as a single map:
//
//	i18n.T("file.save")
//	i18n.T("greet", "name", user.Name)
//	i18n.T("greet", map[string]any{"name": user.Name})
//
// An unresolvable key returns the key itself (bracketed and logged once
// under QUI_I18N_STRICT=1) — see qui.Translate.
//
// Note for widget authors: do NOT call this at construction time and
// store the result. qui is retained-mode, so a string captured when the
// widget was built survives a language switch. Use the widget's TextKey
// field, which resolves during Draw.
func T(key string, args ...any) string {
	return qui.Translate("", key, argsFromPairs(args))
}

// TN translates key with a plural count, selecting the CLDR category
// for n in the active locale. {n} is bound to the count automatically.
//
//	i18n.TN("list.count", len(items))
//	i18n.TN("inbox.unread", n, "folder", name)
func TN(key string, n int, args ...any) string {
	return qui.TranslatePlural("", key, float64(n), argsFromPairs(args))
}

// TNf is TN for a fractional count ("0.5 hours"). Rare in UI text, but
// CLDR distinguishes it: several languages put fractional values in a
// different plural category than the same integer.
func TNf(key string, n float64, args ...any) string {
	return qui.TranslatePlural("", key, n, argsFromPairs(args))
}

// In returns a Printer bound to an explicit locale, for code that
// formats for a locale other than the active one — a per-window
// language, an export, a server-side render.
func In(loc qui.Locale) *Printer { return newPrinter(loc) }

// ForWindow returns a Printer bound to a window's effective locale,
// honoring any per-window override.
func ForWindow(w *qui.Window) *Printer { return newPrinter(w.Locale()) }
