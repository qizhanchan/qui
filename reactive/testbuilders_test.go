package reactive_test

// testbuilders_test.go is a test-only reimplementation of the subset of the
// former reactive/ui fluent builder DSL that the reconciler test suite relies
// on. The reconciler is backend-agnostic; these builders construct NATIVE
// widgets (*qui.Container / *qw.Label / *qw.Button) so the existing type
// assertions in the tests keep holding. It mirrors reactive/ui's behavior but
// covers only what the tests reference.

import (
	"github.com/qizhanchan/qui"
	"github.com/qizhanchan/qui/reactive"
	qw "github.com/qizhanchan/qui/widgets"
)

// uiNode is anything that lowers to a reactive.Element (mirror of ui.Node).
type uiNode interface {
	Build() reactive.Element
}

// rawNode adapts a pre-built reactive.Element into a uiNode.
type rawNode struct{ elem reactive.Element }

func (r rawNode) Build() reactive.Element { return r.elem }

// uiElem wraps an existing reactive.Element so it can sit in a Children list.
func uiElem(e reactive.Element) uiNode { return rawNode{elem: e} }

// buildAll lowers a Node list, turning nil slots into reactive.Empty().
func buildAll(nodes []uiNode) []reactive.Element {
	out := make([]reactive.Element, 0, len(nodes))
	for _, n := range nodes {
		if n == nil {
			out = append(out, reactive.Empty())
			continue
		}
		out = append(out, n.Build())
	}
	return out
}

// ---------------------------------------------------------------------
// Label

type labelBuilder struct {
	key      string
	text     string
	bindText *reactive.Signal[string]
}

func uiLabel(text string) *labelBuilder { return &labelBuilder{text: text} }

func (b *labelBuilder) Key(k string) *labelBuilder { b.key = k; return b }

func (b *labelBuilder) BindText(sig *reactive.Signal[string]) *labelBuilder {
	b.bindText = sig
	return b
}

func (b *labelBuilder) Build() reactive.Element {
	bb := *b
	var unsub func()
	elem := reactive.Leaf[*qw.Label]("label", bb.key,
		func() *qw.Label {
			l := qw.NewLabel("")
			if bb.bindText != nil {
				unsub = reactive.BindWidget(l, bb.bindText, l.SetText)
			}
			return l
		},
		func(l *qw.Label) reactive.Flags {
			flags := reactive.FlagNone
			if bb.bindText == nil && l.Text() != bb.text {
				l.SetText(bb.text)
				flags |= reactive.FlagLayout
			}
			return flags
		})
	if bb.bindText != nil {
		elem.Destroy = func(qui.Widget) {
			if unsub != nil {
				unsub()
			}
		}
	}
	return elem
}

// ---------------------------------------------------------------------
// Button

type buttonBuilder struct {
	key     string
	text    string
	onClick func()
}

func uiButton(text string) *buttonBuilder { return &buttonBuilder{text: text} }

func (b *buttonBuilder) Key(k string) *buttonBuilder      { b.key = k; return b }
func (b *buttonBuilder) OnClick(fn func()) *buttonBuilder { b.onClick = fn; return b }

func (b *buttonBuilder) Build() reactive.Element {
	bb := *b
	return reactive.Leaf[*qw.Button]("button", bb.key,
		func() *qw.Button {
			return qw.NewButton("", nil)
		},
		func(btn *qw.Button) reactive.Flags {
			flags := reactive.FlagNone
			if btn.Text != bb.text {
				btn.Text = bb.text
				flags = reactive.FlagLayout
			}
			btn.OnClick = bb.onClick
			btn.SetEnabled(true)
			return flags
		})
}

// ---------------------------------------------------------------------
// Box (VBox / HBox)

type boxBuilder struct {
	kind     string
	key      string
	id       string
	dir      qui.Direction
	gap      float32
	align    qui.AlignCross
	padding  *qui.Insets
	setup    func(*qui.Container)
	onClick  func()
	children []uiNode
}

func uiVBox() *boxBuilder {
	return &boxBuilder{kind: "vbox", dir: qui.Vertical, align: qui.AlignStretch}
}

func uiHBox() *boxBuilder {
	return &boxBuilder{kind: "hbox", dir: qui.Horizontal, align: qui.AlignCenter}
}

func (b *boxBuilder) Key(k string) *boxBuilder { b.key = k; return b }
func (b *boxBuilder) ID(s string) *boxBuilder  { b.id = s; return b }
func (b *boxBuilder) Gap(g float32) *boxBuilder {
	b.gap = g
	return b
}

// Padding sets inside spacing (CSS shorthand: 1, 2, or 4 values).
func (b *boxBuilder) Padding(vals ...float32) *boxBuilder {
	switch len(vals) {
	case 1:
		b.padding = &qui.Insets{Top: vals[0], Right: vals[0], Bottom: vals[0], Left: vals[0]}
	case 2:
		b.padding = &qui.Insets{Top: vals[0], Right: vals[1], Bottom: vals[0], Left: vals[1]}
	case 4:
		b.padding = &qui.Insets{Top: vals[0], Right: vals[1], Bottom: vals[2], Left: vals[3]}
	default:
		panic("ui: Padding takes 1, 2, or 4 values")
	}
	return b
}

func (b *boxBuilder) Setup(fn func(*qui.Container)) *boxBuilder { b.setup = fn; return b }

// OnClick makes the whole box respond to a left click. Forces the
// gesture-aware container variant (mirror of ui's eventBox).
func (b *boxBuilder) OnClick(fn func()) *boxBuilder { b.onClick = fn; return b }

func (b *boxBuilder) Children(kids ...uiNode) *boxBuilder {
	b.children = append(b.children, kids...)
	return b
}

