package widgets

import (
	. "github.com/qizhanchan/qui"
)

// Localized text on native widgets.
//
// The problem this solves is specific to retained mode. In an immediate-
// mode UI you would write NewButton(T("qui.save")) and the string would
// be re-resolved every frame for free. Here the widget tree is built
// once and kept, so a translated string captured at construction time
// survives a language switch and the button keeps saying "Save" in a
// German UI.
//
// So text-bearing widgets carry a KEY alongside their literal text, and
// resolve it during Measure/Draw:
//
//	btn := widgets.NewButton("", nil)
//	btn.SetTextKey("qui.save")
//
// or, when the caption takes placeholders:
//
//	btn.SetTextKey("file.saveAs", "name", doc.Name)
//
// Resolution rules, shared by every widget below:
//
//   - TextKey empty  -> the literal field is used verbatim. Nothing
//     changes for apps that do not use i18n, which is why every widget
//     keeps its plain Text/Placeholder field.
//   - TextKey set    -> the resolved message REPLACES the literal. The
//     literal is still worth setting: it is what a developer reading
//     the code sees, and what renders if the catalog is missing.
//
// Resolution goes through qui.Translate, so widgets never import the
// i18n package — see the note on qui.Translator.

// messageKey is embedded by widgets that support a localized caption.
// It is deliberately tiny: two fields, no locking. Widget state is
// UI-goroutine-owned, and the resolve path runs inside Draw/Measure.
type messageKey struct {
	key  string
	args map[string]any
}

// set stores the key and its placeholder arguments, returning whether
// anything actually changed — callers use that to skip invalidation.
func (m *messageKey) set(key string, args []any) bool {
	newArgs := pairsToMap(args)
	if m.key == key && sameArgs(m.args, newArgs) {
		return false
	}
	m.key, m.args = key, newArgs
	return true
}

// resolve returns the translated message, falling back to the widget's
// literal field when no key is set OR when the key is not in the
// catalog. Called on the Draw/Measure path, so the no-key case must not
// allocate — it does not.
//
// Falling back to the literal rather than to the key matters: a widget
// built as NewButton("Save", …) plus SetTextKey("qui.save") should read
// "Save" in an app that ships no catalog, not "qui.save". Under
// QUI_I18N_STRICT=1 the gap is surfaced instead — see qui.TranslateOr.
func (m *messageKey) resolve(fallback string) string {
	if m.key == "" {
		return fallback
	}
	return TranslateOr("", m.key, fallback, m.args)
}

func pairsToMap(pairs []any) map[string]any {
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
		if k, ok := pairs[i].(string); ok {
			out[k] = pairs[i+1]
		}
	}
	return out
}

// sameArgs compares placeholder maps by value. Only comparable values
// are compared with ==; anything else counts as changed, which is the
// safe direction (an extra relayout, never a stale caption).
func sameArgs(a, b map[string]any) bool {
	if len(a) != len(b) {
		return false
	}
	for k, av := range a {
		bv, ok := b[k]
		if !ok {
			return false
		}
		if av != bv {
			return false
		}
	}
	return true
}

// -------------------------------------------------------------------
// Per-widget setters.
//
// Each is the same three lines — store, invalidate layout (a translated
// caption almost always measures differently), done. They are spelled
// out rather than generated so the godoc for each widget names the
// field it drives.

// SetTextKey makes the button's caption come from the message catalog.
// Pass "" to go back to the literal Text field.
//
//	btn.SetTextKey("qui.save")
//	btn.SetTextKey("file.saveAs", "name", doc.Name)
func (b *Button) SetTextKey(key string, args ...any) {
	if b.textKey.set(key, args) {
		b.InvalidateLayout()
	}
}

// TextKey returns the button's message key, or "".
func (b *Button) TextKey() string { return b.textKey.key }

// DisplayText is the caption the button actually renders: the resolved
// message when TextKey is set, otherwise Text. Widgets call this rather
// than reading Text directly; applications can too, e.g. to size a
// container around the translated string.
func (b *Button) DisplayText() string { return b.textKey.resolve(b.Text) }

// SetTextKey makes the label's text come from the message catalog.
// Clears any rich-text spans, like SetText.
func (l *Label) SetTextKey(key string, args ...any) {
	if l.textKey.set(key, args) {
		l.spans = nil
		l.InvalidateLayout()
	}
}

// TextKey returns the label's message key, or "".
func (l *Label) TextKey() string { return l.textKey.key }

