package av1

import "sync"

// In-frame filter pipelining. A frame with a single tile decodes
// superblock row by superblock row on one goroutine, which on many cores
// leaves the in-loop filters (deblocking, CDEF, loop restoration) as the
// only parallel work, run after the whole frame. Here they run on their
// own goroutine instead, in bands of 64 luma rows that trail the tile
// decoder, so a frame is done shortly after its last superblock row:
//
//   - Deblocking filters band k in place once decoding has passed row
//     64(k+1): intra prediction of the next superblock row reads the
//     unfiltered row above it. Vertical then horizontal edges of a band
//     equal the spec's frame-wide passes, since the horizontal edges of
//     band k read no rows below it and the vertical edges of band k+1
//     only its own rows.
//   - CDEF filters band k once band k+1 is deblocked (it reads two rows
//     below the band), into a frame the band is first copied to.
//   - Loop restoration filters stripe k (rows 64k-8 .. 64k+55) once CDEF
//     has band k, into a frame the stripe is first copied to.
//
// Nothing else reads the frame's samples until finishFrame waits, so
// results are bit-identical to filtering after decoding
// (TestPipelinedFilterMatchesInPlace). Superres frames (upscaled between
// CDEF and restoration) and the encoder keep the frame-wide path.

// filterPipe is the filter goroutine of one frame.
type filterPipe struct {
	mu      sync.Mutex
	cond    *sync.Cond
	decoded int // luma rows fully decoded
	aborted bool
	done    chan struct{}
	failed  any // a panic of the filter goroutine, re-raised by finish

	cdef, lr bool   // the stages that run
	cdefF    *frame // CDEF output (nil without CDEF)
	out      *frame // loop restoration output (nil without LR)
	cdefRows int    // luma rows of cdefF copied from the frame
	outRows  int    // luma rows of out copied from the CDEF output
	states   [3]lrState
}

// startPipe starts the filters of the current frame on a goroutine when
// its single tile is about to decode, recording the pipe in d.fs.
func (d *Decoder) startPipe() {
	f := d.fs
	h := f.hdr
	t := &h.tiles
	if d.noPipeline || workers() < 2 || t.cols*t.rows != 1 || h.UpscaledWidth != h.Width {
		return
	}
	p := &filterPipe{
		cdef: d.seq.EnableCDEF && !h.codedLossless && !h.allowIntraBC,
		lr:   h.usesLR,
		done: make(chan struct{}),
	}
	deblock := h.lf.level[0] != 0 || h.lf.level[1] != 0
	if !deblock && !p.cdef && !p.lr {
		return
	}
	p.cond = sync.NewCond(&p.mu)
	cur := f.cur
	if p.cdef {
		p.cdefF = d.allocLike(cur)
	}
	if p.lr {
		p.out = d.allocLike(cur)
	}
	f.pipe = p
	d.pipelined++
	go d.runPipe(f, p, deblock)
}

// allocLike returns a frame shaped like f with uninitialised buffers.
func (d *Decoder) allocLike(f *frame) *frame {
	g := *f
	for p := 0; p < f.numPlanes; p++ {
		if b := f.planes[p].pix16; b != nil {
			g.planes[p].pix16 = take(&d.free16, len(b))
		} else {
			g.planes[p].pix8 = take(&d.free8, len(f.planes[p].pix8))
		}
	}
	return &g
}

// publish records that the first rows luma rows are decoded.
func (p *filterPipe) publish(rows int) {
	p.mu.Lock()
	p.decoded = rows
	p.mu.Unlock()
	p.cond.Signal()
}

// wait blocks until at least rows luma rows are decoded; it returns
// false when the frame was abandoned.
func (p *filterPipe) wait(rows int) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	for p.decoded < rows && !p.aborted {
		p.cond.Wait()
	}
	return !p.aborted
}

// abort stops the filter goroutine of a frame that failed to decode and
// waits for it to exit.
func (p *filterPipe) abort() {
	p.mu.Lock()
	p.aborted = true
	p.mu.Unlock()
	p.cond.Signal()
	<-p.done
}

