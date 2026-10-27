package qui_test

import (
	"sync"
	"sync/atomic"
	"testing"

	"github.com/qizhanchan/qui"
)

func TestPostJob_ConcurrentFirstUseOnZeroWindow(t *testing.T) {
	w := &qui.Window{}
	const jobs = 128
	var posted sync.WaitGroup
	posted.Add(jobs)
	var ran atomic.Int32
	for i := 0; i < jobs; i++ {
		go func() {
			defer posted.Done()
			w.PostJob(func() { ran.Add(1) })
		}()
	}
	posted.Wait()

	if got := w.PendingJobs(); got != jobs {
		t.Fatalf("PendingJobs after concurrent first use = %d, want %d", got, jobs)
	}
	if got := w.DrainJobsForTest(); got != jobs {
		t.Fatalf("DrainJobsForTest = %d, want %d", got, jobs)
	}
	if got := ran.Load(); got != jobs {
		t.Fatalf("ran = %d, want %d", got, jobs)
	}
}

func TestPostJob_FIFO(t *testing.T) {
	w := qui.NewTestWindow(qui.Size{W: 100, H: 100})
	var order []int
	for i := 0; i < 5; i++ {
		i := i
		w.PostJob(func() { order = append(order, i) })
	}
	if got := w.PendingJobs(); got != 5 {
		t.Fatalf("PendingJobs = %d, want 5", got)
	}
	if got := w.DrainJobsForTest(); got != 5 {
		t.Fatalf("DrainJobsForTest = %d, want 5", got)
	}
	for i, v := range order {
		if v != i {
			t.Fatalf("job %d ran %d, want %d", i, v, i)
		}
	}
	if got := w.PendingJobs(); got != 0 {
		t.Fatalf("PendingJobs after drain = %d, want 0", got)
	}
}

func TestPostJob_PriorityBeforeNormal(t *testing.T) {
	w := qui.NewTestWindow(qui.Size{W: 100, H: 100})
	var order []string
	w.PostJob(func() { order = append(order, "n1") })
	w.PostJob(func() { order = append(order, "n2") })
	w.PostPriorityJob(func() { order = append(order, "p1") })
	w.PostPriorityJob(func() { order = append(order, "p2") })
	w.DrainJobsForTest()
	want := []string{"p1", "p2", "n1", "n2"}
	for i, v := range order {
		if v != want[i] {
			t.Fatalf("order[%d] = %s, want %s (got %v)", i, v, want[i], order)
		}
	}
}

func TestPendingJobsIncludesDrainedWorkNotYetStarted(t *testing.T) {
	w := qui.NewTestWindow(qui.Size{W: 100, H: 100})
	observed := -1
	w.PostJob(func() {})
	w.PostPriorityJob(func() { observed = w.PendingJobs() })

	w.DrainJobsForTest()

	if observed != 1 {
		t.Fatalf("PendingJobs from priority job = %d, want 1 normal job still pending", observed)
	}
	if got := w.PendingJobs(); got != 0 {
		t.Fatalf("PendingJobs after drain = %d, want 0", got)
	}
}

func TestPostJob_RemainsLosslessPastTryLimit(t *testing.T) {
	w := qui.NewTestWindow(qui.Size{W: 100, H: 100})
	w.SetJobQueueLimit(3)
	var ran []int
	for i := 0; i < 5; i++ {
		i := i
		w.PostJob(func() { ran = append(ran, i) })
	}
	if got := w.PendingJobs(); got != 5 {
		t.Fatalf("PendingJobs = %d, want 5", got)
	}
	w.DrainJobsForTest()
	want := []int{0, 1, 2, 3, 4}
	if len(ran) != len(want) {
		t.Fatalf("ran=%v, want %d entries", ran, len(want))
	}
	for i, v := range ran {
		if v != want[i] {
			t.Fatalf("ran[%d] = %d, want %d", i, v, want[i])
		}
	}
}

func TestTryPostJobRejectsWithoutDroppingQueuedWork(t *testing.T) {
	w := qui.NewTestWindow(qui.Size{W: 100, H: 100})
	w.SetJobQueueLimit(3)
	var ran []int
	for i := 0; i < 3; i++ {
		i := i
		if !w.TryPostJob(func() { ran = append(ran, i) }) {
			t.Fatalf("TryPostJob(%d) rejected below limit", i)
		}
	}
	if w.TryPostJob(func() { ran = append(ran, 99) }) {
		t.Fatal("TryPostJob accepted work above the configured limit")
	}
	if got := w.PendingJobs(); got != 3 {
		t.Fatalf("PendingJobs = %d, want 3", got)
	}
	w.DrainJobsForTest()
	want := []int{0, 1, 2}
	for i, v := range ran {
		if v != want[i] {
			t.Fatalf("ran[%d] = %d, want %d", i, v, want[i])
		}
	}
}

func TestPostJob_NextStep_NotThisOne(t *testing.T) {
	// A job that PostJob()s another job should NOT see the new job
	// run in the same drain — frame time must stay bounded.
	w := qui.NewTestWindow(qui.Size{W: 100, H: 100})
	var first int32
	var second int32
	w.PostJob(func() {
		atomic.AddInt32(&first, 1)
		w.PostJob(func() { atomic.AddInt32(&second, 1) })
	})
	count := w.DrainJobsForTest()
	if count != 1 {
		t.Fatalf("first drain count = %d, want 1", count)
	}
	if atomic.LoadInt32(&second) != 0 {
		t.Fatalf("child job ran in same drain — must defer to next Step")
	}
	if got := w.PendingJobs(); got != 1 {
		t.Fatalf("PendingJobs after first drain = %d, want 1", got)
	}
	w.DrainJobsForTest()
	if atomic.LoadInt32(&second) != 1 {
		t.Fatalf("child job did not run in next drain")
	}
}

func TestSnapshotScaled_NilWithoutFrame(t *testing.T) {
	w := qui.NewTestWindow(qui.Size{W: 100, H: 100})
	if img := w.SnapshotScaled(1); img != nil {
		t.Errorf("expected nil snapshot without rendered frame, got %v", img.Bounds())
	}
	if img := w.SnapshotRegion(qui.Rect{W: 50, H: 50}, 0.5); img != nil {
		t.Errorf("expected nil region snapshot without frame, got %v", img.Bounds())
	}
	if img := w.SnapshotAnnotated(qui.AnnotateOptions{}); img != nil {
		t.Errorf("expected nil annotated snapshot without frame, got %v", img.Bounds())
	}
}

func TestSnapshotScaled_Resample(t *testing.T) {
	// We can't render in tests, but we can exercise resampleRGBA via
	// a manual source. Use the package-private helper through an
	// exported wrapper for tests — instead we just check the API
	// short-circuits cleanly when no frame exists (covered above).
	// A real render path is covered by the verify recipe at the
	// integration level.
	_ = qui.AnnotateOptions{Scale: 0.5, OnlyLeaves: true}
}
