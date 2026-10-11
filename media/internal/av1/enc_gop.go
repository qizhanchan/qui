package av1

import (
	"errors"
	"image"
	"sort"
)

// Frame scheduling: key frames every KeyInt frames, mini-GOPs with a
// hidden ALTREF, and the choice of reference slots.

// encJob is one frame to code.
type encJob struct {
	img     *image.YCbCr
	pts     int64 // display index
	key     bool
	hidden  bool // ALTREF: coded now, shown by show_existing_frame
	refIdx  [refsPerFrame]int
	refresh int   // slots the frame writes
	active  []int // reference frames searched
	future  bool  // a reference follows the frame in display order
}

// Encode queues one frame (img must be at least Width x Height, I420; it
// is copied) and returns the temporal units that are ready, in display
// order.
func (e *Encoder) Encode(img *image.YCbCr) ([]Packet, error) {
	if img.Rect.Dx() < e.cfg.Width || img.Rect.Dy() < e.cfg.Height {
		return nil, errors.New("av1: image smaller than the configured size")
	}
	c := image.NewYCbCr(image.Rect(0, 0, e.cfg.Width, e.cfg.Height), image.YCbCrSubsampleRatio420)
	for y := 0; y < e.cfg.Height; y++ {
		copy(c.Y[y*c.YStride:y*c.YStride+e.cfg.Width], img.Y[img.YOffset(img.Rect.Min.X, img.Rect.Min.Y+y):])
	}
	for y := 0; y < e.cfg.Height/2; y++ {
		o := img.COffset(img.Rect.Min.X, img.Rect.Min.Y+2*y)
		copy(c.Cb[y*c.CStride:y*c.CStride+e.cfg.Width/2], img.Cb[o:])
		copy(c.Cr[y*c.CStride:y*c.CStride+e.cfg.Width/2], img.Cr[o:])
	}
	e.queue = append(e.queue, c)
	e.inCount++
	return e.drain(false)
}

// Flush codes the frames still queued.
func (e *Encoder) Flush() ([]Packet, error) { return e.drain(true) }

// Delay is the number of frames Encode holds back (MiniGOP - 1).
func (e *Encoder) Delay() int { return e.cfg.MiniGOP - 1 }

func (e *Encoder) isKey(pts int64) bool { return pts%int64(e.cfg.KeyInt) == 0 }

// drain codes every complete group in the queue (all of it when flush).
func (e *Encoder) drain(flush bool) ([]Packet, error) {
	var out []Packet
	for len(e.queue) > 0 {
		pts := e.inCount - int64(len(e.queue))
		if e.isKey(pts) {
			p, err := e.codeShown(&encJob{img: e.queue[0], pts: pts, key: true, refresh: 0xff})
			if err != nil {
				return out, err
			}
			out = append(out, p)
			e.queue = e.queue[1:]
			continue
		}
		n := 1
		for n < len(e.queue) && n < e.cfg.MiniGOP && !e.isKey(pts+int64(n)) {
			n++
		}
		complete := n == e.cfg.MiniGOP || n < len(e.queue) || flush
		if !complete {
			break
		}
		ps, err := e.codeGroup(e.queue[:n], pts)
		out = append(out, ps...)
		if err != nil {
			return out, err
		}
		e.queue = e.queue[n:]
	}
	if len(e.queue) == 0 {
		e.queue = e.queue[:0]
	}
	return out, nil
}

// codeGroup codes frames pts..pts+len(g)-1: with two or more, the last
// as a hidden ALTREF first.
func (e *Encoder) codeGroup(g []*image.YCbCr, pts int64) ([]Packet, error) {
	n := len(g)
	var out []Packet
	if n > 1 {
		arf := &encJob{img: g[n-1], pts: pts + int64(n-1), hidden: true}
		e.planRefs(arf)
		arf.refresh = 1 << uint(e.oldestSlot(6, 8))
		data, err := e.codeJob(arf)
		if err != nil {
			return nil, err
		}
		e.pending = data
		g = g[:n-1]
	}
	for i, img := range g {
		job := &encJob{img: img, pts: pts + int64(i)}
		e.planRefs(job)
		job.refresh = 1 << uint(e.oldestSlot(0, e.cfg.Refs))
		p, err := e.codeShown(job)
		if err != nil {
			return out, err
		}
		out = append(out, p)
	}
	if n > 1 {
		p, err := e.showExisting(pts + int64(n-1))
		if err != nil {
			return out, err
		}
		out = append(out, p)
	}
	return out, nil
}

// oldestSlot is the slot in [lo, hi) holding the oldest frame.
func (e *Encoder) oldestSlot(lo, hi int) int {
	best := lo
	for s := lo; s < hi; s++ {
		if e.slotPTS[s] < e.slotPTS[best] {
			best = s
		}
	}
	return best
}

