package html

import (
	"fmt"

	"github.com/qizhanchan/qui"
	"github.com/qizhanchan/qui/htmlcss"
	"github.com/qizhanchan/qui/reactive"
)

// Node is anything that lowers to a reactive.Element: a *Builder, a raw
// reactive.Element (a component, a Show/For node, a portal), or a fragment.
type Node interface {
	Build() reactive.Element
}

// --- child arguments ---
//
// Every tag function takes `...any`, and ONE rule covers every tag:
//
//	string        → the element's text content
//	Node          → a child element (*Builder, reactive.Element, Frag, …)
//	slice         → spliced in place ([]Node, []*Builder, []reactive.Element,
//	                []string, []any)
//	number        → formatted with fmt.Sprint and used as text
//	VectorSource  → the element's icon glyph
//	nil           → dropped
//
// "Text content" means the element's natural content, which for the four
// elements that cannot hold text is their string payload instead:
//
//	<img>       → src
//	<input>     → value (type=checkbox / type=radio: the value attribute)
//	<textarea>  → value
//	<select>    → one option per string
//
// Anything else panics at construction naming the tag, the argument index
// and the offending type: a silently ignored argument is worse than a loud
// one, because it renders an almost-right UI with no error to chase.

// apply folds a tag function's variadic arguments into the builder.
func (b *Builder) apply(args []any) *Builder {
	for i, a := range args {
		b.applyArg(i, a)
	}
	return b
}

func (b *Builder) applyArg(i int, a any) {
	if a == nil {
		return
	}
	switch v := a.(type) {
	case string:
		b.addString(v)
	case Node:
		b.addChild(v)
	case []Node:
		for _, n := range v {
			b.addChild(n)
		}
	case []*Builder:
		for _, n := range v {
			b.addChild(n)
		}
	case []reactive.Element:
		for _, n := range v {
			b.addChild(n)
		}
	case []string:
		for _, s := range v {
			b.addString(s)
		}
	case []any:
		for j, x := range v {
			b.applyArg(j, x)
		}
	case qui.VectorSource:
		b.icon = v
	case fmt.Stringer:
		b.addString(v.String())
	case int, int8, int16, int32, int64,
		uint, uint8, uint16, uint32, uint64,
		float32, float64:
		b.addString(fmt.Sprint(v))
	case bool:
		panic(fmt.Sprintf("reactive/html: <%s> argument %d is a bool — "+
			"a bare bool is never content; use .Checked(v) for a checkbox, "+
			".Disabled(v) for a control, or h.If(cond, node) for a branch", b.tag, i))
	default:
		panic(fmt.Sprintf("reactive/html: <%s> argument %d has type %T, which is "+
			"neither text nor a child node — pass a string, an h builder, a "+
			"reactive.Element, a slice of those, or a qui.VectorSource", b.tag, i, a))
	}
}

func (b *Builder) addChild(n Node) {
	if n == nil {
		return
	}
	b.children = append(b.children, n)
}

// addString routes a bare string to whatever counts as content for this tag.
func (b *Builder) addString(s string) {
	switch b.tag {
	case "select", "datalist":
		b.selectItems = append(b.selectItems, s)
	case "img":
		b.Attr("src", s)
	case "input":
		if t := b.attrs["type"]; t == "checkbox" || t == "radio" {
			b.Attr("value", s)
			return
		}
		v := s
		b.value = &v
	case "textarea":
		v := s
		b.value = &v
	default:
		b.children = append(b.children, textNode(s))
	}
}

// textNode is a bare text child. A content list that is ALL text collapses
// into the parent element's own text content — the overwhelmingly common
// `h.Span("hi")` case, which stays a single widget. Text mixed with element
// children instead lowers to anonymous `#text` segment elements, which is
// how the html-css engine represents `<p>Hello <b>world</b></p>`: the
// segments and the inline children share one InlineBox, one baseline and
// one wrap.
type textNode string

func (t textNode) Build() reactive.Element {
	s := string(t)
	return reactive.Node[*htmlcss.El]("#text", "",
		func() *htmlcss.El {
			eng := engineOf(reactive.CurrentRuntime())
			if eng == nil {
				panic("reactive/html: element created outside an html.Mount runtime")
			}
			return eng.NewTextEl(s)
		},
		func(el *htmlcss.El) reactive.Flags {
			el.SetTextContent(s)
			return reactive.FlagNone
		},
		nil,
	)
}

// splitContent separates a content list into the element's own text and its
// element children. Text-only content collapses (text, nil); anything mixed
// keeps every piece as a child, text segments included.
func splitContent(content []Node) (text string, kids []Node) {
	allText := len(content) > 0
	for _, n := range content {
		if _, ok := n.(textNode); !ok {
			allText = false
			break
		}
	}
	if allText {
		for _, n := range content {
			text += string(n.(textNode))
		}
		return text, nil
	}
	return "", content
}
