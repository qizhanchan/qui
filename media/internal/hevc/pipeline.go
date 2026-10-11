package hevc

import (
	"sync"
	"sync/atomic"
)

// Picture-level pipelining, as in the H.264 decoder: the in-loop filters
// (deblocking and SAO) of picture n run on their own goroutine while the
// decoder parses and reconstructs picture n+1.
//
//   - The filter walks CTB rows: deblock row r, then SAO row r-1, then
//     publish that the luma lines above row r are final. Motion
//     compensation reading a reference waits (rarely: only when a vector
//     points below what is finished) for the lines it reads; output and
//     buffer reuse wait for all.
//   - The decoder alternates between two frame contexts: the filter reads
//     the one its picture was decoded with while the next picture fills
//     the other. One filter job runs at a time.
//   - Output is a queue of pictures; Next copies a picture out only once
//     it is filtered, so it never blocks the decode thread.
//
// Results are bit-identical to filtering in place after decoding.

// progress tracks a picture's in-loop filtering.
type progress struct {
	lines   atomic.Int32 // luma lines that are final
	done    chan struct{}
	mu      sync.Mutex
	cond    *sync.Cond
	waiters atomic.Int32
}

// begin marks filtering as pending.
func (pr *progress) begin() {
	pr.lines.Store(0)
	pr.done = make(chan struct{})
	if pr.cond == nil {
		pr.cond = sync.NewCond(&pr.mu)
	}
}

// complete marks a picture that needs no (more) filtering.
func (pr *progress) complete(lines int) {
	pr.lines.Store(int32(lines))
	pr.done = nil
}

// publish records that the first lines luma lines are final.
func (pr *progress) publish(lines int) {
	pr.lines.Store(int32(lines))
	if pr.waiters.Load() > 0 {
		pr.mu.Lock()
		pr.cond.Broadcast()
		pr.mu.Unlock()
	}
}

// waitLines blocks until at least lines luma lines are final.
func (pr *progress) waitLines(lines int) {
	if pr.done == nil || int(pr.lines.Load()) >= lines {
		return
	}
	pr.waiters.Add(1)
	pr.mu.Lock()
	for int(pr.lines.Load()) < lines {
		pr.cond.Wait()
	}
	pr.mu.Unlock()
	pr.waiters.Add(-1)
}

// wait blocks until filtering has finished.
func (pr *progress) wait() {
	if pr.done != nil {
		<-pr.done
	}
}

// finished reports, without blocking, whether filtering is done.
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

// loopFilter holds what the in-loop filters of one picture read: the SPS,
// the frame context the picture was decoded with, and SAO's copy of the
// deblocked samples.
type loopFilter struct {
	active *SPS
	fc     *frameCtx
	saoTmp [3][]byte
	copied int // luma lines copied into saoTmp
}

// run deblocks and applies SAO to pic CTB row by CTB row, calling publish
// with the number of luma lines that are final.
func (d *loopFilter) run(pic *picture, publish func(lines int)) {
	sp := d.active
	H := sp.Height
	sao := d.saoBegin(pic)
	rows := sp.PicHCtb
	for r := 0; r <= rows; r++ {
		if r < rows {
			top, bot := r<<sp.Log2Ctb, min((r+1)<<sp.Log2Ctb, H)
			d.deblockEdges(pic, true, top, bot)
			d.deblockEdges(pic, false, top, bot)
			if sao {
				// The next row's top edge may still change three lines
				// above it (one in chroma).
				lim := bot - 4
				if r == rows-1 {
					lim = H
				}
				d.saoCopy(pic, lim)
			}
		}
		if r > 0 {
			if sao {
				d.saoRow(pic, r-1)
			}
			publish(min(r<<sp.Log2Ctb, H))
		}
	}
	publish(H)
}

// startFilter runs the in-loop filters of p, on a goroutine unless
// syncFilter is set. The previous job must have finished.
func (d *Decoder) startFilter(p *picture) {
	lf := &d.lf
	lf.active, lf.fc = d.active, d.fc
	p.prog.begin()
	done := p.prog.done
	if d.syncFilter {
		d.runFilter(p)
		close(done)
		return
	}
	d.filtering = p
	go func() {
		defer close(done)
		d.runFilter(p)
	}()
}

func (d *Decoder) runFilter(p *picture) {
	defer func() {
		if r := recover(); r != nil {
			// A corrupt picture: release any waiters with what exists.
			p.prog.publish(p.h)
		}
	}()
	d.lf.run(p, p.prog.publish)
}

// waitFilter waits for the running filter job, if any.
func (d *Decoder) waitFilter() {
	if d.filtering != nil {
		d.filtering.prog.wait()
		d.filtering = nil
	}
}
