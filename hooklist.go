package qui

// hookList is an ordered list of callbacks where each registration can be
// removed again — the storage behind every On* registrar that returns a
// remover. Firing iterates a snapshot, so a callback may add or remove
// hooks (including itself) without disturbing the current pass.
type hookList[F any] struct {
	nextID uint64
	items  []hookItem[F]
}

type hookItem[F any] struct {
	id uint64
	fn F
}

// add appends fn and returns the function that removes exactly this
// registration (idempotent).
func (l *hookList[F]) add(fn F) (remove func()) {
	l.nextID++
	id := l.nextID
	l.items = append(l.items, hookItem[F]{id: id, fn: fn})
	return func() {
		for i, it := range l.items {
			if it.id == id {
				l.items = append(l.items[:i:i], l.items[i+1:]...)
				return
			}
		}
	}
}

// len reports how many callbacks are registered.
func (l *hookList[F]) len() int { return len(l.items) }

// snapshot returns the callbacks in registration order.
func (l *hookList[F]) snapshot() []F {
	if len(l.items) == 0 {
		return nil
	}
	out := make([]F, len(l.items))
	for i, it := range l.items {
		out[i] = it.fn
	}
	return out
}

// clear drops every registration.
func (l *hookList[F]) clear() { l.items = nil }
