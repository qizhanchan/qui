// Package i18n is qui's message catalog and locale-sensitive formatting
// layer.
//
// It plugs into the engine through the root package's Translator seam:
// i18n imports qui, qui never imports i18n, and every other package
// (widgets, htmlcss, reactive) reaches translations through
// qui.Translate rather than importing this one. That keeps the import
// graph flat and preserves "reactive imports ONLY root".
//
// Typical wiring, once at startup:
//
//	//go:embed locales/*.json
//	var locales embed.FS
//
//	func main() {
//	    i18n.Load(locales, "locales")
//	    i18n.Install()
//	    qui.SetDefaultLocale("zh-Hans")   // or leave unset to follow the OS
//	}
//
// After that, i18n.T("file.save") returns the translated string, and
// qui.SetDefaultLocale at any later point re-lays-out every window.
package i18n

import (
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"golang.org/x/text/feature/plural"
)

// Message is one catalog entry: either a single string, or a set of
// CLDR plural forms.
//
// The two shapes share a type because a catalog author should be able
// to turn a plain message into a pluralized one without the call site
// changing — T on a plural message resolves the "other" form, and TN on
// a plain message ignores the count.
type Message struct {
	// Text is the message for a non-plural entry.
	Text string
	// Forms maps CLDR plural category names ("zero", "one", "two",
	// "few", "many", "other") to their message. Nil for plain entries.
	Forms map[string]string
}

// IsPlural reports whether the message carries plural forms.
func (m Message) IsPlural() bool { return len(m.Forms) > 0 }

// UnmarshalJSON accepts either shape:
//
//	"file.save": "Save"
//	"list.count": { "one": "{n} item", "other": "{n} items" }
func (m *Message) UnmarshalJSON(b []byte) error {
	trimmed := strings.TrimSpace(string(b))
	if strings.HasPrefix(trimmed, `"`) {
		var s string
		if err := json.Unmarshal(b, &s); err != nil {
			return err
		}
		m.Text, m.Forms = s, nil
		return nil
	}
	var forms map[string]string
	if err := json.Unmarshal(b, &forms); err != nil {
		return fmt.Errorf("message must be a string or a plural-form object: %w", err)
	}
	for cat := range forms {
		if !validPluralCategory(cat) {
			return fmt.Errorf("unknown plural category %q (want zero/one/two/few/many/other)", cat)
		}
	}
	if _, ok := forms["other"]; !ok {
		return fmt.Errorf(`plural message is missing the required "other" form`)
	}
	m.Forms, m.Text = forms, forms["other"]
	return nil
}

// MarshalJSON writes back the same two shapes, so a catalog round-trips
// through the extract tool without churning diffs.
func (m Message) MarshalJSON() ([]byte, error) {
	if !m.IsPlural() {
		return json.Marshal(m.Text)
	}
	// Emit in CLDR category order, not map order, so regenerated
	// catalogs produce stable diffs.
	ordered := make([][2]string, 0, len(m.Forms))
	for _, cat := range pluralCategoryOrder {
		if v, ok := m.Forms[cat]; ok {
			ordered = append(ordered, [2]string{cat, v})
		}
	}
	var b strings.Builder
	b.WriteByte('{')
	for i, kv := range ordered {
		if i > 0 {
			b.WriteByte(',')
		}
		k, _ := json.Marshal(kv[0])
		v, _ := json.Marshal(kv[1])
		b.Write(k)
		b.WriteByte(':')
		b.Write(v)
	}
	b.WriteByte('}')
	return []byte(b.String()), nil
}

var pluralCategoryOrder = []string{"zero", "one", "two", "few", "many", "other"}

func validPluralCategory(s string) bool {
	for _, c := range pluralCategoryOrder {
		if c == s {
			return true
		}
	}
	return false
}

// formName maps x/text's Form enum back to the CLDR category name used
// as a JSON key.
func formName(f plural.Form) string {
	switch f {
	case plural.Zero:
		return "zero"
	case plural.One:
		return "one"
	case plural.Two:
		return "two"
	case plural.Few:
		return "few"
	case plural.Many:
		return "many"
	default:
		return "other"
	}
}

// interpolate substitutes {name} placeholders from args.
//
// Rules, chosen to be forgiving in a UI context — a formatting slip
// should degrade to slightly wrong text, never to a panic or a blank
// label:
//
//   - {{ and }} are literal braces.
//   - {name} with no matching arg is left verbatim, so the gap is
//     visible in the running UI instead of silently becoming "".
//   - An unterminated { is left verbatim.
//   - Values are rendered with %v, except float64 that is integral,
//     which prints without a trailing ".0" — counts are the overwhelming
//     use and "1.0 item" reads as a bug.
func interpolate(msg string, args map[string]any) string {
	if msg == "" || !strings.ContainsAny(msg, "{}") {
		return msg
	}
	var b strings.Builder
	b.Grow(len(msg))
	for i := 0; i < len(msg); {
		c := msg[i]
		if c == '{' && i+1 < len(msg) && msg[i+1] == '{' {
			b.WriteByte('{')
			i += 2
			continue
		}
		if c == '}' && i+1 < len(msg) && msg[i+1] == '}' {
			b.WriteByte('}')
			i += 2
			continue
		}
		if c != '{' {
			b.WriteByte(c)
			i++
			continue
		}
		end := strings.IndexByte(msg[i+1:], '}')
		if end < 0 {
			b.WriteString(msg[i:])
			break
		}
		name := msg[i+1 : i+1+end]
		v, ok := args[name]
		if !ok {
			// Leave the placeholder in place — a visible {name} in the
			// UI is a bug report; an empty string is a mystery.
			b.WriteString(msg[i : i+2+end])
		} else {
			b.WriteString(renderArg(v))
		}
		i += end + 2
	}
	return b.String()
}

func renderArg(v any) string {
	switch t := v.(type) {
	case string:
		return t
	case float64:
		if t == float64(int64(t)) {
			return strconv.FormatInt(int64(t), 10)
		}
		return strconv.FormatFloat(t, 'g', -1, 64)
	case float32:
		if float64(t) == float64(int64(t)) {
			return strconv.FormatInt(int64(t), 10)
		}
		return strconv.FormatFloat(float64(t), 'g', -1, 32)
	case int:
		return strconv.Itoa(t)
	case int64:
		return strconv.FormatInt(t, 10)
	default:
		return fmt.Sprintf("%v", v)
	}
}

// argsFromPairs turns the variadic ("name", value, ...) form used by T
// and TN into a map. An odd trailing element is ignored rather than
// panicking: a mis-typed call should not take down a UI thread.
//
// A single map[string]any argument passes through unchanged, so both
// spellings work:
//
//	T("greet", "name", user)
//	T("greet", map[string]any{"name": user})
func argsFromPairs(pairs []any) map[string]any {
	if len(pairs) == 0 {
		return nil
	}
	if len(pairs) == 1 {
		if m, ok := pairs[0].(map[string]any); ok {
			return m
		}
	}
	out := make(map[string]any, len(pairs)/2)
	for i := 0; i+1 < len(pairs); i += 2 {
		key, ok := pairs[i].(string)
		if !ok {
			continue
		}
		out[key] = pairs[i+1]
	}
	return out
}

// sortedKeys is a small helper the extract/lint tooling and tests share.
func sortedKeys[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
