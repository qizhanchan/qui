package webview

import "sync"

// handlerTable stores the Go endpoints for JS→Go bridge calls. The
// backend looks up handlers by name when an inbound message arrives;
// the WebView widget owns one table per Page so handlers don't leak
// across pages.
type handlerTable struct {
	mu       sync.RWMutex
	handlers map[string]MessageHandler
}

func newHandlerTable() *handlerTable {
	return &handlerTable{handlers: make(map[string]MessageHandler)}
}

// Set registers (or replaces) a handler. nil fn deletes the entry —
// equivalent to Delete but allows RegisterHandler to express both
// shapes with one method.
func (t *handlerTable) Set(name string, fn MessageHandler) {
	t.mu.Lock()
	if fn == nil {
		delete(t.handlers, name)
	} else {
		t.handlers[name] = fn
	}
	t.mu.Unlock()
}

// Delete removes a handler. No-op if the name isn't registered.
func (t *handlerTable) Delete(name string) {
	t.mu.Lock()
	delete(t.handlers, name)
	t.mu.Unlock()
}

// Get returns the handler for name, or nil if none is registered.
// Safe to call from any goroutine.
func (t *handlerTable) Get(name string) MessageHandler {
	t.mu.RLock()
	fn := t.handlers[name]
	t.mu.RUnlock()
	return fn
}

// Names returns a snapshot of registered handler names. Useful for
// tests and for the renderer-side V8 stub injection (the backend
// queries this list at DOM-ready time to know what methods to attach
// to window.qui).
func (t *handlerTable) Names() []string {
	t.mu.RLock()
	out := make([]string, 0, len(t.handlers))
	for n := range t.handlers {
		out = append(out, n)
	}
	t.mu.RUnlock()
	return out
}
