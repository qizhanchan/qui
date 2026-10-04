package html

import (
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/qizhanchan/qui"
	"github.com/qizhanchan/qui/htmlcss"
	"github.com/qizhanchan/qui/reactive"
)

// Templates: components written as real HTML.
//
// A template is markup with holes, compiled once and filled per render:
//
//	//go:embed todo_row.html
//	var todoRowSrc string
//	var todoRow = h.MustParse(todoRowSrc)
//
//	func TodoRow(t todo, onDelete func()) h.Node {
//		return todoRow.Bind(h.Scope{
//			"text": t.text, "urgent": t.urgent, "onDelete": onDelete,
//		})
//	}
//
//	<div class="todo" :key="id">
//	  <span class="todo-text">{text}</span>
//	  <span class="tag" :if="urgent">urgent</span>
//	  <button class="del" @click="onDelete">✕</button>
//	</div>
//
// The deliberate constraint is that THERE IS NO EXPRESSION LANGUAGE. A
// hole names one key in a flat Scope, and that is all a template can do:
//
//	{name}              text, in content or inside an attribute value
//	:if="name"          include the element while name is true
//	:key="name"         reconciliation key
//	:class="name"       class list (REPLACES the static class attribute)
//	@click="name"       an event handler, and the other @events below
//	<slot name="rows">  a subtree the caller built in Go
//
// Loops, conditions with any structure to them, derived values and
// formatting stay in Go, where the compiler checks them, and reach the
// template through the Scope or through a <slot>. That is the trade: a
// template is legible markup precisely because it cannot compute.
//
// A Scope value that is a signal binds instead of interpolating, so the
// update skips the render pass — `{name}` with a *reactive.Signal[string]
// becomes BindText, `:class` with one becomes BindClass, and `:if` with a
// *reactive.Signal[bool] becomes Show.

// Scope is the flat set of values a template binds against, keyed by the
// names its holes use.
type Scope map[string]any

// Template is compiled markup. Parse it once (a package-level var) and
// Bind it per render; parsing is the expensive half and binding is about
// as cheap as writing the equivalent builders by hand.
type Template struct {
	src   string
	lines []string
	roots []*tnode
	names []string
}

// MustParse is Parse for a package-level var: it panics on a malformed
// template, which is a programming error the author sees on first run.
func MustParse(src string) *Template {
	t, err := Parse(src)
	if err != nil {
		panic(err.Error())
	}
	return t
}

// Parse compiles an HTML fragment into a Template. The markup is parsed
// by the same spec-compliant parser the html-css engine uses, so implied
// tags, optional closing tags, entities and malformed markup behave as
// they do in a browser.
func Parse(src string) (*Template, error) {
	t := &Template{src: src, lines: strings.Split(src, "\n")}
	dom := htmlcss.ParseHTML(src)
	if len(collectTemplates(dom)) > 0 {
		return nil, fmt.Errorf("reactive/html: this source holds <template> " +
			"fragments — compile it with MustParseSet and bind by id")
	}
	body := findTag(dom, "body")
	if body == nil {
		body = dom
	}
	if err := t.compileRoots(body.Children); err != nil {
		return nil, err
	}
	if len(t.roots) == 0 {
		return nil, fmt.Errorf("reactive/html: template has no elements")
	}
	return t, nil
}

// Set is several named templates compiled from ONE source, each wrapped in
// a `<template id="…">`. A dialog is a handful of small fragments — a
// shell, a row, a form line — and keeping them in one file next to each
// other beats a file per fragment:
//
//	//go:embed validation_dialog.html
//	var validationHTML string
//	var validationTpl = h.MustParseSet(validationHTML)
//
//	validationTpl.Bind("rule", h.Scope{"summary": …, "del": …})
//
// Line numbers in errors refer to the whole file, so they point at the
// fragment the author is looking at.
type Set map[string]*Template

// MustParseSet is ParseSet for a package-level var.
func MustParseSet(src string) Set {
	s, err := ParseSet(src)
	if err != nil {
		panic(err.Error())
	}
	return s
}

