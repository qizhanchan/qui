package av1

// Motion field estimation (spec 7.9) and storage (7.19).

// tplMv is one projected motion field entry: the source motion vector
// and the distance it spans.
type tplMv struct {
	mv        mv
	refOffset int
}

var divMult = [32]int32{0, 16384, 8192, 5461, 4096, 3276, 2730, 2340, 2048, 1820, 1638, 1489, 1365, 1260, 1170, 1092, 1024, 963, 910, 862, 819, 780, 744, 712, 682, 655, 630, 606, 585, 564, 546, 528}

// mvProjection is get_mv_projection.
func mvProjection(m mv, num, den int) mv {
	den = min(den, 31)
	num = clip3(-31, 31, num)
	f := func(v int16) int16 {
		x := round2Signed(int64(v)*int64(num)*int64(divMult[den]), 14)
		return int16(clip3(-(1<<14)+1, (1<<14)-1, int(x)))
	}
	return mv{f(m.row), f(m.col)}
}

// projectTpl projects a motion field entry onto reference ref.
func (f *frameState) projectTpl(e *tplMv, ref int) mv {
	h := f.hdr
	cur := f.seq.relDist(h.OrderHint, h.orderHints[ref])
	return mvProjection(e.mv, cur, e.refOffset)
}

// saveMotionField records the frame's motion vectors per 8x8 for later
// frames' temporal prediction.
func (f *frameState) saveMotionField() {
	h := f.hdr
	s := f.seq
	w8, h8 := f.miCols>>1, f.miRows>>1
	cur := f.cur
	cur.mfRef = make([]int8, w8*h8)
	cur.mfMv = make([]mv, w8*h8)
	for y := 0; y < h8; y++ {
		for x := 0; x < w8; x++ {
			m := &f.mi[(2*y+1)*f.miCols+2*x+1]
			ref := int8(-1)
			var v mv
			for l := 0; l < 2; l++ {
				r := int(m.refFrame[l])
				if r <= intraFrame {
					continue
				}
				if s.relDist(h.orderHints[r], h.OrderHint) >= 0 {
					continue
				}
				if iabs(int(m.mv[l].row)) > 4095 || iabs(int(m.mv[l].col)) > 4095 {
					continue
				}
				ref, v = int8(r), m.mv[l]
			}
			cur.mfRef[y*w8+x] = ref
			cur.mfMv[y*w8+x] = v
		}
	}
}

// motionFieldEstimation fills f.motionField (spec 7.9, as libaom does it).
func (d *Decoder) motionFieldEstimation(f *frameState) {
	h := f.hdr
	w8, h8 := f.miCols>>1, f.miRows>>1
	f.motionField = make([]tplMv, w8*h8)
	for i := range f.motionField {
		f.motionField[i].mv.row = invalidMv
	}
	stamp := 2
	if last := &d.ref[h.refFrameIdx[0]]; last.pic != nil {
		altOfLast := last.savedOrderHint[altrefFrame]
		if altOfLast != h.orderHints[goldenFrame] {
			d.project(f, lastFrame, 2)
		}
		stamp--
	}
	s := d.seq
	if s.relDist(h.orderHints[bwdrefFrame], h.OrderHint) > 0 && d.project(f, bwdrefFrame, 0) {
		stamp--
	}
	if s.relDist(h.orderHints[altref2Frame], h.OrderHint) > 0 && d.project(f, altref2Frame, 0) {
		stamp--
	}
	if s.relDist(h.orderHints[altrefFrame], h.OrderHint) > 0 && stamp >= 0 && d.project(f, altrefFrame, 0) {
		stamp--
	}
	if stamp >= 0 {
		d.project(f, last2Frame, 2)
	}
}

func (d *Decoder) project(f *frameState, src, dir int) bool {
	h := f.hdr
	s := d.seq
	slot := &d.ref[h.refFrameIdx[src-lastFrame]]
	p := slot.pic
	if p == nil || p.mfRef == nil || slot.frameType == keyFrame || slot.frameType == intraOnlyFrame ||
		slot.miRows != f.miRows || slot.miCols != f.miCols {
		return false
	}
	var refOffset [numRefFrames]int
	for r := lastFrame; r <= altrefFrame; r++ {
		refOffset[r] = s.relDist(slot.orderHint, slot.savedOrderHint[r])
	}
	startToCur := s.relDist(slot.orderHint, h.OrderHint)
	if dir == 2 {
		startToCur = -startToCur
	}
	w8, h8 := f.miCols>>1, f.miRows>>1
	for y := 0; y < h8; y++ {
		for x := 0; x < w8; x++ {
			r := int(p.mfRef[y*w8+x])
			if r <= intraFrame {
				continue
			}
			off := refOffset[r]
			if !(iabs(off) <= 31 && off > 0 && iabs(startToCur) <= 31) {
				continue
			}
			fwd := p.mfMv[y*w8+x]
			pm := mvProjection(fwd, startToCur, off)
			// get_block_position
			rowOff := int(pm.row) >> 6
			if pm.row < 0 {
				rowOff = -(int(-pm.row) >> 6)
			}
			colOff := int(pm.col) >> 6
			if pm.col < 0 {
				colOff = -(int(-pm.col) >> 6)
			}
			ry, cx := y+rowOff, x+colOff
			if dir == 2 {
				ry, cx = y-rowOff, x-colOff
			}
			baseY, baseX := y>>3<<3, x>>3<<3
			if ry < 0 || ry >= h8 || cx < 0 || cx >= w8 || ry < baseY || ry >= baseY+8 || cx < baseX-8 || cx >= baseX+16 {
				continue
			}
			f.motionField[ry*w8+cx] = tplMv{mv: fwd, refOffset: off}
		}
	}
	return true
}
