package h264

import (
	"sort"
	"sync"
)

// Plane is one 8-bit image plane.
type Plane struct {
	Pix    []byte
	Stride int
	W, H   int
}

// At returns the sample at (x, y).
func (p *Plane) At(x, y int) byte { return p.Pix[y*p.Stride+x] }

// Frame is a decoded picture in I420 layout, already cropped to the
// display size.
type Frame struct {
	Y, Cb, Cr     *Plane
	Width, Height int
	// POC is the picture order count (display order within a coded video
	// sequence).
	POC int
	// Tag is the value passed to SetTag before the picture's first slice
	// (e.g. a container timestamp), so callers can map output frames in
	// display order back to their source samples.
	Tag int64
	// KeyFrame reports an IDR picture.
	KeyFrame bool
	// FullRange and Matrix carry the VUI colour description (Matrix is the
	// H.273 matrix_coefficients code; 2 = unspecified).
	FullRange bool
	Matrix    int
}

const (
	refUnused = 0
	refShort  = 1
	refLong   = 2
)

// picture is a decoded picture with the side information later pictures
// need (motion for direct prediction, reference marking).
type picture struct {
	id              int32
	y, cb, cr       []byte
	stride, cstride int
	mbW, mbH        int

	poc          int
	frameNum     int
	frameNumWrap int
	longTermIdx  int
	refState     int
	needOutput   bool
	nonExisting  bool
	idr          bool
	mmco5        bool
	tag          int64

	// Per-macroblock type flags and per-4x4 motion, kept for co-located
	// (direct) prediction and deblocking.
	mbType []uint32
	mv     [2][]int16 // (x, y) pairs, quarter-sample units
	ref    [2][]int8  // reference index, -1 when the list is unused
	refID  [2][]int32 // id of the referenced picture, -1 when unused

	// prog tracks (possibly asynchronous) deblocking; copyPending marks a
	// picture queued for output but not yet copied out (see pipeline.go).
	prog        progress
	copyPending bool

	// Display description copied from the SPS.
	cropL, cropT, dispW, dispH int
	fullRange                  bool
	matrix                     int
}

func (p *picture) isRef() bool { return p.refState != refUnused }

// newPicture returns a picture sized for sps, reusing pooled storage.
func (d *Decoder) newPicture(sps *SPS) *picture {
	mbW, mbH := sps.PicWidthInMbs, sps.FrameHeightInMbs()
	var p *picture
	for i := len(d.pool) - 1; i >= 0; i-- {
		if c := d.pool[i]; c.mbW == mbW && c.mbH == mbH {
			p = c
			d.pool = append(d.pool[:i], d.pool[i+1:]...)
			p.prog.wait()
			break
		}
	}
	if p == nil {
		w, h := mbW*16, mbH*16
		nb := mbW * mbH * 16
		p = &picture{
			y: make([]byte, w*h), cb: make([]byte, w*h/4), cr: make([]byte, w*h/4),
			stride: w, cstride: w / 2, mbW: mbW, mbH: mbH,
			mbType: make([]uint32, mbW*mbH),
		}
		for l := 0; l < 2; l++ {
			p.mv[l] = make([]int16, nb*2)
			p.ref[l] = make([]int8, nb)
			p.refID[l] = make([]int32, nb)
		}
	}
	d.nextID++
	p.id = d.nextID
	p.poc, p.frameNum, p.frameNumWrap, p.longTermIdx = 0, 0, 0, 0
	p.refState = refUnused
	p.needOutput, p.nonExisting, p.idr, p.mmco5 = false, false, false, false
	p.tag = 0
	p.prog.complete(0)
	p.copyPending = false
	p.cropL, p.cropT, p.dispW, p.dispH = sps.CropLeft, sps.CropTop, sps.Width, sps.Height
	p.fullRange = sps.FullRange
	p.matrix = sps.MatrixCoefficients
	return p
}

// recycle returns p to the pool when nothing references it any more.
func (d *Decoder) recycle(p *picture) {
	if p == nil || p.isRef() || p.needOutput || p.copyPending || p == d.cur {
		return
	}
	for _, q := range d.dpb {
		if q == p {
			return
		}
	}
	if len(d.pool) < 4 {
		d.pool = append(d.pool, p)
	}
}