// ParseSet compiles every `<template id="…">` in src into its own
// Template, keyed by id.
func ParseSet(src string) (Set, error) {
	dom := htmlcss.ParseHTML(src)
	frags := collectTemplates(dom)
	if len(frags) == 0 {
		return nil, fmt.Errorf("reactive/html: no <template id=\"…\"> fragments — " +
			"wrap each one, or compile a single fragment with MustParse")
	}
	lines := strings.Split(src, "\n")
	set := Set{}
	for _, f := range frags {
		id, _ := f.Attr("id")
		if id == "" {
			t := &Template{src: src, lines: lines}
			return nil, t.errAt("template", "<template",
				`every fragment needs an id: <template id="row">…</template>`)
		}
		if _, dup := set[id]; dup {
			t := &Template{src: src, lines: lines}
			return nil, t.errAt("template", `id="`+id+`"`,
				"duplicate template id "+strconv.Quote(id))
		}
		t := &Template{src: src, lines: lines}
		if err := t.compileRoots(f.Children); err != nil {
			return nil, err
		}
		if len(t.roots) == 0 {
			return nil, t.errAt("template", `id="`+id+`"`,
				"template "+strconv.Quote(id)+" is empty")
		}
		set[id] = t
	}
	return set, nil
}

// Bind fills the named fragment. An unknown id names the ids the set does
// hold, which is the whole diagnosis for a typo.
func (s Set) Bind(name string, scope Scope) Node {
	t, ok := s[name]
	if !ok {
		panic(fmt.Sprintf("reactive/html: no template %q in this set (it has: %s)",
			name, strings.Join(s.Names(), ", ")))
	}
	return t.Bind(scope)
}

