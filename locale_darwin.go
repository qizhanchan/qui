//go:build darwin && cgo

package qui

import "github.com/qizhanchan/qui/internal/platform/macos"

// platformSystemLocale reads the user's preferred language from
// NSLocale. macOS returns the FIRST entry of the user's ordered
// language list (System Settings > General > Language & Region), which
// is what an app should follow — [NSLocale currentLocale] reports the
// region formatting locale and can differ (an English UI in Germany).
func platformSystemLocale() Locale {
	return Locale(macos.PreferredLocale())
}
