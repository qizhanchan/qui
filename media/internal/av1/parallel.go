package av1

import (
	"runtime"
	"sync"
	"sync/atomic"
)

// parallelFor runs fn(worker, i) for i = 0..n-1 on up to GOMAXPROCS
// goroutines; worker (0..workers-1) identifies per-goroutine scratch.
// It returns once all calls are done; a panic in any call is re-raised
// in the caller (so Decode's recover sees it).
func parallelFor(n int, fn func(worker, i int)) {
	nw := min(runtime.GOMAXPROCS(0), n)
	if nw <= 1 {
		for i := 0; i < n; i++ {
			fn(0, i)
		}
		return
	}
	var next atomic.Int32
	var wg sync.WaitGroup
	var failed atomic.Pointer[any]
	for k := 0; k < nw; k++ {
		wg.Add(1)
		go func(k int) {
			defer wg.Done()
			defer func() {
				if r := recover(); r != nil {
					failed.CompareAndSwap(nil, &r)
					next.Store(int32(n)) // stop handing out work
				}
			}()
			for {
				i := int(next.Add(1)) - 1
				if i >= n {
					return
				}
				fn(k, i)
			}
		}(k)
	}
	wg.Wait()
	if r := failed.Load(); r != nil {
		panic(*r)
	}
}

func workers() int { return runtime.GOMAXPROCS(0) }
