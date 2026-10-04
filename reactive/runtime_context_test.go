package reactive_test

import (
	"sync"
	"testing"
	"time"

	"github.com/qizhanchan/qui"
	"github.com/qizhanchan/qui/reactive"
)

// Host hooks resolve their backend through CurrentRuntime. Separate
// Runtime.Render calls may originate on separate goroutines, so each pass
// must keep that context (and the hook scope) isolated for its full duration.
func TestConcurrentRuntimePassesKeepOwnHostContext(t *testing.T) {
	start := make(chan struct{})
	var rtA, rtB *reactive.Runtime
	var gotA, gotB *reactive.Runtime

	makeRender := func(got **reactive.Runtime, kind string) func() reactive.Element {
		return func() reactive.Element {
			<-start
			// Without full-pass serialization both render functions reach this
			// point with competing process-global contexts.
			time.Sleep(10 * time.Millisecond)
			return reactive.Leaf(kind, "", func() *qui.Container {
				*got = reactive.CurrentRuntime()
				return qui.NewContainer(nil)
			}, nil)
		}
	}
	rtA = reactive.NewRuntime(nil, makeRender(&gotA, "a"))
	rtB = reactive.NewRuntime(nil, makeRender(&gotB, "b"))

	var wg sync.WaitGroup
	wg.Add(2)
	go func() { defer wg.Done(); rtA.Render() }()
	go func() { defer wg.Done(); rtB.Render() }()
	close(start)
	wg.Wait()

	if gotA != rtA || gotB != rtB {
		t.Fatalf("CurrentRuntime crossed runtimes: A=%p want %p, B=%p want %p", gotA, rtA, gotB, rtB)
	}
}
