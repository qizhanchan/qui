package i18n

import "github.com/qizhanchan/qui"

// Date/time patterns per language, as Go reference-time layouts.
//
// Why a hand-written table: golang.org/x/text ships a `date` package
// with CLDR tables but NO exported API — there is nothing to call. The
// realistic alternatives were to vendor a CLDR parser (large, and it
// would pull locale data into every binary) or to carry patterns for
// the languages the framework actually ships strings for. We took the
// second.
//
// Consequence, stated plainly: locales outside this table fall back to
// the `und` entry, an ISO-8601-ish form that is unambiguous but not
// idiomatic anywhere. Applications needing a locale we do not cover can
// register their own patterns with RegisterDateFormats rather than
// waiting on the framework.
//
// Month and weekday NAMES still come from Go's time package, which
// prints them in English. Medium/Long/Full styles therefore render
// English month names in every locale — see the caveat on
// RegisterDateFormats. Numeric styles (DateShort) are fully correct
// everywhere. This is the one deliberate gap in the formatting layer.
type dateFormats struct {
	// Indexed by DateStyle: Short, Medium, Long, Full.
	date     [4]string
	time     [4]string
	dateTime [4]string // "{date}" and "{time}" placeholders
}

var (
	// dtSepWestern is the common "date, time" join used by most
	// European locales at the longer styles.
	dtSepWestern = [4]string{"{date}, {time}", "{date}, {time}", "{date} at {time}", "{date} at {time}"}
	dtSepPlain   = [4]string{"{date} {time}", "{date} {time}", "{date} {time}", "{date} {time}"}
)

var dateFormatTable = map[string]dateFormats{
	"und": {
		date:     [4]string{"2006-01-02", "2006-01-02", "2006-01-02", "Monday, 2006-01-02"},
		time:     [4]string{"15:04", "15:04:05", "15:04:05", "15:04:05 MST"},
		dateTime: dtSepPlain,
	},
	"en": {
		date:     [4]string{"1/2/06", "Jan 2, 2006", "January 2, 2006", "Monday, January 2, 2006"},
		time:     [4]string{"3:04 PM", "3:04:05 PM", "3:04:05 PM MST", "3:04:05 PM MST"},
		dateTime: dtSepWestern,
	},
	"zh": {
		date:     [4]string{"2006/1/2", "2006年1月2日", "2006年1月2日", "2006年1月2日 Monday"},
		time:     [4]string{"15:04", "15:04:05", "15:04:05 MST", "15:04:05 MST"},
		dateTime: dtSepPlain,
	},
	"ja": {
		date:     [4]string{"2006/01/02", "2006年1月2日", "2006年1月2日", "2006年1月2日 Monday"},
		time:     [4]string{"15:04", "15:04:05", "15:04:05 MST", "15:04:05 MST"},
		dateTime: dtSepPlain,
	},
	"ko": {
		date:     [4]string{"06. 1. 2.", "2006년 1월 2일", "2006년 1월 2일", "2006년 1월 2일 Monday"},
		time:     [4]string{"15:04", "15:04:05", "15:04:05 MST", "15:04:05 MST"},
		dateTime: dtSepPlain,
	},
	"de": {
		date:     [4]string{"02.01.06", "02.01.2006", "2. January 2006", "Monday, 2. January 2006"},
		time:     [4]string{"15:04", "15:04:05", "15:04:05 MST", "15:04:05 MST"},
		dateTime: dtSepWestern,
	},
	"fr": {
		date:     [4]string{"02/01/2006", "2 Jan 2006", "2 January 2006", "Monday 2 January 2006"},
		time:     [4]string{"15:04", "15:04:05", "15:04:05 MST", "15:04:05 MST"},
		dateTime: dtSepWestern,
	},
	"es": {
		date:     [4]string{"2/1/06", "2 Jan 2006", "2 de January de 2006", "Monday, 2 de January de 2006"},
		time:     [4]string{"15:04", "15:04:05", "15:04:05 MST", "15:04:05 MST"},
		dateTime: dtSepWestern,
	},
	"pt": {
		date:     [4]string{"02/01/2006", "2 de Jan de 2006", "2 de January de 2006", "Monday, 2 de January de 2006"},
		time:     [4]string{"15:04", "15:04:05", "15:04:05 MST", "15:04:05 MST"},
		dateTime: dtSepWestern,
	},
	"ru": {
		date:     [4]string{"02.01.2006", "2 Jan 2006", "2 January 2006", "Monday, 2 January 2006"},
		time:     [4]string{"15:04", "15:04:05", "15:04:05 MST", "15:04:05 MST"},
		dateTime: dtSepWestern,
	},
	"it": {
		date:     [4]string{"02/01/06", "2 Jan 2006", "2 January 2006", "Monday 2 January 2006"},
		time:     [4]string{"15:04", "15:04:05", "15:04:05 MST", "15:04:05 MST"},
		dateTime: dtSepWestern,
	},
	"ar": {
		date:     [4]string{"2/1/2006", "2 Jan 2006", "2 January 2006", "Monday، 2 January 2006"},
		time:     [4]string{"3:04 PM", "3:04:05 PM", "3:04:05 PM MST", "3:04:05 PM MST"},
		dateTime: dtSepPlain,
	},
	"he": {
		date:     [4]string{"2.1.2006", "2 Jan 2006", "2 January 2006", "Monday, 2 January 2006"},
		time:     [4]string{"15:04", "15:04:05", "15:04:05 MST", "15:04:05 MST"},
		dateTime: dtSepPlain,
	},
}

// RegisterDateFormats installs date/time patterns for a language,
// overriding or extending the builtin table. Patterns are Go reference-
// time layouts, indexed by DateStyle (Short, Medium, Long, Full).
//
// Use this to cover a locale the framework does not ship, or to get
// localized month/weekday NAMES: Go's time package always prints those
// in English, so a fully-localized long date needs the application to
// substitute names itself. For the common case, prefer the numeric
// DateShort style, which is correct in every locale in the table.
//
// Not safe to call concurrently with formatting; call at startup.
func RegisterDateFormats(lang string, date, timeFmt, dateTime [4]string) {
	dateFormatTable[lang] = dateFormats{date: date, time: timeFmt, dateTime: dateTime}
}

func dateFormatsFor(loc qui.Locale) dateFormats {
	if f, ok := dateFormatTable[loc.Language()]; ok {
		return f
	}
	return dateFormatTable["und"]
}
