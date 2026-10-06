package qui

// Per-widget focus and key hooks.
//
// Any widget embedding BaseWidget can be observed without subclassing: an
// autocomplete wires OnKeyDown on a plain widgets.Input to steer its list
// with the arrow keys, a form validates a field OnBlur. (The htmlcss El
// has its own equivalents; these are for native widgets.)

type widgetHooks struct {
	onFocus hookList[func()]
	onBlur  hookList[func()]
	onKey   hookList[func(KeyEvent) bool]
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

// OnFocus registers fn to run after the widget gains keyboard focus, and
// returns the function that removes it (call it when whatever installed
// the hook goes away, or the closure keeps running).
func (b *BaseWidget) OnFocus(fn func()) (remove func()) {
	if fn == nil {
		return func() {}
	}
	return b.ensureHooks().onFocus.add(fn)
}

// OnBlur registers fn to run after the widget loses keyboard focus, and
// returns its remover.
func (b *BaseWidget) OnBlur(fn func()) (remove func()) {
	if fn == nil {
		return func() {}
	}
	return b.ensureHooks().onBlur.add(fn)
}

// OnKeyDown registers fn to see key-down events aimed at the widget (it is
// focused) BEFORE the widget's own handling — Tab included, ahead of focus
// navigation, so an autocomplete can take Tab to accept a suggestion.
// Returning true consumes the key: the widget never sees it, it doesn't
// bubble, and Tab doesn't move focus. Returns the hook's remover.
func (b *BaseWidget) OnKeyDown(fn func(KeyEvent) bool) (remove func()) {
	if fn == nil {
		return func() {}
	}
	return b.ensureHooks().onKey.add(fn)
}

func runFocusHooks(w Widget, focused bool) {
	h, ok := w.(hookHost)
	if !ok || h.widgetHooks() == nil {
		return
	}
	hooks := h.widgetHooks()
	list := &hooks.onBlur
	if focused {
		list = &hooks.onFocus
	}
	for _, fn := range list.snapshot() {
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
	for _, fn := range h.widgetHooks().onKey.snapshot() {
		if fn(ke) {
			return true
		}
	}
	return false
}
