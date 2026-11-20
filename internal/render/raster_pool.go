package render

import (
	"container/heap"
	"image"
	"os"
	"runtime"
	"strconv"
	"sync"
	"sync/atomic"
	"time"
)

// raster_pool.go — worker-thread rasterization of Pictures (Chromium's
// RasterWorkerPool / TileManager task graph, reduced to what qui needs).
//
// The UI thread records a Picture (cheap: picture.go) and schedules a
// RasterTask for a rectangle of it. A pool of worker goroutines plays the
// picture into a fresh RGBA. The UI thread either:
//
//   - keeps showing what it had (an older bitmap, or nothing for content
//     still outside the visible area) and adopts the result when the
//     task's callback tells it — it never blocks; or
//   - needs the pixels this frame (content becoming visible with nothing
//     to show): Wait runs a still-queued task on the calling thread at
//     once, or waits for the worker that already has it. Several such
//     tasks scheduled before the first Wait run in parallel.
//
// Playback is pure pixel work (Picture.WorkerSafe); pictures that are not
// worker-safe run inline when scheduled.

// RasterTask is one scheduled rasterization.
type RasterTask struct {
	pic    *Picture
	rect   image.Rectangle
	prio   int
	seq    uint64
	index  int // heap index; -1 when not queued
	onDone func(*RasterTask)

	state atomic.Int32
	done  chan struct{}
	img   *image.RGBA
	dur   time.Duration
}

const (
	taskQueued int32 = iota
	taskRunning
	taskDone
	taskCanceled
)

// Image is the finished raster, nil until Done (or after Cancel).
func (t *RasterTask) Image() *image.RGBA {
	if t == nil || t.state.Load() != taskDone {
		return nil
	}
	return t.img
}

// Done reports whether the raster is finished.
func (t *RasterTask) Done() bool { return t != nil && t.state.Load() == taskDone }

// Duration is how long playback took.
func (t *RasterTask) Duration() time.Duration {
	if !t.Done() {
		return 0
	}
	return t.dur
}

// Cancel drops a task that has not started; a running one finishes, but
// its callback is not called.
func (t *RasterTask) Cancel() {
	if t == nil {
		return
	}
	for {
		s := t.state.Load()
		if s == taskDone || s == taskCanceled {
			return
		}
		if t.state.CompareAndSwap(s, taskCanceled) {
			if s == taskQueued {
				rasterPool.remove(t)
				close(t.done)
			}
			return
		}
	}
}

// Wait returns the raster, running the task on the calling thread when no
// worker has picked it up yet.
func (t *RasterTask) Wait() *image.RGBA {
	if t == nil {
		return nil
	}
	if rasterPool.remove(t) && t.state.CompareAndSwap(taskQueued, taskRunning) {
		rasterStats.stolen.Add(1)
		t.run(false)
	}
	<-t.done
	return t.Image()
}

func (t *RasterTask) run(callback bool) {
	start := time.Now()
	img := image.NewRGBA(image.Rect(0, 0, t.rect.Dx(), t.rect.Dy()))
	t.pic.RasterizeInto(img, t.rect.Min)
	t.img, t.dur = img, time.Since(start)
	rasterStats.rasters.Add(1)
	rasterStats.nanos.Add(int64(t.dur))
	if !t.state.CompareAndSwap(taskRunning, taskDone) {
		t.img = nil // canceled while running
		close(t.done)
		return
	}
	close(t.done)
	if callback && t.onDone != nil {
		t.onDone(t)
	}
}

// ScheduleRaster queues pic's rect (device pixels of the recording) for
// rasterization. Lower prio runs first; equal prio runs in order.
// onDone, when non-nil, is called on the worker goroutine when the raster
// is ready — not when the caller collected it itself through Wait.
func ScheduleRaster(pic *Picture, rect image.Rectangle, prio int, onDone func(*RasterTask)) *RasterTask {
	t := &RasterTask{pic: pic, rect: rect, prio: prio, onDone: onDone, index: -1, done: make(chan struct{})}
	if !pic.WorkerSafe() || RasterThreads() == 0 {
		t.state.Store(taskRunning)
		t.run(false)
		return t
	}
	rasterPool.push(t)
	return t
}

// RasterThreads is the number of raster workers: QUI_RASTER_THREADS, or
// half the CPUs, between 1 and 4 (Chromium's default range). 0 turns
// worker raster off: every task runs inline.
func RasterThreads() int {
	rasterThreadsOnce.Do(func() {
		n := runtime.NumCPU() / 2
		if v, err := strconv.Atoi(os.Getenv("QUI_RASTER_THREADS")); err == nil && v >= 0 {
			n = v
		} else {
			n = max(1, min(4, n))
		}
		rasterThreadsN = n
	})
	return rasterThreadsN
}

var (
	rasterThreadsOnce sync.Once
	rasterThreadsN    int
)

type rasterQueue []*RasterTask

func (q rasterQueue) Len() int { return len(q) }
func (q rasterQueue) Less(i, j int) bool {
	if q[i].prio != q[j].prio {
		return q[i].prio < q[j].prio
	}
	return q[i].seq < q[j].seq
}
func (q rasterQueue) Swap(i, j int) {
	q[i], q[j] = q[j], q[i]
	q[i].index, q[j].index = i, j
}
func (q *rasterQueue) Push(x any) {
	t := x.(*RasterTask)
	t.index = len(*q)
	*q = append(*q, t)
}
func (q *rasterQueue) Pop() any {
	old := *q
	t := old[len(old)-1]
	old[len(old)-1] = nil
	*q = old[:len(old)-1]
	t.index = -1
	return t
}

var rasterPool = &workerPool{}

type workerPool struct {
	mu      sync.Mutex
	cond    *sync.Cond
	queue   rasterQueue
	seq     uint64
	started int
}

func (p *workerPool) push(t *RasterTask) {
	p.mu.Lock()
	if p.cond == nil {
		p.cond = sync.NewCond(&p.mu)
	}
	for p.started < RasterThreads() {
		p.started++
		go p.worker()
	}
	p.seq++
	t.seq = p.seq
	heap.Push(&p.queue, t)
	p.cond.Signal()
	p.mu.Unlock()
}

// remove takes t off the queue; false when a worker already has it.
func (p *workerPool) remove(t *RasterTask) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	if t.index < 0 {
		return false
	}
	heap.Remove(&p.queue, t.index)
	return true
}

func (p *workerPool) worker() {
	for {
		p.mu.Lock()
		for len(p.queue) == 0 {
			p.cond.Wait()
		}
		t := heap.Pop(&p.queue).(*RasterTask)
		p.mu.Unlock()
		if t.state.CompareAndSwap(taskQueued, taskRunning) {
			t.run(true)
		}
	}
}

var rasterStats struct{ rasters, stolen, nanos atomic.Int64 }

// RasterStats reports rasters run (on workers or inline), how many of
// them a Wait ran on the calling thread, and their total playback time.
func RasterStats() (rasters, stolen int64, total time.Duration) {
	return rasterStats.rasters.Load(), rasterStats.stolen.Load(), time.Duration(rasterStats.nanos.Load())
}
