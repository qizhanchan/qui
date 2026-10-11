package h264

// Slice types (slice_type % 5).
const (
	sliceP  = 0
	sliceB  = 1
	sliceI  = 2
	sliceSP = 3
	sliceSI = 4
)

type refModOp struct {
	idc int // 0: subtract, 1: add, 2: long-term
	val int // abs_diff_pic_num_minus1 or long_term_pic_num
}

type mmcoOp struct {
	op                 int
	diffPicNumsMinus1  int
	longTermPicNum     int
	longTermFrameIdx   int
	maxLongTermIdxPlus int
}

// sliceHeader is a parsed slice header (spec 7.3.3).
type sliceHeader struct {
	nalType   int
	nalRefIDC int
	idr       bool

	firstMB   int
	sliceType int // 0..4
	ppsID     int
	frameNum  int
	idrPicID  int

	pocLsb         int
	deltaPOCBottom int
	deltaPOC       [2]int

	redundantPicCnt int
	directSpatial   bool
	numRefIdx       [2]int

	refMod [2][]refModOp

	// Explicit weighted prediction tables (pred_weight_table).
	hasWeights     bool
	lumaLog2WD     int
	chromaLog2WD   int
	lumaWeight     [2][32]int
	lumaOffset     [2][32]int
	chromaWeight   [2][32][2]int
	chromaOffset   [2][32][2]int
	lumaWeightFlag [2][32]bool
	chromaWFlag    [2][32]bool

	noOutputOfPriorPics bool
	longTermReference   bool
	adaptiveMarking     bool
	mmco                []mmcoOp

	cabacInitIDC   int
	qpDelta        int
	disableDeblock int
	alphaOffset    int // slice_alpha_c0_offset_div2 * 2
	betaOffset     int

	sps *SPS
	pps *PPS
}

func (h *sliceHeader) isI() bool { return h.sliceType == sliceI || h.sliceType == sliceSI }
func (h *sliceHeader) isB() bool { return h.sliceType == sliceB }
func (h *sliceHeader) isP() bool { return h.sliceType == sliceP || h.sliceType == sliceSP }

