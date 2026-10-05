package qui

// Per-widget focus and key hooks.
//
// Any widget embedding BaseWidget can be observed without subclassing: an
// autocomplete wires OnKeyDown on a plain widgets.Input to steer its list
// with the arrow keys, a form validates a field OnBlur. (The htmlcss El
// has its own equivalents; these are for native widgets.)

type widgetHooks struct {
	onFocus []func()
	onBlur  []func()
	onKey   []func(KeyEvent) bool
}

type hookHost interface {
	widgetHooks() *widgetHooks
}

func (b *BaseWidget) widgetHooks() *widgetHooks { return b.hooks }

func (b *BaseWidget) ensureHooks() *widgetHooks {
	if b.hooks == nil {
		b.hooks = &widgetHooks{}
	}
	return b.hooks
}

// OnFocus registers fn to run after the widget gains keyboard focus.
func (b *BaseWidget) OnFocus(fn func()) {
	if fn != nil {
		b.ensureHooks().onFocus = append(b.ensureHooks().onFocus, fn)
	}
}

// OnBlur registers fn to run after the widget loses keyboard focus.
func (b *BaseWidget) OnBlur(fn func()) {
	if fn != nil {
		b.ensureHooks().onBlur = append(b.ensureHooks().onBlur, fn)
	}
}

// OnKeyDown registers fn to see key-down events aimed at the widget (it is
// focused) BEFORE the widget's own handling. Returning true consumes the
// key: the widget never sees it and it doesn't bubble.
func (b *BaseWidget) OnKeyDown(fn func(KeyEvent) bool) {
	if fn != nil {
		b.ensureHooks().onKey = append(b.ensureHooks().onKey, fn)
	}
}

func runFocusHooks(w Widget, focused bool) {
	h, ok := w.(hookHost)
	if !ok || h.widgetHooks() == nil {
		return
	}
	hooks := h.widgetHooks()
	list := hooks.onBlur
	if focused {
		list = hooks.onFocus
	}
	for _, fn := range append([]func(){}, list...) {
		fn()
	}
}

func runKeyHook(target Widget, event Event) bool {
	ke, ok := event.(KeyEvent)
	if !ok || ke.Type() != EventKeyDown {
		return false
	}
	h, ok := target.(hookHost)
	if !ok || h.widgetHooks() == nil {
		return false
	}
	for _, fn := range append([]func(KeyEvent) bool{}, h.widgetHooks().onKey...) {
		if fn(ke) {
			return true
		}
	}
	return false
}
