package i18n

import (
	"strings"
	"sync"
	"time"

	"github.com/qizhanchan/qui"
	"golang.org/x/text/collate"
	"golang.org/x/text/currency"
	"golang.org/x/text/language"
	"golang.org/x/text/message"
	"golang.org/x/text/number"
)

// Printer formats values for one locale. Get one from In(locale) or
// ForWindow(window).
//
// Printers are safe for concurrent use and cached per locale, so
// obtaining one on the Draw path is a map lookup rather than a
// table load.
type Printer struct {
	loc qui.Locale
	tag language.Tag
	p   *message.Printer

	collatorOnce sync.Once
	collator     *collate.Collator
}

var (
	printerMu    sync.RWMutex
	printerCache = map[qui.Locale]*Printer{}
)

func newPrinter(loc qui.Locale) *Printer {
	loc = canonical(loc)
	if loc == "" {
		loc = qui.DefaultLocale()
	}
	printerMu.RLock()
	p, ok := printerCache[loc]
	printerMu.RUnlock()
	if ok {
		return p
	}
	tag := parseTag(loc)
	p = &Printer{loc: loc, tag: tag, p: message.NewPrinter(tag)}
	printerMu.Lock()
	if existing, ok := printerCache[loc]; ok {
		p = existing // lost the race; keep one Printer per locale
	} else {
		printerCache[loc] = p
	}
	printerMu.Unlock()
	return p
}

// Locale returns the locale this printer formats for.
func (p *Printer) Locale() qui.Locale { return p.loc }

// Direction returns the locale's writing direction.
func (p *Printer) Direction() qui.TextDirection { return p.loc.Direction() }

// T translates a key in this printer's locale.
func (p *Printer) T(key string, args ...any) string {
	return qui.Translate(p.loc, key, argsFromPairs(args))
}

// TN translates a key with a plural count in this printer's locale.
func (p *Printer) TN(key string, n int, args ...any) string {
	return qui.TranslatePlural(p.loc, key, float64(n), argsFromPairs(args))
}

// Number formats an integer or float with locale-appropriate grouping
// and decimal separators: 1234567.5 renders "1,234,567.5" in en,
// "1.234.567,5" in de, "1 234 567,5" in fr.
//
// Pass number options to control precision, e.g.
// number.MaxFractionDigits(2).
func (p *Printer) Number(v any, opts ...number.Option) string {
	return p.p.Sprint(number.Decimal(v, opts...))
}

// Percent formats a ratio as a percentage: 0.075 renders "7.5%" in en
// and "٧٫٥٪" in ar. Pass a ratio, not an already-multiplied value.
func (p *Printer) Percent(v any, opts ...number.Option) string {
	return p.p.Sprint(number.Percent(v, opts...))
}

// Currency formats an amount in the given ISO 4217 code ("USD", "EUR",
// "CNY"), using the locale's symbol and placement: 1234.5 in USD is
// "$1,234.50" in en and "1.234,50 $" in de.
//
// An unknown code degrades to "<code> <number>" rather than failing —
// a formatting call on the Draw path must always return something.
func (p *Printer) Currency(v any, code string) string {
	unit, err := currency.ParseISO(strings.ToUpper(strings.TrimSpace(code)))
	if err != nil {
		return strings.ToUpper(code) + " " + p.Number(v)
	}
	return p.p.Sprint(currency.Symbol(unit.Amount(v)))
}

// Collator returns a locale-aware string collator, cached per printer.
//
// Use it wherever the UI sorts user-visible strings — TableView column
// sorting, Select option ordering, file lists. Byte-order sorting is
// wrong in most languages: it puts "Ä" after "Z" in German, ignores
// pinyin order in Chinese, and mis-sorts every accented Latin script.
//
//	c := i18n.In(win.Locale()).Collator()
//	sort.Slice(rows, func(i, j int) bool {
//	    return c.CompareString(rows[i].Name, rows[j].Name) < 0
//	})
func (p *Printer) Collator() *collate.Collator {
	p.collatorOnce.Do(func() { p.collator = collate.New(p.tag) })
	return p.collator
}