// parseSliceHeader parses the slice header; r is left at slice_data().
func parseSliceHeader(r *bitReader, nal byte, spsTab []*SPS, ppsTab []*PPS) (*sliceHeader, error) {
	h := &sliceHeader{nalType: nalType(nal), nalRefIDC: nalRefIDC(nal)}
	h.idr = h.nalType == nalIDR
	h.firstMB = int(r.ue())
	st := int(r.ue())
	if st > 9 {
		return nil, errf("h264: bad slice_type")
	}
	h.sliceType = st % 5
	h.ppsID = int(r.ue())
	if h.ppsID > 255 || ppsTab[h.ppsID] == nil {
		return nil, errf("h264: slice references unknown PPS")
	}
	pps := ppsTab[h.ppsID]
	sps := spsTab[pps.SPSID]
	if sps == nil {
		return nil, errf("h264: slice references unknown SPS")
	}
	h.sps, h.pps = sps, pps
	if !sps.FrameMbsOnly || sps.ChromaFormatIDC != 1 || sps.BitDepthLuma != 8 || sps.BitDepthChroma != 8 {
		return nil, ErrUnsupported
	}
	if h.sliceType == sliceSP || h.sliceType == sliceSI {
		return nil, ErrUnsupported
	}
	h.frameNum = int(r.u(sps.Log2MaxFrameNum))
	if h.idr {
		h.idrPicID = int(r.ue())
	}
	switch sps.PicOrderCntType {
	case 0:
		h.pocLsb = int(r.u(sps.Log2MaxPicOrderCntLsb))
		if pps.BottomFieldPicOrder {
			h.deltaPOCBottom = int(r.se())
		}
	case 1:
		if !sps.DeltaPicOrderAlwaysZero {
			h.deltaPOC[0] = int(r.se())
			if pps.BottomFieldPicOrder {
				h.deltaPOC[1] = int(r.se())
			}
		}
	}
	if pps.RedundantPicCntPresent {
		h.redundantPicCnt = int(r.ue())
	}
	if h.isB() {
		h.directSpatial = r.u1()
	}
	h.numRefIdx = [2]int{pps.NumRefIdxL0Default, pps.NumRefIdxL1Default}
	if !h.isI() {
		if r.u1() { // num_ref_idx_active_override_flag
			h.numRefIdx[0] = int(r.ue()) + 1
			if h.isB() {
				h.numRefIdx[1] = int(r.ue()) + 1
			}
		}
		if h.numRefIdx[0] > 32 || h.numRefIdx[1] > 32 {
			return nil, errf("h264: num_ref_idx_active too large")
		}
	}
	if !h.isB() {
		h.numRefIdx[1] = 0
	}
	if h.isI() {
		h.numRefIdx[0] = 0
	}
	// ref_pic_list_modification()
	for list := 0; list < 2; list++ {
		if (list == 0 && h.isI()) || (list == 1 && !h.isB()) {
			continue
		}
		if !r.u1() {
			continue
		}
		for n := 0; ; n++ {
			idc := int(r.ue())
			if idc == 3 {
				break
			}
			if idc > 2 || n > 64 || r.err() != nil {
				return nil, errf("h264: bad ref_pic_list_modification")
			}
			h.refMod[list] = append(h.refMod[list], refModOp{idc: idc, val: int(r.ue())})
		}
	}
	if (pps.WeightedPred && h.isP()) || (pps.WeightedBipredIDC == 1 && h.isB()) {
		h.hasWeights = true
		h.lumaLog2WD = int(r.ue())
		h.chromaLog2WD = int(r.ue())
		if h.lumaLog2WD > 7 || h.chromaLog2WD > 7 {
			return nil, errf("h264: bad weight denominator")
		}
		lists := 1
		if h.isB() {
			lists = 2
		}
		for l := 0; l < lists; l++ {
			for i := 0; i < h.numRefIdx[l]; i++ {
				h.lumaWeight[l][i] = 1 << uint(h.lumaLog2WD)
				if r.u1() {
					h.lumaWeightFlag[l][i] = true
					h.lumaWeight[l][i] = int(r.se())
					h.lumaOffset[l][i] = int(r.se())
				}
				h.chromaWeight[l][i] = [2]int{1 << uint(h.chromaLog2WD), 1 << uint(h.chromaLog2WD)}
				if r.u1() {
					h.chromaWFlag[l][i] = true
					for c := 0; c < 2; c++ {
						h.chromaWeight[l][i][c] = int(r.se())
						h.chromaOffset[l][i][c] = int(r.se())
					}
				}
			}
		}
	}
	if h.nalRefIDC != 0 {
		if h.idr {
			h.noOutputOfPriorPics = r.u1()
			h.longTermReference = r.u1()
		} else if r.u1() {
			h.adaptiveMarking = true
			for n := 0; ; n++ {
				op := mmcoOp{op: int(r.ue())}
				if op.op == 0 {
					break
				}
				if op.op > 6 || n > 66 || r.err() != nil {
					return nil, errf("h264: bad memory_management_control_operation")
				}
				if op.op == 1 || op.op == 3 {
					op.diffPicNumsMinus1 = int(r.ue())
				}
				if op.op == 2 {
					op.longTermPicNum = int(r.ue())
				}
				if op.op == 3 || op.op == 6 {
					op.longTermFrameIdx = int(r.ue())
				}
				if op.op == 4 {
					op.maxLongTermIdxPlus = int(r.ue())
				}
				h.mmco = append(h.mmco, op)
			}
		}
	}
	if pps.EntropyCodingCABAC && !h.isI() {
		h.cabacInitIDC = int(r.ue())
		if h.cabacInitIDC > 2 {
			return nil, errf("h264: bad cabac_init_idc")
		}
	}
	h.qpDelta = int(r.se())
	if pps.DeblockingFilterCtl {
		h.disableDeblock = int(r.ue())
		if h.disableDeblock > 2 {
			return nil, errf("h264: bad disable_deblocking_filter_idc")
		}
		if h.disableDeblock != 1 {
			h.alphaOffset = int(r.se()) * 2
			h.betaOffset = int(r.se()) * 2
		}
	}
	if err := r.err(); err != nil {
		return nil, err
	}
	qp := pps.PicInitQP + h.qpDelta
	if qp < 0 || qp > 51 {
		return nil, errf("h264: bad slice QP")
	}
	if h.firstMB >= sps.PicWidthInMbs*sps.FrameHeightInMbs() {
		return nil, errf("h264: first_mb_in_slice out of range")
	}
	return h, nil
}