// pruneDPB drops pictures that are neither references nor awaiting output.
func (d *Decoder) pruneDPB() {
	out := d.dpb[:0]
	for _, p := range d.dpb {
		if p.isRef() || p.needOutput {
			out = append(out, p)
		} else if !p.copyPending && len(d.pool) < 4 {
			d.pool = append(d.pool, p)
		}
	}
	for i := len(out); i < len(d.dpb); i++ {
		d.dpb[i] = nil
	}
	d.dpb = out
}

// Output frames are recycled through a small free list: a consumer that is
// done with a Frame may hand it back with Recycle, which spares a 1080p
// stream ~3 MB of allocation (and the page faults behind it) per frame.
var framePool struct {
	sync.Mutex
	free []*Frame
}

const framePoolMax = 8

func getFrame(w, h int) *Frame {
	framePool.Lock()
	for i := len(framePool.free) - 1; i >= 0; i-- {
		f := framePool.free[i]
		if f.Width == w && f.Height == h {
			framePool.free = append(framePool.free[:i], framePool.free[i+1:]...)
			framePool.Unlock()
			return f
		}
	}
	framePool.Unlock()
	cw, ch := (w+1)/2, (h+1)/2
	return &Frame{
		Y:     &Plane{Pix: make([]byte, w*h), Stride: w, W: w, H: h},
		Cb:    &Plane{Pix: make([]byte, cw*ch), Stride: cw, W: cw, H: ch},
		Cr:    &Plane{Pix: make([]byte, cw*ch), Stride: cw, W: cw, H: ch},
		Width: w, Height: h,
	}
}

// Recycle returns a Frame obtained from a Decoder for reuse by later
// output. The caller must not touch f (or its planes) afterwards. Calling
// it is optional.
func Recycle(f *Frame) {
	if f == nil || f.Y == nil {
		return
	}
	framePool.Lock()
	if len(framePool.free) < framePoolMax {
		framePool.free = append(framePool.free, f)
	}
	framePool.Unlock()
}

// frameOf produces the cropped output copy of a picture.
func (p *picture) frame() *Frame {
	w, h := p.dispW, p.dispH
	cw, ch := (w+1)/2, (h+1)/2
	f := getFrame(w, h)
	f.POC, f.Tag, f.KeyFrame = p.poc, p.tag, p.idr
	f.FullRange, f.Matrix = p.fullRange, p.matrix
	for y := 0; y < h; y++ {
		o := (y+p.cropT)*p.stride + p.cropL
		copy(f.Y.Pix[y*w:y*w+w], p.y[o:o+w])
	}
	cl, ct := p.cropL/2, p.cropT/2
	for y := 0; y < ch; y++ {
		o := (y+ct)*p.cstride + cl
		copy(f.Cb.Pix[y*cw:y*cw+cw], p.cb[o:o+cw])
		copy(f.Cr.Pix[y*cw:y*cw+cw], p.cr[o:o+cw])
	}
	return f
}

// ---------------------------------------------------------------------------
// Picture order count (spec 8.2.1, frames only).

func (d *Decoder) computePOC(h *sliceHeader) int {
	sps := h.sps
	maxFrameNum := 1 << uint(sps.Log2MaxFrameNum)
	switch sps.PicOrderCntType {
	case 0:
		if h.idr {
			d.prevPOCMsb, d.prevPOCLsb = 0, 0
		}
		maxLsb := 1 << uint(sps.Log2MaxPicOrderCntLsb)
		lsb := h.pocLsb
		msb := d.prevPOCMsb
		switch {
		case lsb < d.prevPOCLsb && d.prevPOCLsb-lsb >= maxLsb/2:
			msb += maxLsb
		case lsb > d.prevPOCLsb && lsb-d.prevPOCLsb > maxLsb/2:
			msb -= maxLsb
		}
		d.curPOCMsb = msb
		top := msb + lsb
		d.curTopPOC = top
		bottom := top + h.deltaPOCBottom
		return min(top, bottom)
	default:
		var off int
		switch {
		case h.idr:
			off = 0
		case d.prevFrameNum > h.frameNum:
			off = d.prevFrameNumOffset + maxFrameNum
		default:
			off = d.prevFrameNumOffset
		}
		d.curFrameNumOffset = off
		if sps.PicOrderCntType == 2 {
			if h.idr {
				return 0
			}
			if h.nalRefIDC == 0 {
				return 2*(off+h.frameNum) - 1
			}
			return 2 * (off + h.frameNum)
		}
		n := len(sps.OffsetForRefFrame)
		abs := 0
		if n != 0 {
			abs = off + h.frameNum
		}
		if h.nalRefIDC == 0 && abs > 0 {
			abs--
		}
		expected := 0
		if abs > 0 {
			delta := 0
			for _, o := range sps.OffsetForRefFrame {
				delta += o
			}
			cycle, inCycle := (abs-1)/n, (abs-1)%n
			expected = cycle * delta
			for i := 0; i <= inCycle; i++ {
				expected += sps.OffsetForRefFrame[i]
			}
		}
		if h.nalRefIDC == 0 {
			expected += sps.OffsetForNonRefPic
		}
		top := expected + h.deltaPOC[0]
		bottom := top + sps.OffsetForTopToBottom + h.deltaPOC[1]
		d.curTopPOC = top
		return min(top, bottom)
	}
}

