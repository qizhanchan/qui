package reactive

import "sync"

// Signal is a minimal reactive value container.
type Signal[T any] struct {
	mu       sync.RWMutex
	value    T
	nextID   int
	watchers map[int]func()

	// dispose releases upstream subscriptions when this is a derived signal.
	// Plain signals have no upstream resources, so Dispose is a harmless no-op.
	disposeOnce sync.Once
	dispose     func()
}

func NewSignal[T any](initial T) *Signal[T] {
	return &Signal[T]{
		value:    initial,
		watchers: make(map[int]func()),
	}
}

// Get returns the current value. Inside an auto-tracked Computed run
// it also registers this signal as a dependency (see track.go); the
// fast path outside tracked runs is one atomic load.
func (s *Signal[T]) Get() T {
	trackDep(s)
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.value
}

// Peek returns the current value WITHOUT registering a dependency in
// an auto-tracked Computed — Solid's untrack for a single read.
func (s *Signal[T]) Peek() T {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.value
}

// Set stores v and notifies watchers. Setting a value equal to the
// current one (valuesEqual) is a no-op — watchers don't fire.
func (s *Signal[T]) Set(v T) {
	watchers := s.setAndSnapshot(func(T) T { return v })
	notify(watchers)
}

// Update applies fn to the current value; equal results skip
// notification like Set.
func (s *Signal[T]) Update(fn func(current T) T) {
	if fn == nil {
		return
	}
	watchers := s.setAndSnapshot(fn)
	notify(watchers)
}

func (s *Signal[T]) Subscribe(fn func()) func() {
	if s == nil || fn == nil {
		return func() {}
	}

	s.mu.Lock()
	id := s.nextID
	s.nextID++
	s.watchers[id] = fn
	s.mu.Unlock()

	return func() {
		s.mu.Lock()
		delete(s.watchers, id)
		s.mu.Unlock()
	}
}

// Dispose releases the upstream subscriptions held by a derived signal
// returned from Map or Computed. It is idempotent. It does not dispose this
// signal's own subscribers: callers may still read it or Set it manually,
// but it will no longer follow its former sources.
//
// Dispose derived signals created for temporary UI scopes when that scope
// ends. Signals owned for the whole application normally need no disposal.
func (s *Signal[T]) Dispose() {
	if s == nil {
		return
	}
	s.disposeOnce.Do(func() {
		s.mu.Lock()
		dispose := s.dispose
		s.dispose = nil
		s.mu.Unlock()
		if dispose != nil {
			dispose()
		}
	})
}

func (s *Signal[T]) setDispose(dispose func()) {
	if s == nil {
		return
	}
	s.mu.Lock()
	s.dispose = dispose
	s.mu.Unlock()
}

func (s *Signal[T]) setAndSnapshot(update func(current T) T) []func() {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	next := update(s.value)
	if valuesEqual(s.value, next) {
		s.mu.Unlock()
		return nil
	}
	s.value = next
	watchers := make([]func(), 0, len(s.watchers))
	for _, watcher := range s.watchers {
		watchers = append(watchers, watcher)
	}
	s.mu.Unlock()
	return watchers
}

// anySignal is the non-generic view of a Signal, used by introspection to
// read a signal's current value and watcher count without knowing its
// element type T.
type anySignal interface {
	anyValue() any
	watcherCount() int
}

func (s *Signal[T]) anyValue() any {
	if s == nil {
		return nil
	}
	return s.Peek()
}

func (s *Signal[T]) watcherCount() int {
	if s == nil {
		return 0
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.watchers)
}

func notify(watchers []func()) {
	for _, watcher := range watchers {
		if watcher != nil {
			watcher()
		}
	}
}
