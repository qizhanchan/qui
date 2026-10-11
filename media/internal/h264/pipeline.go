package h264

import (
	"sync"
	"sync/atomic"
)

// Picture-level pipelining. Deblocking picture n runs on its own goroutine
// while the decoder parses and reconstructs picture n+1, so a stream uses
// two cores instead of one without any fine-grained handoff:
//
//   - progress publishes how many macroblock rows of a picture are fully
//     deblocked. Motion compensation reading a reference waits (rarely —
//     only when a vector points below what is finished) until the rows it
//     reads are final; concealment, gap filling and output wait for all.
//   - The decoder double-buffers its picCtx: the deblocker reads the one
//     its picture was decoded with while the next picture fills the other.
//   - Output is a queue of pictures; Next copies a picture out only once
//     its deblocking is done, so it never blocks the decode thread.
//
// Results are bit-identical to deblocking in place: every sample is read
// only after its last write.

// progress tracks a picture's deblocking.
type progress struct {
	rows    atomic.Int32 // macroblock rows completely filtered
	done    chan struct{}
	mu      sync.Mutex
	cond    *sync.Cond
	waiters atomic.Int32
}

// begin marks deblocking as pending.
func (pr *progress) begin() {
	pr.rows.Store(0)
	pr.done = make(chan struct{})
	if pr.cond == nil {
		pr.cond = sync.NewCond(&pr.mu)
	}
}

// complete marks a picture whose deblocking (if any) ran synchronously.
func (pr *progress) complete(rows int) {
	pr.rows.Store(int32(rows))
	pr.done = nil
}

// publish records that rows macroblock rows are final.
func (pr *progress) publish(rows int) {
	pr.rows.Store(int32(rows))
	if pr.waiters.Load() > 0 {
		pr.mu.Lock()
		pr.cond.Broadcast()
		pr.mu.Unlock()
	}
}

// waitRows blocks until at least rows macroblock rows are final.
func (pr *progress) waitRows(rows int) {
	if pr.done == nil || int(pr.rows.Load()) >= rows {
		return
	}
	pr.waiters.Add(1)
	pr.mu.Lock()
	for int(pr.rows.Load()) < rows {
		pr.cond.Wait()
	}
	pr.mu.Unlock()
	pr.waiters.Add(-1)
}

// wait blocks until deblocking has finished.
func (pr *progress) wait() {
	if pr.done != nil {
		<-pr.done
	}
}

// finished reports, without blocking, whether deblocking is done.
func (pr *progress) finished() bool {
	if pr.done == nil {
		return true
	}
	select {
	case <-pr.done:
		return true
	default:
		return false
	}
}

// deblockAsync filters p on a new goroutine using pc, publishing progress
// row by row.
func (pc *picCtx) deblockAsync(p *picture) {
	p.prog.begin()
	done := p.prog.done
	go func() {
		defer close(done)
		mbW := pc.mbW
		for y := 0; y < pc.mbH; y++ {
			for x := 0; x < mbW; x++ {
				pc.deblockMB(p, y*mbW+x)
			}
			p.prog.publish(y + 1)
		}
	}()
}

// waitRef waits until the reference rows a prediction block reads are
// final: luma rows up to ly (inclusive) and chroma rows up to cy.
// Filtering macroblock row k rewrites up to three luma rows (one chroma
// row) above it, so luma row Y is final once rows (Y+3)/16+1 are done.
func (p *picture) waitRef(ly, cy int) {
	if p.prog.done == nil {
		return
	}
	need := max((max(ly, 0)+3)/16+1, (max(cy, 0)+1)/8+1)
	p.prog.waitRows(min(need, p.mbH))
}
