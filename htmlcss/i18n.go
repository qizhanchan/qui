package htmlcss

import (
	"strconv"
	"strings"

	"github.com/qizhanchan/qui"
)

// Localization attributes on the live element tree.
//
// Markup declares WHICH message an element shows; the engine resolves it
// during restyle, so a language switch flows through the same coalesced
// pass as a class change:
//
//	<h1 data-i18n="app.title">Inbox</h1>
//	<input data-i18n-placeholder="search.hint" placeholder="Search…">
//	<button data-i18n="qui.save" title="Save" data-i18n-title="qui.save">
//
// The literal content stays in the markup. It is the fallback when the
// catalog has no entry, and it keeps the HTML readable — a source file
// full of bare keys is unreviewable.
//
// Counts use the plural form:
//
//	<span data-i18n="cart.items" data-i18n-count="3">3 items</span>
//
// `lang` and `dir` are read here too, so an element can carry its own
// language (which drives locale-sensitive shaping) and direction
// independent of the window's.

const (
	attrI18n            = "data-i18n"
	attrI18nCount       = "data-i18n-count"
	attrI18nPlaceholder = "data-i18n-placeholder"
	attrI18nTitle       = "data-i18n-title"
	attrI18nValue       = "data-i18n-value"
	attrI18nPrefix      = "data-i18n-arg-"
)

// i18nArgs collects data-i18n-arg-* attributes into a placeholder map.
//
//	<span data-i18n="greet" data-i18n-arg-name="Ada">
//
// Values are plain strings — markup has no types, and a message that
// needs a formatted number should be given the already-formatted string
// (i18n.Printer.Number) by the code that sets the attribute.
func (e *El) i18nArgs() map[string]any {
	var out map[string]any
	// Direct iteration, not attrSnapshot: this runs inside the restyle
	// pass on the main goroutine, where nothing writes the map, and a
	// per-element map copy on every restyle would be pure waste.
	for k, v := range e.attrs {
		if !strings.HasPrefix(k, attrI18nPrefix) {
			continue
		}
		name := strings.TrimPrefix(k, attrI18nPrefix)
		if name == "" {
			continue
		}
		if out == nil {
			out = make(map[string]any, 2)
		}
		out[name] = v
	}
	if cnt, ok := e.readAttr(attrI18nCount); ok {
		if out == nil {
			out = make(map[string]any, 1)
		}
		out["n"] = cnt
	}
	return out
}

// i18nLocale returns the locale this element resolves messages in: the
// nearest `lang` attribute on itself or an ancestor, else "" meaning
// "the active locale".
//
// Walking ancestors rather than inheriting through ComputedStyle keeps
// `lang` out of the cascade, which is right — it is a document property,
// not a presentational one, and CSS cannot set it.
func (e *El) i18nLocale() qui.Locale {
	for cur := e; cur != nil; cur = cur.elParent {
		if v, ok := cur.readAttr("lang"); ok {
			if v = strings.TrimSpace(v); v != "" {
				return qui.Locale(v)
			}
		}
	}
	return ""
}

// resolveI18nText returns the text this element should display, given
// its data-i18n attributes and its current literal content. Returns
// (text, true) only when a key is present.
func (e *El) resolveI18nText() (string, bool) {
	key, ok := e.readAttr(attrI18n)
	if !ok || key == "" {
		return "", false
	}
	loc := e.i18nLocale()
	args := e.i18nArgs()
	if cnt, has := e.readAttr(attrI18nCount); has {
		if n, err := parseCount(cnt); err == nil {
			return qui.TranslatePlural(loc, key, n, args), true
		}
	}
	return qui.TranslateOr(loc, key, e.text, args), true
}

// resolveI18nAttr resolves one keyed attribute (placeholder, title,
// value), falling back to the literal attribute already present.
func (e *El) resolveI18nAttr(keyAttr, valueAttr string) (string, bool) {
	key, ok := e.readAttr(keyAttr)
	if !ok || key == "" {
		return "", false
	}
	fallback, _ := e.readAttr(valueAttr)
	return qui.TranslateOr(e.i18nLocale(), key, fallback, e.i18nArgs()), true
}

// applyI18n resolves every localization attribute on this element and
// writes the results where the rest of the apply pass will pick them up.
// Called at the top of applyComputed, before any branch reads e.text or
// the attribute map.
//
// It mutates e.text rather than caching a separate field so every
// downstream consumer — inline folding, the backing Label, table cells,
// AccessibleName — sees the translated string without knowing i18n
// exists.
func (e *El) applyI18n() {
	if text, ok := e.resolveI18nText(); ok && text != e.text {
		e.text = text
		if e.node != nil && e.node.Type == TextNode {
			e.node.Text = text
		}
	}
	if v, ok := e.resolveI18nAttr(attrI18nPlaceholder, "placeholder"); ok {
		e.storeAttr("placeholder", v)
	}
	if v, ok := e.resolveI18nAttr(attrI18nTitle, "title"); ok {
		e.storeAttr("title", v)
	}
	if v, ok := e.resolveI18nAttr(attrI18nValue, "value"); ok {
		e.storeAttr("value", v)
	}
}

// AccessibleNameKey exposes the element's message key to the selector
// grammar as [key=...], so agent scripts and CI smoke tests can target a
// control by key rather than by its translated caption.
func (e *El) AccessibleNameKey() string {
	if v, ok := e.readAttr(attrI18n); ok {
		return v
	}
	if v, ok := e.readAttr(attrI18nPlaceholder); ok {
		return v
	}
	return ""
}

// parseCount reads the data-i18n-count attribute. Accepts integers and
// decimals; CLDR distinguishes "1 hour" from "1.0 hours" in several
// languages, so the fractional form is not rounded away.
func parseCount(s string) (float64, error) {
	return strconv.ParseFloat(strings.TrimSpace(s), 64)
}
