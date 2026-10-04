//go:build !darwin

package qui

import (
	"os"
	"strings"
)

// platformSystemLocale reads the POSIX locale environment, which is what
// both X11 and Wayland desktops export. Precedence follows the POSIX
// spec: LC_ALL overrides LC_MESSAGES, which overrides LANG.
//
// Values arrive as "zh_CN.UTF-8" or "de_DE@euro"; strip the charset and
// modifier and let Locale.canonical fix the separator and casing.
// "C" and "POSIX" mean "no locale", reported as "" so DefaultLocale can
// fall through to its own default rather than inventing a language.
func platformSystemLocale() Locale {
	for _, key := range []string{"LC_ALL", "LC_MESSAGES", "LANG"} {
		v := strings.TrimSpace(os.Getenv(key))
		if v == "" {
			continue
		}
		if i := strings.IndexAny(v, ".@"); i >= 0 {
			v = v[:i]
		}
		if v == "" || v == "C" || v == "POSIX" {
			continue
		}
		return Locale(v)
	}
	return ""
}