// SetPlaceholderKey makes the input's ghost text come from the catalog.
func (t *Input) SetPlaceholderKey(key string, args ...any) {
	if t.placeholderKey.set(key, args) {
		t.InvalidateLayout()
	}
}

// PlaceholderKey returns the input's placeholder message key, or "".
func (t *Input) PlaceholderKey() string { return t.placeholderKey.key }

// DisplayPlaceholder is the ghost text the input actually renders.
func (t *Input) DisplayPlaceholder() string { return t.placeholderKey.resolve(t.Placeholder) }

// SetPlaceholderKey makes the text area's ghost text come from the
// catalog.
func (t *TextArea) SetPlaceholderKey(key string, args ...any) {
	if t.placeholderKey.set(key, args) {
		t.InvalidateLayout()
	}
}

// PlaceholderKey returns the text area's placeholder message key, or "".
func (t *TextArea) PlaceholderKey() string { return t.placeholderKey.key }

// DisplayPlaceholder is the ghost text the text area actually renders.
func (t *TextArea) DisplayPlaceholder() string { return t.placeholderKey.resolve(t.Placeholder) }

// SetLabelKey makes the checkbox's caption come from the catalog.
func (c *CheckBox) SetLabelKey(key string, args ...any) {
	if c.labelKey.set(key, args) {
		c.InvalidateLayout()
	}
}

// LabelKey returns the checkbox's message key, or "".
func (c *CheckBox) LabelKey() string { return c.labelKey.key }

// DisplayLabel is the caption the checkbox actually renders.
func (c *CheckBox) DisplayLabel() string { return c.labelKey.resolve(c.Label) }

// SetLabelKey makes the switch's caption come from the catalog.
func (s *Switch) SetLabelKey(key string, args ...any) {
	if s.labelKey.set(key, args) {
		s.InvalidateLayout()
	}
}

// LabelKey returns the switch's message key, or "".
func (s *Switch) LabelKey() string { return s.labelKey.key }

// DisplayLabel is the caption the switch actually renders.
func (s *Switch) DisplayLabel() string { return s.labelKey.resolve(s.Label) }

// SetPlaceholderKey makes the select's placeholder come from the
// catalog.
func (cb *Select) SetPlaceholderKey(key string, args ...any) {
	if cb.placeholderKey.set(key, args) {
		cb.InvalidateLayout()
	}
}

// PlaceholderKey returns the select's placeholder message key, or "".
func (cb *Select) PlaceholderKey() string { return cb.placeholderKey.key }

// DisplayPlaceholder is the placeholder the select actually renders.
func (cb *Select) DisplayPlaceholder() string { return cb.placeholderKey.resolve(cb.Placeholder) }

// SetLabelKey makes the radio button's caption come from the catalog.
func (r *RadioButton) SetLabelKey(key string, args ...any) {
	if r.labelKey.set(key, args) {
		r.InvalidateLayout()
	}
}

// LabelKey returns the radio button's message key, or "".
func (r *RadioButton) LabelKey() string { return r.labelKey.key }

// DisplayLabel is the caption the radio button actually renders.
func (r *RadioButton) DisplayLabel() string { return r.labelKey.resolve(r.Label) }

// SetLabelKey makes the input's floating label come from the catalog
// (Label stays the fallback and still decides whether a label is shown).
func (t *Input) SetLabelKey(key string, args ...any) {
	if t.labelKey.set(key, args) {
		t.InvalidateLayout()
	}
}

// LabelKey returns the input's label message key, or "".
func (t *Input) LabelKey() string { return t.labelKey.key }

// DisplayLabel is the floating label the input actually renders.
func (t *Input) DisplayLabel() string { return t.labelKey.resolve(t.Label) }

// SetLabelKey makes the select's floating label come from the catalog
// (Label stays the fallback and still decides whether a label is shown).
func (cb *Select) SetLabelKey(key string, args ...any) {
	if cb.labelKey.set(key, args) {
		cb.InvalidateLayout()
	}
}

// LabelKey returns the select's label message key, or "".
func (cb *Select) LabelKey() string { return cb.labelKey.key }

// DisplayLabel is the floating label the select actually renders.
func (cb *Select) DisplayLabel() string { return cb.labelKey.resolve(cb.Label) }

// SetTitleKey makes the fieldset's title come from the catalog.
func (g *FieldSet) SetTitleKey(key string, args ...any) { g.title.SetTextKey(key, args...) }

// TitleKey returns the fieldset's title message key, or "".
func (g *FieldSet) TitleKey() string { return g.title.TextKey() }
