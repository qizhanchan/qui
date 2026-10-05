package reactive

import (
	"context"
	"strconv"
	"sync"
	"sync/atomic"
)

var useIDCounter atomic.Uint64

// UseId returns an id unique within the process and stable for the life of
// the component instance — for pairing a label with its field
// (`for` / `id`), aria-style references, or agent selectors that must not
// collide when a component is rendered twice.
func UseId() string {
	ref := UseRef("")
	if *ref == "" {
		*ref = "q" + strconv.FormatUint(useIDCounter.Add(1), 36)
	}
	return *ref
}

// Resource is the state of an async load started by UseResource.
type Resource[T any] struct {
	// Value is the last successful result (kept while a reload runs, so a
	// list doesn't blank out on refresh).
	Value T
	// Err is the last load's error, nil after a success.
	Err error
	// Loading is true while a load is in flight.
	Loading bool
	// Ready is true once any load has succeeded.
	Ready bool
	// Reload starts a fresh load with the current deps.
	Reload func()
}

type resourceState[T any] struct {
	value   T
	err     error
	loading bool
	ready   bool
}

// UseResource runs fetch off the UI goroutine whenever deps change (and on
// mount) and re-renders with its result. The context passed to fetch is
// cancelled when deps change again, on Reload, or when the component
// unmounts, and a result that arrives after that is dropped — so a slow
// response can never overwrite a newer one or touch an unmounted
// component. Results are delivered on the UI goroutine.
//
//	user := reactive.UseResource(func(ctx context.Context) (User, error) {
//	    return api.User(ctx, id)
//	}, id)
//	if user.Loading && !user.Ready { return h.P("Loading…") }
func UseResource[T any](fetch func(ctx context.Context) (T, error), deps ...any) Resource[T] {
	scope := requireScope()
	rt := scope.runtime
	st, _, update := UseStateFn(resourceState[T]{loading: true})
	reload, setReload := UseState(0)
	ctl := UseRef(resourceControl{})

	UseEffect(func() func() {
		ctx, cancel := context.WithCancel(context.Background())
		gen := ctl.next(cancel)
		update(func(s resourceState[T]) resourceState[T] {
			if s.loading {
				return s
			}
			s.loading = true
			return s
		})
		go func() {
			v, err := fetch(ctx)
			deliver := func() {
				if !ctl.current(gen) || ctx.Err() != nil {
					return
				}
				update(func(s resourceState[T]) resourceState[T] {
					s.loading = false
					s.err = err
					if err == nil {
						s.value, s.ready = v, true
					}
					return s
				})
			}
			if rt.window != nil {
				rt.window.PostJob(deliver)
			} else {
				deliver()
			}
		}()
		return func() { cancel() }
	}, append(append([]any(nil), deps...), reload)...)

	return Resource[T]{
		Value:   st.value,
		Err:     st.err,
		Loading: st.loading,
		Ready:   st.ready,
		Reload:  func() { setReload(reload + 1) },
	}
}

// resourceControl tracks the in-flight load generation.
type resourceControl struct {
	mu     *sync.Mutex
	gen    uint64
	cancel context.CancelFunc
}

func (c *resourceControl) next(cancel context.CancelFunc) uint64 {
	if c.mu == nil {
		c.mu = &sync.Mutex{}
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.cancel != nil {
		c.cancel()
	}
	c.gen++
	c.cancel = cancel
	return c.gen
}

func (c *resourceControl) current(gen uint64) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.gen == gen
}
