package htmlcss

import (
	"strings"
	"sync"

	"github.com/qizhanchan/qui"
)

// Extension points: custom elements backed by native widgets, custom CSS
// properties, a per-element style hook, and style-aware canvas painting.

// ElementDef defines a custom element tag. The element hosts the widget
// Create returns as its content — like <canvas> hosting a widget — so it
// takes classes, box styling, margins and flex/grid placement like any
// element and participates in selectors (:nth-child, +, ~).
type ElementDef struct {
	// Create builds the backing widget once per element.
	Create func(el *El) qui.Widget
	// Apply runs after every restyle with the element's computed style —
	// the place to map CSS (color, custom properties) onto the widget's
	// own setters. Optional.
	Apply func(el *El, w qui.Widget, cs *ComputedStyle)
}

var (
	registryMu   sync.RWMutex
	elementDefs  = map[string]ElementDef{}
	propertyDefs = map[string]PropertyDef{}
)

// RegisterElement makes tag (lower-case, conventionally with a dash:
// "x-chart") a custom element. Register at init time; a later
// registration of the same tag replaces the earlier one.
func RegisterElement(tag string, def ElementDef) {
	registryMu.Lock()
	defer registryMu.Unlock()
	elementDefs[strings.ToLower(tag)] = def
}

func lookupElement(tag string) (ElementDef, bool) {
	registryMu.RLock()
	defer registryMu.RUnlock()
	d, ok := elementDefs[tag]
	return d, ok
}

// PropertyDef describes a custom CSS property registered with
// RegisterProperty.
type PropertyDef struct {
	// Inherited properties flow from parent to child like `color`.
	Inherited bool
	// Initial is the value when nothing declares it.
	Initial string
}

// RegisterProperty declares a custom CSS property (any name, e.g.
// "chart-line-width"; it needs no `--` prefix). Its declared, inherited or
// initial value is readable with ComputedStyle.Property — typically from an
// ElementDef.Apply or El.SetOnStyle hook that feeds a native setter.
// Register at init time.
func RegisterProperty(name string, def PropertyDef) {
	registryMu.Lock()
	defer registryMu.Unlock()
	propertyDefs[strings.ToLower(name)] = def
}

// inheritRegisteredProps resolves registered properties for one element:
// an inherited property nobody declared takes the parent's value, and the
// CSS-wide keywords on any registered property resolve the way they do on
// built-in ones — `inherit` to the parent's value (or Initial at the root),
// `initial` to the registered Initial, `unset` / `revert` to inherit for
// an inherited property and Initial otherwise.
func inheritRegisteredProps(m map[string]string, parent *ComputedStyle) {
	registryMu.RLock()
	defer registryMu.RUnlock()
	parentValue := func(name string) (string, bool) {
		if parent == nil {
			return "", false
		}
		v, ok := parent.raw[name]
		return v, ok
	}
	for name, def := range propertyDefs {
		v, declared := m[name]
		if !declared {
			if def.Inherited {
				if pv, ok := parentValue(name); ok {
					m[name] = pv
				}
			}
			continue
		}
		kw := strings.ToLower(strings.TrimSpace(v))
		if kw == "unset" || kw == "revert" {
			kw = "initial"
			if def.Inherited {
				kw = "inherit"
			}
		}
		switch kw {
		case "inherit":
			if pv, ok := parentValue(name); ok {
				m[name] = pv
			} else {
				m[name] = def.Initial
			}
		case "initial":
			m[name] = def.Initial
		}
	}
}

// Property returns a property's computed value as written in CSS (after
// var() substitution): a declared value, an inherited one for a registered
// inherited property, or a registered property's Initial. Works for any
// property name, built-in ones included.
func (cs *ComputedStyle) Property(name string) string {
	if cs == nil {
		return ""
	}
	name = strings.ToLower(name)
	if v, ok := cs.raw[name]; ok {
		return v
	}
	registryMu.RLock()
	defer registryMu.RUnlock()
	return propertyDefs[name].Initial
}

// Var returns a custom property's value (`--accent`, or "accent").
func (cs *ComputedStyle) Var(name string) string {
	if cs == nil {
		return ""
	}
	if !strings.HasPrefix(name, "--") {
		name = "--" + name
	}
	return cs.customProps[name]
}