// planRefs points the job's references at the slots: LAST, LAST2, ...
// at the latest past frames, the backward references at the nearest
// future one (if any).
func (e *Encoder) planRefs(job *encJob) {
	type sp struct {
		slot int
		pts  int64
	}
	var past, future []sp
	seen := map[int64]bool{}
	for s, p := range e.slotPTS {
		if p < 0 || seen[p] || p == job.pts {
			continue
		}
		seen[p] = true
		if p < job.pts {
			past = append(past, sp{s, p})
		} else {
			future = append(future, sp{s, p})
		}
	}
	sort.Slice(past, func(i, j int) bool { return past[i].pts > past[j].pts })
	sort.Slice(future, func(i, j int) bool { return future[i].pts < future[j].pts })
	np := min(len(past), e.cfg.Refs)
	for i := range job.refIdx {
		job.refIdx[i] = past[0].slot
	}
	job.active = job.active[:0]
	for j := 0; j < min(np, 3); j++ {
		job.refIdx[j] = past[j].slot
		job.active = append(job.active, lastFrame+j)
	}
	if len(future) > 0 {
		job.future = true
		for _, r := range []int{bwdrefFrame, altref2Frame, altrefFrame} {
			job.refIdx[r-lastFrame] = future[0].slot
		}
		job.active = append(job.active, altrefFrame)
	}
}

// codeShown codes a shown frame and returns its temporal unit (with any
// pending hidden frame first).
func (e *Encoder) codeShown(job *encJob) (Packet, error) {
	data, err := e.codeJob(job)
	if err != nil {
		return Packet{}, err
	}
	if e.pending != nil {
		data = append(e.pending, data...)
		e.pending = nil
	}
	typ := byte('P')
	switch {
	case job.key:
		typ = 'K'
	case job.future:
		typ = 'B'
	}
	return Packet{Data: data, Key: job.key, PTS: job.pts, Type: typ, QIndex: e.d.hdr.baseQIdx}, nil
}

// codeJob codes one frame with rate control (re-coding a frame that
// would overflow the VBV) and commits it.
func (e *Encoder) codeJob(job *encJob) ([]byte, error) {
	e.frameCount = job.pts
	if job.key {
		e.keyFrame = job.pts
	}
	e.loadSource(job.img)
	q := e.rc.frameQIndex(job.key, e)
	if job.hidden {
		q = qindexOf(qscaleOf(q) / 1.25)
	}
	data, err := e.encodeFrame(job, q)
	for tries := 0; err == nil && e.rc.vbvOverflow(len(data)*8) && q < 255 && tries < 4; tries++ {
		e.d.seenFrameHeader = false
		q = qindexOf(qscaleOf(q) * 1.5)
		data, err = e.encodeFrame(job, q)
	}
	if err != nil {
		return nil, err
	}
	if err := e.finishFrame(); err != nil {
		return nil, err
	}
	for s := 0; s < numRefFrames; s++ {
		if job.refresh>>uint(s)&1 != 0 {
			e.slotPTS[s] = job.pts
		}
	}
	e.rc.update(job.key, q, len(data)*8, e)
	return data, nil
}

// showExisting shows the hidden frame of display index pts.
func (e *Encoder) showExisting(pts int64) (Packet, error) {
	slot := -1
	for s, p := range e.slotPTS {
		if p == pts {
			slot = s
		}
	}
	if slot < 0 {
		return Packet{}, errors.New("av1: hidden frame lost")
	}
	fh := encFrameHdr{showExisting: true, frameToShow: slot}
	var bw bitWriter
	fh.write(e.d, &bw)
	bw.trailing()
	data := appendOBU(nil, obuFrameHeader, bw.buf)
	if err := e.d.DecodeTemporalUnit(data); err != nil {
		return Packet{}, err
	}
	e.collectRecon()
	return Packet{Data: data, PTS: pts, Type: 'A', QIndex: e.d.hdr.baseQIdx}, nil
}

// frameHeader returns the header fields of the job's frame.
func (e *Encoder) frameHeader(job *encJob, q int) encFrameHdr {
	fh := encFrameHdr{
		frameType: keyFrame, showFrame: true,
		orderHint:   int(job.pts) & (1<<uint(e.seq.orderHintBits) - 1),
		baseQ:       q,
		cdefDamping: 3,
		txSelect:    true,
	}
	e.activeRefs = append(e.activeRefs[:0], job.active...)
	if e.lrWiener || len(e.sgrSets) > 0 {
		fh.lrType = [3]int{restoreSwitchable, restoreSwitchable, restoreSwitchable}
		fh.lrUnitShift = lrUnitShift(e.cfg.Width, e.cfg.Height)
	}
	if !job.key {
		fh.frameType = interFrame
		fh.showFrame = !job.hidden
		fh.showable = job.hidden
		fh.primaryRef = 0
		fh.refresh = job.refresh
		fh.refIdx = job.refIdx
		fh.interp = filterSwitchable
		fh.allowHP = fh.baseQ < 128
		fh.useRefMVs = e.seq.refFrameMVs
		if job.future {
			fh.refSelect = true
			tmp := FrameHeader{referenceSelect: true, OrderHint: fh.orderHint}
			for i := range tmp.refFrameIdx {
				tmp.refFrameIdx[i] = fh.refIdx[i]
			}
			fh.skipModeAllowd = e.d.skipModeAllowed(&tmp)
			fh.skipMode = fh.skipModeAllowd
		}
	}
	return fh
}