// ---------------------------------------------------------------------------
// Reference picture marking (spec 8.2.5).

func (d *Decoder) refCounts() (short, long int) {
	for _, p := range d.dpb {
		switch p.refState {
		case refShort:
			short++
		case refLong:
			long++
		}
	}
	return
}

func (d *Decoder) updateFrameNumWrap(frameNum int, sps *SPS) {
	maxFrameNum := 1 << uint(sps.Log2MaxFrameNum)
	for _, p := range d.dpb {
		if p.refState == refShort {
			p.frameNumWrap = p.frameNum
			if p.frameNum > frameNum {
				p.frameNumWrap -= maxFrameNum
			}
		}
	}
}

func (d *Decoder) slidingWindow(sps *SPS) {
	maxRefs := max(sps.MaxNumRefFrames, 1)
	for {
		s, l := d.refCounts()
		if s+l < maxRefs || s == 0 {
			return
		}
		var victim *picture
		for _, p := range d.dpb {
			if p.refState == refShort && (victim == nil || p.frameNumWrap < victim.frameNumWrap) {
				victim = p
			}
		}
		victim.refState = refUnused
	}
}

// markReference performs the decoded reference picture marking for the
// just-decoded picture cur (with first slice header h).
func (d *Decoder) markReference(cur *picture, h *sliceHeader) {
	sps := h.sps
	if h.idr {
		for _, p := range d.dpb {
			p.refState = refUnused
		}
		if h.longTermReference {
			cur.refState = refLong
			cur.longTermIdx = 0
			d.maxLongTermIdx = 0
		} else {
			cur.refState = refShort
			d.maxLongTermIdx = -1
		}
		return
	}
	d.updateFrameNumWrap(h.frameNum, sps)
	curLong := false
	if h.adaptiveMarking {
		currPicNum := h.frameNum
		for _, op := range h.mmco {
			switch op.op {
			case 1:
				picNum := currPicNum - (op.diffPicNumsMinus1 + 1)
				for _, p := range d.dpb {
					if p.refState == refShort && p.frameNumWrap == picNum {
						p.refState = refUnused
					}
				}
			case 2:
				for _, p := range d.dpb {
					if p.refState == refLong && p.longTermIdx == op.longTermPicNum {
						p.refState = refUnused
					}
				}
			case 3:
				picNum := currPicNum - (op.diffPicNumsMinus1 + 1)
				for _, p := range d.dpb {
					if p.refState == refLong && p.longTermIdx == op.longTermFrameIdx {
						p.refState = refUnused
					}
				}
				for _, p := range d.dpb {
					if p.refState == refShort && p.frameNumWrap == picNum {
						p.refState = refLong
						p.longTermIdx = op.longTermFrameIdx
					}
				}
			case 4:
				d.maxLongTermIdx = op.maxLongTermIdxPlus - 1
				for _, p := range d.dpb {
					if p.refState == refLong && p.longTermIdx > d.maxLongTermIdx {
						p.refState = refUnused
					}
				}
			case 5:
				for _, p := range d.dpb {
					p.refState = refUnused
				}
				d.maxLongTermIdx = -1
				cur.mmco5 = true
			case 6:
				for _, p := range d.dpb {
					if p != cur && p.refState == refLong && p.longTermIdx == op.longTermFrameIdx {
						p.refState = refUnused
					}
				}
				cur.refState = refLong
				cur.longTermIdx = op.longTermFrameIdx
				curLong = true
			}
		}
	} else {
		d.slidingWindow(sps)
	}
	if !curLong {
		// Guard against streams that exceed max_num_ref_frames.
		d.slidingWindow(sps)
		cur.refState = refShort
	}
}

// ---------------------------------------------------------------------------
// Reference picture list construction (spec 8.2.4).