// Names lists the fragment ids, sorted.
func (s Set) Names() []string {
	out := make([]string, 0, len(s))
	for k := range s {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// compileRoots compiles a fragment's top-level children.
func (t *Template) compileRoots(kids []*htmlcss.Node) error {
	names := map[string]bool{}
	for _, c := range kids {
		// Indentation and comments around the root elements are not
		// content — there is no inline context at the top level for a
		// word gap to belong to.
		if c.Type == htmlcss.TextNode && htmlcss.CollapseText(c.Text) == "" {
			continue
		}
		n, err := t.compile(c, "", names)
		if err != nil {
			return err
		}
		if n != nil {
			t.roots = append(t.roots, n)
		}
	}
	for k := range names {
		t.names = append(t.names, k)
	}
	sort.Strings(t.names)
	return nil
}

// collectTemplates finds every <template> element in the document. The
// parser puts them in <head> when they lead the file, so this walks the
// whole tree rather than just the body.
func collectTemplates(n *htmlcss.Node) []*htmlcss.Node {
	var out []*htmlcss.Node
	var walk func(*htmlcss.Node)
	walk = func(x *htmlcss.Node) {
		if x.Type == htmlcss.ElementNode && x.Tag == "template" {
			out = append(out, x)
			return // fragments do not nest
		}
		for _, c := range x.Children {
			walk(c)
		}
	}
	walk(n)
	return out
}

// Names lists every Scope key the template refers to, sorted. A test can
// assert a caller's Scope covers it, turning a runtime panic into a
// compile-adjacent check.
func (t *Template) Names() []string { return append([]string(nil), t.names...) }

// Bind fills the template from scope and returns the node. A missing or
// wrongly-typed value panics naming the template line and the directive,
// the same way a bad tag argument does — an almost-right UI with nothing
// to chase is the worse failure.
func (t *Template) Bind(scope Scope) Node {
	out := make([]Node, 0, len(t.roots))
	for _, r := range t.roots {
		if n := r.bind(t, scope); n != nil {
			out = append(out, n)
		}
	}
	switch len(out) {
	case 0:
		return Nothing()
	case 1:
		return out[0]
	default:
		return Frag(out)
	}
}

// --- compiled form ---

type tnode struct {
	path string // "div>ul>li[2]", for error messages

	isText bool
	parts  []part // text content, literals and holes interleaved

	tag   string
	slot  string // <slot name="…">: the Scope key holding the subtree
	attrs []tattr
	dirs  map[string]string // ":if" → scope key
	evts  map[string]string // "click" → scope key
	kids  []*tnode

	// textOnly mirrors buildStaticEl: an element with no element children
	// carries its text itself instead of hosting #text segments.
	textOnly bool
	preserve bool // <pre>: keep the author's whitespace
	options  []tOption
}

type tOption struct {
	label    string
	value    string
	disabled bool
}

type tattr struct {
	name  string
	parts []part
}

// part is one piece of an interpolated string: a literal, or a hole
// naming a Scope key.
type part struct {
	lit  string
	hole string
}

// --- compilation ---

func (t *Template) compile(n *htmlcss.Node, parentPath string, names map[string]bool) (*tnode, error) {
	if n.Type == htmlcss.TextNode {
		parts, err := t.parseParts(n.Text, parentPath, names)
		if err != nil {
			return nil, err
		}
		return &tnode{path: parentPath, isText: true, parts: parts}, nil
	}
	if n.Type != htmlcss.ElementNode || htmlcss.IsNonRenderedTag(n.Tag) {
		return nil, nil
	}
	path := n.Tag
	if parentPath != "" {
		path = parentPath + ">" + n.Tag
	}
	e := &tnode{path: path, tag: n.Tag, preserve: n.Tag == "pre"}

	if n.Tag == "slot" {
		name, _ := n.Attr("name")
		if name == "" {
			return nil, t.errAt(path, "", `<slot> needs a name: <slot name="rows"></slot>`)
		}
		if len(n.Children) > 0 {
			return nil, t.errAt(path, `<slot name="`+name+`"`,
				"<slot> must be empty and explicitly closed — write "+
					`<slot name="`+name+`"></slot>, not a self-closing tag `+
					"(HTML has no self-closing non-void elements, so the rest of "+
					"the template becomes its children)")
		}
		e.slot = name
		names[name] = true
		return e, nil
	}

	// Attributes: directives (: and @) are separated out; everything else
	// is a plain attribute whose value may interpolate.
	keys := make([]string, 0, len(n.Attrs))
	for k := range n.Attrs {
		keys = append(keys, k)
	}
	sort.Strings(keys) // deterministic errors and output
	for _, k := range keys {
		v := n.Attrs[k]
		switch {
		case strings.HasPrefix(k, ":"):
			d := k[1:]
			if !directives[d] {
				return nil, t.errAt(path, k+`="`+v+`"`,
					"unknown directive :"+d+" — supported: "+directiveList())
			}
			if v == "" {
				return nil, t.errAt(path, k, ":"+d+" needs a scope key, e.g. :"+d+`="myValue"`)
			}
			if e.dirs == nil {
				e.dirs = map[string]string{}
			}
			e.dirs[d] = v
			names[v] = true
		case strings.HasPrefix(k, "@"):
			ev := k[1:]
			if !events[ev] {
				return nil, t.errAt(path, k+`="`+v+`"`,
					"unknown event @"+ev+" — supported: "+eventList())
			}
			if v == "" {
				return nil, t.errAt(path, k, "@"+ev+" needs a scope key, e.g. @"+ev+`="onThing"`)
			}
			if e.evts == nil {
				e.evts = map[string]string{}
			}
			e.evts[ev] = v
			names[v] = true
		default:
			parts, err := t.parseParts(v, path, names)
			if err != nil {
				return nil, err
			}
			e.attrs = append(e.attrs, tattr{name: k, parts: parts})
		}
	}

	// <select> takes its options from <option> children, as in HTML.
	if n.Tag == "select" {
		for _, c := range n.Children {
			if c.Type != htmlcss.ElementNode || c.Tag != "option" {
				continue
			}
			label := htmlcss.CollapseText(textOf(c))
			value := c.AttrOr("value", label)
			_, disabled := c.Attr("disabled")
			e.options = append(e.options, tOption{label, value, disabled})
		}
		return e, nil
	}

	// Leaves: nothing below them renders.
	switch n.Tag {
	case "img", "br", "hr", "input":
		return e, nil
	}

	elemKids := 0
	for _, c := range n.Children {
		if c.Type == htmlcss.ElementNode && !htmlcss.IsNonRenderedTag(c.Tag) {
			elemKids++
		}
	}
	if elemKids == 0 {
		e.textOnly = true
		parts, err := t.parseParts(textOf(n), path, names)
		if err != nil {
			return nil, err
		}
		e.parts = parts
		return e, nil
	}

	// Mixed content. Whitespace-only text nodes survive only where the
	// element has inline content at all — between blocks they would leak
	// empty labels. Same rule as htmlcss/static.go's buildStaticEl.
	inlineish := false
	for _, c := range n.Children {
		if c.Type == htmlcss.TextNode && htmlcss.CollapseText(c.Text) != "" {
			inlineish = true
			break
		}
		if c.Type == htmlcss.ElementNode && htmlcss.IsInlineTag(c.Tag) {
			inlineish = true
			break
		}
	}
	for _, c := range n.Children {
		if c.Type == htmlcss.TextNode && !inlineish && htmlcss.CollapseText(c.Text) == "" {
			continue
		}
		k, err := t.compile(c, path, names)
		if err != nil {
			return nil, err
		}
		if k != nil {
			e.kids = append(e.kids, k)
		}
	}
	return e, nil
}

// parseParts splits "count: {n} of {total}" into literals and holes.
// "{{" is a literal "{".
func (t *Template) parseParts(s, path string, names map[string]bool) ([]part, error) {
	var out []part
	var lit strings.Builder
	for i := 0; i < len(s); {
		c := s[i]
		if c != '{' {
			lit.WriteByte(c)
			i++
			continue
		}
		if i+1 < len(s) && s[i+1] == '{' {
			lit.WriteByte('{')
			i += 2
			continue
		}
		end := strings.IndexByte(s[i:], '}')
		if end < 0 {
			return nil, t.errAt(path, s, "unclosed { in "+strconv.Quote(s)+
				` — a hole is written {name}, and a literal brace is written {{`)
		}
		name := strings.TrimSpace(s[i+1 : i+end])
		if !validName(name) {
			return nil, t.errAt(path, s, "bad hole {"+name+"} — a hole names one Scope "+
				"key (letters, digits, _), with no expressions: compute the value in Go")
		}
		if lit.Len() > 0 {
			out = append(out, part{lit: lit.String()})
			lit.Reset()
		}
		out = append(out, part{hole: name})
		names[name] = true
		i += end + 1
	}
	if lit.Len() > 0 {
		out = append(out, part{lit: lit.String()})
	}
	return out, nil
}

func validName(s string) bool {
	if s == "" {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		ok := c == '_' || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') ||
			(i > 0 && c >= '0' && c <= '9')
		if !ok {
			return false
		}
	}
	return true
}

// --- binding ---

func (n *tnode) bind(t *Template, scope Scope) Node {
	if n.isText {
		return textNode(t.interpolate(n, "text", n.parts, scope))
	}
	if n.slot != "" {
		return slotNode(t, n, scope)
	}

	// :if gates the whole element. A signal gates it WITHOUT a render
	// pass, by mounting and unmounting the subtree straight off the
	// signal (h.Show), which is why the key is the node's path.
	if key, ok := n.dirs["if"]; ok {
		switch v := t.lookup(n, "if", key, scope).(type) {
		case bool:
			if !v {
				return nil
			}
		case *reactive.Signal[bool]:
			return Show(n.path, v, func() Node { return n.bindElement(t, scope) })
		default:
			panic(t.errAt(n.path, `:if="`+key+`"`, fmt.Sprintf(
				":if wants a bool or a *reactive.Signal[bool], got %T", v)).Error())
		}
	}
	return n.bindElement(t, scope)
}

func (n *tnode) bindElement(t *Template, scope Scope) Node {
	b := tag(n.tag, nil)

	for _, a := range n.attrs {
		b.Attr(a.name, t.interpolate(n, a.name, a.parts, scope))
	}
	for _, o := range n.options {
		b.selectItems = append(b.selectItems, o.label)
		b.optionValues = append(b.optionValues, o.value)
		b.optionDisabled = append(b.optionDisabled, o.disabled)
	}
	n.applyDirectives(t, b, scope)
	n.applyEvents(t, b, scope)
	n.applyContent(t, b, scope)
	return b
}

func (n *tnode) applyContent(t *Template, b *Builder, scope Scope) {
	if len(n.kids) > 0 {
		for _, k := range n.kids {
			if k.isText {
				s := t.interpolate(k, "text", k.parts, scope)
				b.children = append(b.children, textNode(s))
				continue
			}
			if c := k.bind(t, scope); c != nil {
				b.children = append(b.children, c)
			}
		}
		return
	}
	if !n.textOnly || len(n.parts) == 0 {
		return
	}
	// A lone signal hole binds the text instead of interpolating it, so
	// the update never reaches the reconciler.
	if len(n.parts) == 1 && n.parts[0].hole != "" {
		if sig, ok := t.lookup(n, "text", n.parts[0].hole, scope).(*reactive.Signal[string]); ok {
			b.BindText(sig)
			return
		}
	}
	s := t.interpolate(n, "text", n.parts, scope)
	if !n.preserve {
		s = htmlcss.CollapseText(s)
	} else {
		s = strings.TrimPrefix(strings.TrimPrefix(s, "\r\n"), "\n")
	}
	if s != "" {
		b.addString(s)
	}
}

func (n *tnode) applyDirectives(t *Template, b *Builder, scope Scope) {
	for d, key := range n.dirs {
		if d == "if" {
			continue // handled before the element is built
		}
		v := t.lookup(n, d, key, scope)
		bad := func(want string) {
			panic(t.errAt(n.path, ":"+d+`="`+key+`"`, fmt.Sprintf(
				":%s wants %s, got %T", d, want, v)).Error())
		}
		switch d {
		case "key":
			b.Key(t.asText(n, d, key, v))
		case "text":
			if sig, ok := v.(*reactive.Signal[string]); ok {
				b.BindText(sig)
				continue
			}
			b.Text(t.asText(n, d, key, v))
		case "class":
			switch c := v.(type) {
			case string:
				b.Class(c)
			case *reactive.Signal[string]:
				b.BindClass(c)
			default:
				bad("a string or a *reactive.Signal[string]")
			}
		case "value":
			b.Value(t.asText(n, d, key, v))
		case "options":
			opts, ok := v.([]string)
			if !ok {
				bad("a []string")
			}
			b.Options(opts...)
		case "checked":
			c, ok := v.(bool)
			if !ok {
				bad("a bool")
			}
			b.Checked(c)
		case "disabled":
			c, ok := v.(bool)
			if !ok {
				bad("a bool")
			}
			b.Disabled(c)
		case "selected":
			i, ok := v.(int)
			if !ok {
				bad("an int")
			}
			b.Selected(i)
		case "draggable":
			b.Draggable(t.asText(n, d, key, v))
		case "icon":
			src, ok := v.(qui.VectorSource)
			if !ok {
				bad("a qui.VectorSource")
			}
			b.Icon(src)
		case "ref":
			fn, ok := v.(func(*htmlcss.El))
			if !ok {
				bad("a func(*htmlcss.El)")
			}
			b.Ref(fn)
		}
	}
}

func (n *tnode) applyEvents(t *Template, b *Builder, scope Scope) {
	for ev, key := range n.evts {
		v := t.lookup(n, "@"+ev, key, scope)
		bad := func(want string) {
			panic(t.errAt(n.path, "@"+ev+`="`+key+`"`, fmt.Sprintf(
				"@%s wants %s, got %T", ev, want, v)).Error())
		}
		switch ev {
		case "click":
			fn, ok := v.(func())
			if !ok {
				bad("a func()")
			}
			b.OnClick(fn)
		case "dblclick":
			fn, ok := v.(func())
			if !ok {
				bad("a func()")
			}
			b.OnDoubleClick(fn)
		case "focus":
			fn, ok := v.(func())
			if !ok {
				bad("a func()")
			}
			b.OnFocus(fn)
		case "blur":
			fn, ok := v.(func())
			if !ok {
				bad("a func()")
			}
			b.OnBlur(fn)
		case "mouseenter":
			fn, ok := v.(func())
			if !ok {
				bad("a func()")
			}
			b.OnMouseEnter(fn)
		case "mouseleave":
			fn, ok := v.(func())
			if !ok {
				bad("a func()")
			}
			b.OnMouseLeave(fn)
		case "dragend":
			fn, ok := v.(func())
			if !ok {
				bad("a func()")
			}
			b.OnDragEnd(fn)
		case "input":
			fn, ok := v.(func(string))
			if !ok {
				bad("a func(string)")
			}
			b.OnInput(fn)
		case "submit":
			fn, ok := v.(func(string))
			if !ok {
				bad("a func(string)")
			}
			b.OnSubmit(fn)
		case "commit":
			fn, ok := v.(func(string))
			if !ok {
				bad("a func(string)")
			}
			b.OnCommit(fn)
		case "drop":
			fn, ok := v.(func(string))
			if !ok {
				bad("a func(sourceKey string)")
			}
			b.OnDrop(fn)
		case "toggle":
			fn, ok := v.(func(bool))
			if !ok {
				bad("a func(bool)")
			}
			b.OnToggle(fn)
		case "select":
			fn, ok := v.(func(int, string))
			if !ok {
				bad("a func(index int, value string)")
			}
			b.OnSelect(fn)
		case "contextmenu":
			fn, ok := v.(func(x, y float32))
			if !ok {
				bad("a func(x, y float32)")
			}
			b.OnContextMenu(fn)
		case "dragover":
			fn, ok := v.(func(string, bool))
			if !ok {
				bad("a func(sourceKey string, after bool)")
			}
			b.OnDragOver(fn)
		case "keydown":
			fn, ok := v.(func(qui.KeyEvent) bool)
			if !ok {
				bad("a func(qui.KeyEvent) bool")
			}
			b.OnKeyDown(fn)
		case "keyup":
			fn, ok := v.(func(qui.KeyEvent) bool)
			if !ok {
				bad("a func(qui.KeyEvent) bool")
			}
			b.OnKeyUp(fn)
		case "wheel":
			fn, ok := v.(func(dx, dy float32) bool)
			if !ok {
				bad("a func(dx, dy float32) bool")
			}
			b.OnWheel(fn)
		}
	}
}

// slotNode splices a Go-built subtree into the template.
func slotNode(t *Template, n *tnode, scope Scope) Node {
	v, ok := scope[n.slot]
	if !ok || v == nil {
		return nil
	}
	switch s := v.(type) {
	case Node:
		return s
	case []Node:
		return Frag(s)
	case []*Builder:
		return Frag(s)
	case []reactive.Element:
		return Frag(s)
	default:
		panic(t.errAt(n.path, `<slot name="`+n.slot+`">`, fmt.Sprintf(
			"a slot wants a Node or a []Node, got %T", v)).Error())
	}
}

// --- scope access ---

func (t *Template) lookup(n *tnode, what, key string, scope Scope) any {
	v, ok := scope[key]
	if !ok {
		panic(t.errAt(n.path, needleFor(what, key), fmt.Sprintf(
			"no %q in the Scope (it has: %s)", key, scopeKeys(scope))).Error())
	}
	return v
}

func (t *Template) asText(n *tnode, what, key string, v any) string {
	switch s := v.(type) {
	case string:
		return s
	case fmt.Stringer:
		return s.String()
	case int, int8, int16, int32, int64,
		uint, uint8, uint16, uint32, uint64, float32, float64, bool:
		return fmt.Sprint(s)
	default:
		panic(t.errAt(n.path, needleFor(what, key), fmt.Sprintf(
			"%q is a %T, which has no text form", key, v)).Error())
	}
}

// interpolate renders a literal/hole sequence against the scope.
func (t *Template) interpolate(n *tnode, what string, parts []part, scope Scope) string {
	if len(parts) == 1 && parts[0].hole == "" {
		return parts[0].lit
	}
	var sb strings.Builder
	for _, p := range parts {
		if p.hole == "" {
			sb.WriteString(p.lit)
			continue
		}
		sb.WriteString(t.asText(n, what, p.hole, t.lookup(n, what, p.hole, scope)))
	}
	return sb.String()
}

func scopeKeys(scope Scope) string {
	if len(scope) == 0 {
		return "nothing"
	}
	keys := make([]string, 0, len(scope))
	for k := range scope {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return strings.Join(keys, ", ")
}

// needleFor reconstructs the source text of a directive so errAt can find
// its line.
func needleFor(what, key string) string {
	switch {
	case what == "text":
		return "{" + key + "}"
	case strings.HasPrefix(what, "@"):
		return what + `="` + key + `"`
	default:
		return ":" + what + `="` + key + `"`
	}
}

// --- errors ---

// errAt reports a template problem with the element path and, when the
// offending source text occurs exactly once, its line number — which is
// what makes a template debuggable at all, since the HTML parser itself
// carries no positions.
func (t *Template) errAt(path, needle, msg string) error {
	where := path
	if where == "" {
		where = "template"
	}
	if ln := t.lineOf(needle); ln > 0 {
		return fmt.Errorf("reactive/html: template line %d (%s): %s", ln, where, msg)
	}
	return fmt.Errorf("reactive/html: template at %s: %s", where, msg)
}

// lineOf returns the 1-based line holding needle, or 0 when it is absent
// or ambiguous — a wrong line number is worse than none.
func (t *Template) lineOf(needle string) int {
	if needle == "" || strings.Count(t.src, needle) != 1 {
		return 0
	}
	for i, ln := range t.lines {
		if strings.Contains(ln, needle) {
			return i + 1
		}
	}
	return 0
}

// --- directive tables ---

var directives = map[string]bool{
	"if": true, "key": true, "class": true, "text": true,
	"value": true, "checked": true, "selected": true, "disabled": true,
	"options": true, "draggable": true, "icon": true, "ref": true,
}

var events = map[string]bool{
	"click": true, "dblclick": true, "contextmenu": true,
	"input": true, "submit": true, "commit": true, "toggle": true, "select": true,
	"focus": true, "blur": true, "mouseenter": true, "mouseleave": true,
	"keydown": true, "keyup": true, "wheel": true,
	"drop": true, "dragover": true, "dragend": true,
}

func directiveList() string { return sortedKeys(directives, ":") }
func eventList() string     { return sortedKeys(events, "@") }

func sortedKeys(m map[string]bool, prefix string) string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, prefix+k)
	}
	sort.Strings(out)
	return strings.Join(out, " ")
}

// --- DOM helpers ---

func findTag(n *htmlcss.Node, tag string) *htmlcss.Node {
	if n == nil {
		return nil
	}
	if n.Type == htmlcss.ElementNode && n.Tag == tag {
		return n
	}
	for _, c := range n.Children {
		if f := findTag(c, tag); f != nil {
			return f
		}
	}
	return nil
}

func textOf(n *htmlcss.Node) string {
	if n.Type == htmlcss.TextNode {
		return n.Text
	}
	var sb strings.Builder
	for _, c := range n.Children {
		sb.WriteString(textOf(c))
	}
	return sb.String()
}