// ColorVar parses a custom property as a color (ok false when unset or
// not a color).
func (cs *ComputedStyle) ColorVar(name string) (qui.Color, bool) {
	v := cs.Var(name)
	if v == "" {
		return qui.Color{}, false
	}
	return parseColor(v)
}

// SetUserData attaches an arbitrary value to the element under key (nil
// removes it) — for host layers and apps that need per-element state
// without a side map.
func (e *El) SetUserData(key string, v any) {
	if v == nil {
		delete(e.userData, key)
		return
	}
	if e.userData == nil {
		e.userData = map[string]any{}
	}
	e.userData[key] = v
}

// UserData returns the value stored by SetUserData.
func (e *El) UserData(key string) any { return e.userData[key] }

// ComputedStyle returns the element's last computed style (nil before its
// first restyle).
func (e *El) ComputedStyle() *ComputedStyle { return e.lastCS }

// SetOnStyle registers fn to run after every restyle of the element with
// its computed style — map CSS onto native setters, read a --var.
func (e *El) SetOnStyle(fn func(cs *ComputedStyle)) { e.onStyle = fn }

// SetOnLayout installs fn to run after every layout of the element, once its
// own Bounds() and those of all its descendants — including out-of-flow
// (position:absolute) ones laid out inside it — are final. It is the hook
// for decisions that depend on measured geometry (how many toolbar groups
// fit). fn runs inside the layout pass, so it must not mutate the tree
// synchronously: defer any change with Window().AfterLayout, which lays out
// again before the frame paints. nil removes it.
func (e *El) SetOnLayout(fn func(*El)) { e.onLayout = fn }

func (e *El) runStyleHooks(cs *ComputedStyle) {
	if cs == nil {
		return
	}
	e.applyPopupColors()
	e.applyScrollbarColors(cs)
	if e.engine != nil && e.engine.root == e {
		e.engine.applyTooltipStyle()
	}
	if def, ok := lookupElement(e.tag); ok && def.Apply != nil && e.canvasUser != nil {
		def.Apply(e, e.canvasUser, cs)
	}
	if e.onStyle != nil {
		e.onStyle(cs)
	}
}

// SetHostedWidget makes any element host w as its content, the way
// <canvas> does with SetCanvas: the element keeps its tag, classes and box
// styling, sizes from CSS width/height (or w's own measure), and w paints
// and handles events inside it. reactive/html's Leaf uses it.
func (e *El) SetHostedWidget(w qui.Widget) {
	e.hostsWidget = w != nil
	e.canvasUser = w
	e.canvasDraw = nil
	e.markDirty()
}

// HostedWidget returns the widget set by SetHostedWidget / SetCanvas or a
// custom element's backing widget.
func (e *El) HostedWidget() qui.Widget { return e.canvasUser }

// CanvasContext is what a style-aware canvas paint callback receives.
type CanvasContext struct {
	// Bounds is the canvas region.
	Bounds qui.Rect
	// Style is the element's computed style: Color, FontSize, Var(...).
	Style *ComputedStyle
	// Color and Font are the element's text color and font, ready to use.
	Color qui.Color
	Font  qui.Font
}

// SetCanvasPaint is SetCanvasDraw with the element's computed style handed
// to the callback, so drawing follows the stylesheet (and a dark theme):
// stroke in ctx.Color, read ctx.Style.Var("series-1").
func (e *El) SetCanvasPaint(fn func(cv qui.Canvas, ctx CanvasContext)) {
	if fn == nil {
		e.SetCanvasDraw(nil)
		return
	}
	e.SetCanvasDraw(func(cv qui.Canvas, b qui.Rect) {
		ctx := CanvasContext{Bounds: b, Style: e.lastCS}
		if e.lastCS != nil {
			ctx.Color = e.lastCS.Color
			ctx.Font = fontFrom(e.lastCS)
		}
		fn(cv, ctx)
	})
}

// hostFillLayout lays a hosted widget over the element's whole content box
// and measures the element by the widget — a replaced element at 100%.
type hostFillLayout struct{}

