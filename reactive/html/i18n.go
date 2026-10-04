package html

import (
	"fmt"
	"strconv"
)

// Localized text in the DSL.
//
// These lower onto the same data-i18n attributes the htmlcss engine
// reads, which means a language switch never goes through the
// reconciler: qui.SetDefaultLocale triggers the style engine's restyle,
// the El tree re-resolves its messages in place, and the reactive layer
// is not involved at all. Same shape as a signal update — the render
// pass is skipped entirely.
//
//	h.H1().T("app.title").Text("Inbox")
//	h.Span().T("greet", "name", user.Name)
//	h.Span().TN("cart.items", len(cart))
//	h.Input().TPlaceholder("search.hint").Attr("placeholder", "Search…")
//
// Always keep the literal (.Text / the fallback attribute) alongside the
// key. It is what renders when a translation is missing, and it keeps
// the component readable — a tree of bare keys tells a reviewer nothing
// about what the screen says.

// T marks the element's text as coming from the message catalog.
//
// Extra arguments are name/value placeholder pairs, matching {name} in
// the message:
//
//	h.Span().T("greet", "name", "Ada").Text("Hello")
//
// Values are stringified — markup attributes carry no types. Format
// numbers and dates with an i18n.Printer before passing them.
func (b *Builder) T(key string, args ...any) *Builder {
	b.Attr("data-i18n", key)
	return b.i18nArgs(args)
}

// TN is T with a plural count. The count selects a CLDR plural category
// for the active locale and is bound to {n} in the message.
//
//	h.Span().TN("cart.items", len(cart)).Text("items")
func (b *Builder) TN(key string, n int, args ...any) *Builder {
	b.Attr("data-i18n", key)
	b.Attr("data-i18n-count", strconv.Itoa(n))
	return b.i18nArgs(args)
}

// TNf is TN for a fractional count. CLDR puts 1.0 and 1 in different
// plural categories in several languages, so this is not the same as
// rounding and calling TN.
func (b *Builder) TNf(key string, n float64, args ...any) *Builder {
	b.Attr("data-i18n", key)
	b.Attr("data-i18n-count", strconv.FormatFloat(n, 'f', -1, 64))
	return b.i18nArgs(args)
}

// TPlaceholder localizes an input's placeholder. The literal
// placeholder attribute, if set, is the fallback.
func (b *Builder) TPlaceholder(key string, args ...any) *Builder {
	b.Attr("data-i18n-placeholder", key)
	return b.i18nArgs(args)
}

// TTitle localizes the hover tooltip (the `title` attribute).
func (b *Builder) TTitle(key string, args ...any) *Builder {
	b.Attr("data-i18n-title", key)
	return b.i18nArgs(args)
}

// TValue localizes a control's `value` attribute — the caption of
// <input type=submit|reset|button>.
func (b *Builder) TValue(key string, args ...any) *Builder {
	b.Attr("data-i18n-value", key)
	return b.i18nArgs(args)
}

// Lang sets the element's language, overriding the window locale for
// this subtree.
//
// Two effects: messages under it resolve in that language, and text
// shapes with that language tag — which is what selects the right Han
// glyph variants when a page mixes Chinese and Japanese.
func (b *Builder) Lang(tag string) *Builder { return b.Attr("lang", tag) }

// Dir sets the element's writing direction ("ltr", "rtl", or "auto").
func (b *Builder) Dir(dir string) *Builder { return b.Attr("dir", dir) }

// i18nArgs writes name/value pairs as data-i18n-arg-* attributes. An
// odd trailing element or a non-string name is skipped rather than
// panicking — a typo in a component should not take down the frame.
func (b *Builder) i18nArgs(args []any) *Builder {
	for i := 0; i+1 < len(args); i += 2 {
		name, ok := args[i].(string)
		if !ok || name == "" {
			continue
		}
		b.Attr("data-i18n-arg-"+name, stringifyArg(args[i+1]))
	}
	return b
}

func stringifyArg(v any) string {
	switch t := v.(type) {
	case string:
		return t
	case int:
		return strconv.Itoa(t)
	case int64:
		return strconv.FormatInt(t, 10)
	case float64:
		if t == float64(int64(t)) {
			return strconv.FormatInt(int64(t), 10)
		}
		return strconv.FormatFloat(t, 'g', -1, 64)
	case bool:
		return strconv.FormatBool(t)
	default:
		return fmt.Sprint(v)
	}
}