func (b *boxBuilder) newContainer() *qui.Container {
	c := qui.NewContainer(qui.FlexLayout{Direction: b.dir, Gap: b.gap, AlignItems: b.align})
	if b.setup != nil {
		b.setup(c)
	}
	if b.padding != nil {
		c.Style().Padding = *b.padding
	}
	return c
}

func (b *boxBuilder) Build() reactive.Element {
	bb := *b
	kids := buildAll(b.children)

	// Plain container when there are no gestures — keeps the common path a
	// bare *qui.Container (what tests assert on).
	if bb.onClick == nil {
		return reactive.Node[*qui.Container](bb.kind, bb.key,
			func() *qui.Container {
				c := bb.newContainer()
				c.SetID(bb.id)
				return c
			},
			func(*qui.Container) reactive.Flags { return reactive.FlagNone },
			reactive.SetContainerChildren,
			kids...)
	}

	// Gesture-aware variant: distinct kind so the reconciler never reuses a
	// plain container as an eventBox (or vice versa).
	onClick := bb.onClick
	return reactive.Node[*eventBox](bb.kind+"-evt", bb.key,
		func() *eventBox {
			e := &eventBox{Container: bb.newContainer(), onClick: onClick}
			e.SetSelf(e)
			e.SetID(bb.id)
			return e
		},
		func(e *eventBox) reactive.Flags {
			e.onClick = onClick
			return reactive.FlagNone
		},
		func(e *eventBox, kids []qui.Widget) {
			reactive.SetContainerChildren(e.Container, kids)
		},
		kids...)
}

// eventBox is a Container that also reacts to a left click at the bubble
// phase (mirror of the former reactive/ui eventBox).
type eventBox struct {
	*qui.Container
	onClick func()
}

func (e *eventBox) Role() string { return "group" }

func (e *eventBox) Handle(event qui.Event) bool {
	if me, ok := event.(qui.MouseEvent); ok && me.Type() == qui.EventMouseDown {
		if me.Button == qui.MouseButtonLeft && e.onClick != nil {
			e.onClick()
			return true
		}
	}
	return e.Container.Handle(event)
}

// ---------------------------------------------------------------------
// Conditionals

type emptyNode struct{}

func (emptyNode) Build() reactive.Element { return reactive.Empty() }

// uiNothing renders nothing while still occupying its conditional slot.
func uiNothing() uiNode { return emptyNode{} }

// uiIf returns child when cond is true, otherwise an empty slot (eager).
func uiIf(cond bool, child uiNode) uiNode {
	if cond && child != nil {
		return child
	}
	return emptyNode{}
}

// uiWhen is the lazy form of uiIf: build runs only when cond is true.
func uiWhen(cond bool, build func() uiNode) uiNode {
	if !cond || build == nil {
		return emptyNode{}
	}
	return uiIf(true, build())
}

// uiFrag groups multiple Nodes without introducing a container widget.
type fragNode struct{ kids []uiNode }

func (f fragNode) Build() reactive.Element {
	return reactive.Fragment(buildAll(f.kids)...)
}

func uiFrag(kids ...uiNode) uiNode { return fragNode{kids: kids} }

// ---------------------------------------------------------------------
// Signal-driven structural nodes

// uiShow mirrors ui.Show (reactive.Show).
func uiShow(cond *reactive.Signal[bool], build func() uiNode) uiNode {
	return rawNode{elem: reactive.Show("", cond, func() reactive.Element {
		n := build()
		if n == nil {
			return reactive.Empty()
		}
		return n.Build()
	})}
}

// forBuilder configures a uiForOf list's hosting container (mirror of ui.ForBuilder).
type forBuilder[T any] struct {
	key    string
	items  *reactive.Signal[[]T]
	render func(int, T) uiNode
	layout reactive.BoundLayout
}

func uiForOf[T any](items *reactive.Signal[[]T], render func(index int, item T) uiNode) *forBuilder[T] {
	return &forBuilder[T]{
		items:  items,
		render: render,
		layout: reactive.BoundLayout{Direction: qui.Vertical},
	}
}

func (b *forBuilder[T]) Horizontal() *forBuilder[T] {
	b.layout.Direction = qui.Horizontal
	return b
}

func (b *forBuilder[T]) Gap(g float32) *forBuilder[T]  { b.layout.Gap = g; return b }
func (b *forBuilder[T]) Grow(g float32) *forBuilder[T] { b.layout.Grow = g; return b }

func (b *forBuilder[T]) Build() reactive.Element {
	render := b.render
	return reactive.ForWith(b.key, b.layout, b.items, func(i int, item T) reactive.Element {
		n := render(i, item)
		if n == nil {
			return reactive.Empty()
		}
		return n.Build()
	})
}

// ---------------------------------------------------------------------
// Portals

type portalNode struct {
	opts  reactive.PortalOptions
	child uiNode
}

func (p portalNode) Build() reactive.Element {
	var childElem reactive.Element
	if p.child != nil {
		childElem = p.child.Build()
	}
	return reactive.PortalWith(p.opts, childElem)
}

// uiPortal shows child as a centered, non-modal window overlay.
func uiPortal(child uiNode) uiNode {
	return portalNode{opts: reactive.PortalOptions{}, child: child}
}

// uiModalPortal shows child above a click-to-dismiss scrim.
func uiModalPortal(onDismiss func(), child uiNode) uiNode {
	return portalNode{
		opts: reactive.PortalOptions{
			Modal:           true,
			Backdrop:        qui.Color{A: 0.45},
			OnBackdropClick: onDismiss,
		},
		child: child,
	}
}