func (hostFillLayout) Apply(children []qui.Widget, b qui.Rect) {
	for _, c := range children {
		c.Layout(b)
	}
}

func (hostFillLayout) Measure(children []qui.Widget, avail qui.Size) qui.Size {
	var out qui.Size
	for _, c := range children {
		s := qui.MeasureConstrained(c, avail)
		out.W = max(out.W, s.W)
		out.H = max(out.H, s.H)
	}
	return out
}

// adoptHostedFlex lets a hosted widget that sizes itself with flex grow
// (SetFlex on the widget, the native-tree idiom) keep doing so through its
// host element, unless the stylesheet sets the element's flex.
func (e *El) adoptHostedFlex(cs *ComputedStyle) {
	w := e.canvasUser
	if w == nil {
		return
	}
	for _, p := range []string{"flex", "flex-grow"} {
		if _, ok := cs.raw[p]; ok {
			return
		}
	}
	grow := qui.FlexItemOf(w).Grow
	if st := w.Style(); st != nil && st.Grow > grow {
		grow = st.Grow
	}
	if grow > 0 {
		e.Style().Grow = grow
	}
}

// cssWideKeyword reports the CSS-wide keyword a value is, or "".
func cssWideKeyword(v string) string {
	switch kw := strings.ToLower(strings.TrimSpace(v)); kw {
	case "inherit", "initial", "unset", "revert":
		return kw
	}
	return ""
}

// inheritedBuiltins are the built-in properties that inherit by default.
var inheritedBuiltins = map[string]bool{
	"color": true, "font-size": true, "font-weight": true, "font-style": true,
	"font-family": true, "line-height": true, "text-align": true,
	"letter-spacing": true, "word-spacing": true, "text-indent": true,
	"white-space": true, "text-transform": true, "visibility": true,
	"cursor": true, "list-style-type": true, "accent-color": true,
	"direction": true, "user-select": true,
}

// builtinInitial is the initial value of the built-in properties whose
// initial value differs from what interpret assumes when the property is
// simply absent (an inherited property would otherwise keep the parent's
// value; display would fall back to the tag's UA default).
var builtinInitial = map[string]string{
	"color": "#000000", "font-size": "16px", "font-weight": "400",
	"font-style": "normal", "line-height": "normal", "text-align": "start",
	"letter-spacing": "normal", "word-spacing": "normal", "text-indent": "0",
	"white-space": "normal", "text-transform": "none", "visibility": "visible",
	"list-style-type": "disc", "display": "inline",
}

// resolveCSSWideKeywords applies inherit / initial / unset / revert on
// built-in properties (registered ones are handled by
// inheritRegisteredProps): inherit takes the parent's value, initial the
// property's initial value, and unset / revert inherit an inherited
// property and reset any other (revert approximated as unset). A keyword
// mixed with other tokens (`border-top: 3px solid inherit`) makes the
// declaration invalid, so it is dropped — the browser behavior.
func resolveCSSWideKeywords(m map[string]string, parent *ComputedStyle) {
	for prop, v := range m {
		if strings.HasPrefix(prop, "--") {
			continue
		}
		kw := cssWideKeyword(v)
		if kw == "" {
			if fields := strings.Fields(v); len(fields) > 1 {
				for _, f := range fields {
					if cssWideKeyword(f) != "" {
						delete(m, prop)
						break
					}
				}
			}
			continue
		}
		if kw == "unset" || kw == "revert" {
			kw = "initial"
			if inheritedBuiltins[prop] {
				kw = "inherit"
			}
		}
		switch kw {
		case "inherit":
			if parent != nil {
				if pv, ok := parent.raw[prop]; ok && cssWideKeyword(pv) == "" {
					m[prop] = pv
					continue
				}
			}
			if inheritedBuiltins[prop] {
				delete(m, prop) // the inherited copy already holds the parent's value
			} else if iv, ok := builtinInitial[prop]; ok {
				m[prop] = iv
			} else {
				delete(m, prop)
			}
		case "initial":
			if iv, ok := builtinInitial[prop]; ok {
				m[prop] = iv
			} else {
				delete(m, prop) // absent is the initial value
			}
		}
	}
}
