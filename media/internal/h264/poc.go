package h264

// Picture order count (spec 8.2.1), decoded picture buffer and reference list
// construction (spec 8.2.4) for P and B slices.

// refPicture is a decoded reference picture in the DPB. The per-4x4 motion
// arrays are retained so B-slice direct mode can read the co-located picture.
type refPicture struct {
	frame    *Frame
	poc      int
	frameNum uint32

	mvLX, mvLY, refLX    []int
	mvL1X, mvL1Y, refL1X []int
}

// computePOC returns the picture order count of the current picture and
// updates the decoder's POC state. isRef reports whether the picture is used
// for reference (which is what advances the type-0/2 state).
func (d *Decoder) computePOC(sps *SPS, hdr *SliceHeader, isIDR, isRef bool) int {
	switch sps.PicOrderCntType {
	case 0:
		maxLsb := 1 << uint(sps.Log2MaxPicOrderCntLsb)
		if isIDR {
			d.prevPOCMsb, d.prevPOCLsb = 0, 0
		}
		lsb := int(hdr.PicOrderCntLsb)
		switch {
		case lsb < d.prevPOCLsb && d.prevPOCLsb-lsb >= maxLsb/2:
			d.prevPOCMsb += maxLsb
		case lsb > d.prevPOCLsb && lsb-d.prevPOCLsb > maxLsb/2:
			d.prevPOCMsb -= maxLsb
		}
		poc := d.prevPOCMsb + lsb
		if isRef {
			d.prevPOCLsb = lsb
		}
		return poc

	case 1:
		maxFrameNum := 1 << uint(sps.Log2MaxFrameNum)
		var frameNumOffset int
		if isIDR {
			frameNumOffset = 0
		} else if d.prevFrameNum > int(hdr.FrameNum) {
			frameNumOffset = d.prevFrameNumOffset + maxFrameNum
		} else {
			frameNumOffset = d.prevFrameNumOffset
		}
		absFrameNum := 0
		if sps.NumRefFramesInPOCCycle != 0 {
			absFrameNum = frameNumOffset + int(hdr.FrameNum)
		}
		if !isRef && absFrameNum > 0 {
			absFrameNum--
		}
		var expected int
		if absFrameNum > 0 {
			cycleCnt := (absFrameNum - 1) / sps.NumRefFramesInPOCCycle
			var cycleDelta int
			for _, o := range sps.OffsetForRefFrame {
				cycleDelta += int(o)
			}
			expected = cycleCnt * cycleDelta
			inCycle := (absFrameNum - 1) % sps.NumRefFramesInPOCCycle
			for i := 0; i <= inCycle && i < len(sps.OffsetForRefFrame); i++ {
				expected += int(sps.OffsetForRefFrame[i])
			}
		}
		if !isRef {
			expected += int(sps.OffsetForNonRefPic)
		}
		top := expected + int(hdr.DeltaPicOrderCnt[0])
		bottom := top
		if !hdr.FieldPic {
			bottom = top + int(sps.OffsetForTopToBottom) + int(hdr.DeltaPicOrderCnt[1])
		}
		if top < bottom {
			return top
		}
		return bottom

	default: // type 2
		maxFrameNum := 1 << uint(sps.Log2MaxFrameNum)
		var frameNumOffset int
		if isIDR {
			frameNumOffset = 0
		} else if d.prevFrameNum > int(hdr.FrameNum) {
			frameNumOffset = d.prevFrameNumOffset + maxFrameNum
		} else {
			frameNumOffset = d.prevFrameNumOffset
		}
		poc := 0
		if !isIDR {
			poc = 2 * (frameNumOffset + int(hdr.FrameNum))
		}
		if isRef {
			d.prevFrameNum = int(hdr.FrameNum)
			d.prevFrameNumOffset = frameNumOffset
		}
		return poc
	}
}

// buildRefLists builds the default reference lists L0 and L1 for a picture with
// order count curPOC. Ref-picture-list reordering is not applied.
func (d *Decoder) buildRefLists(curPOC int) (l0, l1 []*refPicture) {
	// L0: past (poc < cur) in descending POC, then future (poc > cur) ascending.
	for i := len(d.dpb) - 1; i >= 0; i-- {
		if d.dpb[i].poc < curPOC {
			l0 = append(l0, d.dpb[i])
		}
	}
	for i := 0; i < len(d.dpb); i++ {
		if d.dpb[i].poc > curPOC {
			l0 = append(l0, d.dpb[i])
		}
	}
	// L1: future ascending, then past descending.
	for i := 0; i < len(d.dpb); i++ {
		if d.dpb[i].poc > curPOC {
			l1 = append(l1, d.dpb[i])
		}
	}
	for i := len(d.dpb) - 1; i >= 0; i-- {
		if d.dpb[i].poc < curPOC {
			l1 = append(l1, d.dpb[i])
		}
	}
	return l0, l1
}

// addReference inserts a reference picture and evicts the oldest when the DPB
// exceeds the SPS limit (sliding-window marking).
func (d *Decoder) addReference(p *refPicture, maxRefs int) {
	d.dpb = append(d.dpb, p)
	if maxRefs < 1 {
		maxRefs = 1
	}
	if len(d.dpb) > maxRefs {
		d.dpb = d.dpb[len(d.dpb)-maxRefs:]
	}
}