// finish waits for the filters, re-raising a panic of theirs.
func (p *filterPipe) finish() {
	<-p.done
	if p.failed != nil {
		panic(p.failed)
	}
}

func (d *Decoder) runPipe(f *frameState, p *filterPipe, deblock bool) {
	defer close(p.done)
	defer func() {
		if r := recover(); r != nil {
			p.failed = r
		}
	}()
	all := f.miRows * 4
	nb := (f.miRows + 15) / 16
	for k := 0; k < nb; k++ {
		if !p.wait(min(64*(k+1)+1, all)) {
			return
		}
		if deblock {
			d.deblockBand(f, k)
		}
		if k > 0 {
			d.pipeBand(f, p, k-1)
		}
	}
	d.pipeBand(f, p, nb-1)
	if p.lr {
		// Stripes are offset 8 rows up, so one may remain below the
		// last band.
		n := 0
		for pl := 0; pl < f.cur.numPlanes; pl++ {
			if f.hdr.lrType[pl] != restoreNone {
				n = max(n, lrStripes(f, pl))
			}
		}
		for s := nb; s < n; s++ {
			d.lrBand(f, p, s)
		}
	}
	// The rows below the last band (allocation padding) as in the
	// frame-wide path, which copies whole buffers.
	if p.cdef {
		copyRows(p.cdefF, f.cur, p.cdefRows, -1)
	}
	if p.lr {
		copyRows(p.out, p.cdefSource(f), p.outRows, -1)
	}
}

// cdefSource is the frame CDEF leaves its output in.
func (p *filterPipe) cdefSource(f *frameState) *frame {
	if p.cdef {
		return p.cdefF
	}
	return f.cur
}

// pipeBand runs CDEF on band k and restores stripe k.
func (d *Decoder) pipeBand(f *frameState, p *filterPipe, k int) {
	if p.cdef {
		end := 64 * (k + 1)
		copyRows(p.cdefF, f.cur, p.cdefRows, end)
		p.cdefRows = end
		d.cdefBand(f, f.cur, p.cdefF, k)
	}
	if p.lr {
		d.lrBand(f, p, k)
	}
}

// lrBand restores stripe s of every plane.
func (d *Decoder) lrBand(f *frameState, p *filterPipe, s int) {
	src := p.cdefSource(f)
	end := 64*(s+1) - 8
	copyRows(p.out, src, p.outRows, end)
	p.outRows = end
	parallelFor(f.cur.numPlanes, func(_, pl int) {
		if f.hdr.lrType[pl] != restoreNone && s < lrStripes(f, pl) {
			lrStripe(f, src, f.cur, p.out, pl, s, &p.states[pl])
		}
	})
}

// copyRows copies luma rows [from, to) of src into dst (to < 0: to the end
// of the buffers), with the matching chroma rows.
func copyRows(dst, src *frame, from, to int) {
	for pl := 0; pl < src.numPlanes; pl++ {
		ss := uint(min(pl, 1))
		sp, dp := &src.planes[pl], &dst.planes[pl]
		n := max(len(sp.pix8), len(sp.pix16))
		a, b := (from>>ss)*sp.stride, n
		if to >= 0 {
			b = min((to>>ss)*sp.stride, n)
		}
		if a >= b {
			continue
		}
		if sp.pix16 != nil {
			copy(dp.pix16[a:b], sp.pix16[a:b])
		} else {
			copy(dp.pix8[a:b], sp.pix8[a:b])
		}
	}
}

// finishPipe waits for the frame's filters and installs their output as
// the frame's samples, freeing the intermediate buffers.
func (d *Decoder) finishPipe(f *frameState) {
	p := f.pipe
	f.pipe = nil
	p.finish()
	cur := f.cur
	final := cur
	switch {
	case p.lr:
		final = p.out
	case p.cdef:
		final = p.cdefF
	}
	if final != cur {
		d.release(&cur.planes, cur.numPlanes)
		cur.planes = final.planes
	}
	if p.cdef && p.lr {
		d.release(&p.cdefF.planes, cur.numPlanes)
	}
}