// DateStyle selects the length of a formatted date or time.
type DateStyle int

const (
	// DateShort is the most compact numeric form: "1/2/06", "2006/1/2".
	DateShort DateStyle = iota
	// DateMedium abbreviates the month name: "Jan 2, 2006".
	DateMedium
	// DateLong spells the month out: "January 2, 2006".
	DateLong
	// DateFull adds the weekday: "Monday, January 2, 2006".
	DateFull
)

// Date formats the date part of t.
func (p *Printer) Date(t time.Time, style DateStyle) string {
	f := dateFormatsFor(p.loc)
	return t.Format(f.date[clampStyle(style)])
}

// Time formats the time part of t.
func (p *Printer) Time(t time.Time, style DateStyle) string {
	f := dateFormatsFor(p.loc)
	return t.Format(f.time[clampStyle(style)])
}

// DateTime formats both parts, joined the way the locale joins them.
func (p *Printer) DateTime(t time.Time, style DateStyle) string {
	f := dateFormatsFor(p.loc)
	s := clampStyle(style)
	return strings.ReplaceAll(
		strings.ReplaceAll(f.dateTime[s], "{date}", t.Format(f.date[s])),
		"{time}", t.Format(f.time[s]))
}

func clampStyle(s DateStyle) int {
	if s < DateShort || s > DateFull {
		return int(DateMedium)
	}
	return int(s)
}

// RelativeTime renders t relative to now: "3 minutes ago", "in 2 days",
// "just now". The strings come from the message catalog under the
// reserved qui.relative.* keys, so they are translated like any other
// message and pluralize per locale.
//
// Granularity walks second -> minute -> hour -> day -> month -> year,
// picking the largest unit whose count is at least 1.
func (p *Printer) RelativeTime(t, now time.Time) string {
	d := t.Sub(now)
	future := d >= 0
	if !future {
		d = -d
	}
	secs := int(d / time.Second)

	unit, n := "second", secs
	switch {
	case secs < 45:
		if secs < 10 {
			return p.T("qui.relative.now")
		}
		unit, n = "second", secs
	case secs < 90:
		unit, n = "minute", 1
	case secs < 45*60:
		unit, n = "minute", (secs+30)/60
	case secs < 90*60:
		unit, n = "hour", 1
	case secs < 22*3600:
		unit, n = "hour", (secs+1800)/3600
	case secs < 36*3600:
		unit, n = "day", 1
	case secs < 26*86400:
		unit, n = "day", (secs+43200)/86400
	case secs < 46*86400:
		unit, n = "month", 1
	case secs < 320*86400:
		unit, n = "month", (secs+15*86400)/(30*86400)
	default:
		unit, n = "year", (secs+182*86400)/(365*86400)
	}

	dir := "past"
	if future {
		dir = "future"
	}
	return p.TN("qui.relative."+dir+"."+unit, n)
}

// ListStyle selects how List joins its items.
type ListStyle int

const (
	// ListAnd is a conjunction: "A, B, and C".
	ListAnd ListStyle = iota
	// ListOr is a disjunction: "A, B, or C".
	ListOr
	// ListUnit is a plain enumeration with no conjunction: "A, B, C".
	ListUnit
)

// List joins items the way the locale does. English uses ", " with a
// final "and"; Chinese uses "、" throughout; Arabic uses "، " with a
// final "و".
//
// Patterns live in the catalog under qui.list.*, so a translator can
// fix them without a code change.
func (p *Printer) List(items []string, style ListStyle) string {
	switch len(items) {
	case 0:
		return ""
	case 1:
		return items[0]
	}
	suffix := map[ListStyle]string{ListAnd: "and", ListOr: "or", ListUnit: "unit"}[style]
	sep := p.T("qui.list." + suffix + ".sep")
	last := p.T("qui.list." + suffix + ".last")
	if len(items) == 2 {
		two := p.T("qui.list." + suffix + ".two")
		return items[0] + two + items[1]
	}
	return strings.Join(items[:len(items)-1], sep) + last + items[len(items)-1]
}