// buildRefLists initialises and modifies the reference lists for slice h of
// the current picture.
func (d *Decoder) buildRefLists(h *sliceHeader, curPOC int) ([2][]*picture, error) {
	var lists [2][]*picture
	if h.isI() {
		return lists, nil
	}
	sps := h.sps
	d.updateFrameNumWrap(h.frameNum, sps)
	var short, long []*picture
	for _, p := range d.dpb {
		if p == d.cur {
			continue
		}
		switch p.refState {
		case refShort:
			short = append(short, p)
		case refLong:
			long = append(long, p)
		}
	}
	sort.SliceStable(long, func(i, j int) bool { return long[i].longTermIdx < long[j].longTermIdx })
	if h.isP() {
		sort.SliceStable(short, func(i, j int) bool { return short[i].frameNumWrap > short[j].frameNumWrap })
		lists[0] = append(append(lists[0], short...), long...)
	} else {
		var before, after []*picture
		for _, p := range short {
			if p.poc < curPOC {
				before = append(before, p)
			} else {
				after = append(after, p)
			}
		}
		sort.SliceStable(before, func(i, j int) bool { return before[i].poc > before[j].poc })
		sort.SliceStable(after, func(i, j int) bool { return after[i].poc < after[j].poc })
		lists[0] = append(append(append(lists[0], before...), after...), long...)
		lists[1] = append(append(append(lists[1], after...), before...), long...)
		if len(lists[1]) > 1 {
			same := len(lists[0]) == len(lists[1])
			for i := 0; same && i < len(lists[0]); i++ {
				same = lists[0][i] == lists[1][i]
			}
			if same {
				lists[1][0], lists[1][1] = lists[1][1], lists[1][0]
			}
		}
	}
	maxFrameNum := 1 << uint(sps.Log2MaxFrameNum)
	for l := 0; l < 2; l++ {
		n := h.numRefIdx[l]
		if n == 0 {
			lists[l] = nil
			continue
		}
		if len(lists[l]) > n {
			lists[l] = lists[l][:n]
		}
		// Modification (8.2.4.3).
		if len(h.refMod[l]) > 0 {
			list := make([]*picture, n+1)
			copy(list, lists[l])
			picNumPred := h.frameNum
			for idx, op := range h.refMod[l] {
				if idx >= n {
					break
				}
				var pic *picture
				if op.idc < 2 {
					abs := op.val + 1
					var noWrap int
					if op.idc == 0 {
						noWrap = picNumPred - abs
						if noWrap < 0 {
							noWrap += maxFrameNum
						}
					} else {
						noWrap = picNumPred + abs
						if noWrap >= maxFrameNum {
							noWrap -= maxFrameNum
						}
					}
					picNumPred = noWrap
					picNum := noWrap
					if picNum > h.frameNum {
						picNum -= maxFrameNum
					}
					for _, p := range short {
						if p.frameNumWrap == picNum {
							pic = p
						}
					}
				} else {
					for _, p := range long {
						if p.longTermIdx == op.val {
							pic = p
						}
					}
				}
				if pic == nil {
					// The modification names a picture that is not in the
					// DPB. Spec 8.2.4.3.1 treats this as an error, but real
					// streams (and the conformance error-concealment set)
					// reach it after a lost reference or a frame_num reset
					// without an IDR. Conceal: keep the reference already at
					// this position rather than failing the whole decode.
					d.concealedRefs++
					continue
				}
				copy(list[idx+1:], list[idx:n])
				list[idx] = pic
				w := idx + 1
				for r := idx + 1; r <= n; r++ {
					if list[r] != pic {
						list[w] = list[r]
						w++
					}
				}
				for ; w <= n; w++ {
					list[w] = nil
				}
			}
			lists[l] = list[:n]
		}
		// Fill holes (corrupt or truncated lists) so prediction never sees nil.
		full := lists[l]
		if len(full) < n {
			full = append(full, make([]*picture, n-len(full))...)
		}
		var fill *picture
		for _, p := range full {
			if p != nil {
				fill = p
				break
			}
		}
		if fill == nil {
			fill = d.fallbackRef()
			if fill == nil {
				return lists, errf("h264: inter slice without reference pictures")
			}
		}
		for i := range full {
			if full[i] == nil {
				full[i] = fill
			}
		}
		lists[l] = full
	}
	return lists, nil
}

// fallbackRef returns any decoded picture usable as a stand-in reference.
func (d *Decoder) fallbackRef() *picture {
	var best *picture
	for _, p := range d.dpb {
		if p != d.cur && (best == nil || p.id > best.id) {
			best = p
		}
	}
	return best
}
