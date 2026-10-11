package h264

// SliceHeader is a parsed slice header (spec 7.3.3). Fields that only
// matter to inter prediction are parsed (so the reader lands exactly at
// the slice-data start) but not all are stored.
type SliceHeader struct {
	FirstMbInSlice uint32
	SliceType      uint32
	PPSID          uint32
	ColourPlaneID  uint32

	FrameNum    uint32
	FieldPic    bool
	BottomField bool
	IDRPicID    uint32

	PicOrderCntType        uint32
	PicOrderCntLsb         uint32
	DeltaPicOrderCntBottom int32
	DeltaPicOrderCnt       [2]int32

	RedundantPicCnt uint32

	DirectSpatialMV   bool
	NumRefIdxL0Active int
	NumRefIdxL1Active int

	CabacInitIDC               uint32
	SliceQPDelta               int32
	DisableDeblockingFilterIDC uint32
	SliceAlphaC0OffsetDiv2     int32
	SliceBetaOffsetDiv2        int32

	// Weighted prediction (pred_weight_table).
	LumaLog2WeightDenom   int
	ChromaLog2WeightDenom int
	LumaWeightL0          []int
	LumaOffsetL0          []int
	ChromaWeightL0        [][2]int
	ChromaOffsetL0        [][2]int
	LumaWeightL1          []int
	LumaOffsetL1          []int
	ChromaWeightL1        [][2]int
	ChromaOffsetL1        [][2]int
}

// Slice-type predicates (types 5..9 are the "all slices" range).
func sliceTypeIsP(t uint32) bool  { return t%5 == 0 }
func sliceTypeIsB(t uint32) bool  { return t%5 == 1 }
func sliceTypeIsI(t uint32) bool  { return t%5 == 2 }
func sliceTypeIsSP(t uint32) bool { return t%5 == 3 }
func sliceTypeIsSI(t uint32) bool { return t%5 == 4 }

// IsIntra reports whether the slice is intra-coded (I or SI).
func (h *SliceHeader) IsIntra() bool { return sliceTypeIsI(h.SliceType) || sliceTypeIsSI(h.SliceType) }

// parseSliceHeader reads a slice header from r (positioned after the NAL
// header, RBSP unescaped) and leaves r positioned at the start of slice
// data.
func parseSliceHeader(r *bitReader, sps *SPS, pps *PPS, nalRefIDC, nalTyp int) (*SliceHeader, error) {
	h := &SliceHeader{
		PicOrderCntType:   sps.PicOrderCntType,
		NumRefIdxL0Active: pps.NumRefIdxL0,
		NumRefIdxL1Active: pps.NumRefIdxL1,
	}
	h.FirstMbInSlice = r.ue()
	h.SliceType = r.ue()
	h.PPSID = r.ue()
	if sps.SeparateColourPlane {
		h.ColourPlaneID = r.u(2)
	}
	h.FrameNum = r.u(sps.Log2MaxFrameNum)
	if !sps.FrameMbsOnly {
		h.FieldPic = r.u1()
		if h.FieldPic {
			h.BottomField = r.u1()
		}
	}
	if nalTyp == nalIDR {
		h.IDRPicID = r.ue()
	}
	switch sps.PicOrderCntType {
	case 0:
		h.PicOrderCntLsb = r.u(sps.Log2MaxPicOrderCntLsb)
		if pps.BottomFieldPIC && !h.FieldPic {
			h.DeltaPicOrderCntBottom = r.se()
		}
	case 1:
		if !sps.DeltaPicOrderAlwaysZero {
			h.DeltaPicOrderCnt[0] = r.se()
			if pps.BottomFieldPIC && !h.FieldPic {
				h.DeltaPicOrderCnt[1] = r.se()
			}
		}
	}
	if pps.RedundantPicCnt {
		h.RedundantPicCnt = r.ue()
	}
	if sliceTypeIsB(h.SliceType) {
		h.DirectSpatialMV = r.u1()
	}
	if sliceTypeIsP(h.SliceType) || sliceTypeIsSP(h.SliceType) || sliceTypeIsB(h.SliceType) {
		if r.u1() { // num_ref_idx_active_override_flag
			h.NumRefIdxL0Active = int(r.ue()) + 1
			if sliceTypeIsB(h.SliceType) {
				h.NumRefIdxL1Active = int(r.ue()) + 1
			}
		} else {
			h.NumRefIdxL0Active = pps.NumRefIdxL0
			h.NumRefIdxL1Active = pps.NumRefIdxL1
		}
	}
	if !sliceTypeIsI(h.SliceType) && !sliceTypeIsSI(h.SliceType) {
		parseRefPicListReordering(r, sliceTypeIsB(h.SliceType))
	}
	if (pps.WeightedPred && (sliceTypeIsP(h.SliceType) || sliceTypeIsSP(h.SliceType))) ||
		(pps.WeightedBipred == 1 && sliceTypeIsB(h.SliceType)) {
		parsePredWeightTable(r, sps, h, sliceTypeIsB(h.SliceType))
	}
	if nalRefIDC != 0 {
		parseDecRefPicMarking(r, nalTyp, h.SliceType)
	}
	if pps.EntropyCoding && !sliceTypeIsI(h.SliceType) && !sliceTypeIsSI(h.SliceType) {
		h.CabacInitIDC = r.ue()
	}
	h.SliceQPDelta = r.se()
	if pps.DeblockingCtl {
		h.DisableDeblockingFilterIDC = r.ue()
		if h.DisableDeblockingFilterIDC != 1 {
			h.SliceAlphaC0OffsetDiv2 = r.se()
			h.SliceBetaOffsetDiv2 = r.se()
		}
	}
	if err := r.err(); err != nil {
		return nil, err
	}
	return h, nil
}

func parseRefPicListReordering(r *bitReader, isB bool) {
	skipOne := func() {
		for {
			idc := r.ue()
			if idc == 3 {
				return
			}
			switch idc {
			case 0, 1:
				r.ue() // abs_diff_pic_num_minus1
			case 2:
				r.ue() // long_term_pic_num
			default:
				r.skip(0)
				return
			}
		}
	}
	if r.u1() { // ref_pic_list_reordering_flag_l0
		skipOne()
	}
	if isB && r.u1() { // ref_pic_list_reordering_flag_l1
		skipOne()
	}
}

func parsePredWeightTable(r *bitReader, sps *SPS, h *SliceHeader, isB bool) {
	h.LumaLog2WeightDenom = int(r.ue())
	if sps.ChromaFormatIDC != 0 {
		h.ChromaLog2WeightDenom = int(r.ue())
	}
	read := func(n int) (lw, lo []int, cw, co [][2]int) {
		lw = make([]int, n)
		lo = make([]int, n)
		cw = make([][2]int, n)
		co = make([][2]int, n)
		for i := 0; i < n; i++ {
			lw[i] = 1 << uint(h.LumaLog2WeightDenom)
			if r.u1() { // luma_weight_x_flag
				lw[i] = int(r.se())
				lo[i] = int(r.se())
			}
			if sps.ChromaFormatIDC != 0 {
				cw[i] = [2]int{1 << uint(h.ChromaLog2WeightDenom), 1 << uint(h.ChromaLog2WeightDenom)}
				if r.u1() { // chroma_weight_x_flag
					for j := 0; j < 2; j++ {
						cw[i][j] = int(r.se())
						co[i][j] = int(r.se())
					}
				}
			}
		}
		return
	}
	h.LumaWeightL0, h.LumaOffsetL0, h.ChromaWeightL0, h.ChromaOffsetL0 = read(h.NumRefIdxL0Active)
	if isB {
		h.LumaWeightL1, h.LumaOffsetL1, h.ChromaWeightL1, h.ChromaOffsetL1 = read(h.NumRefIdxL1Active)
	}
}

func parseDecRefPicMarking(r *bitReader, nalTyp int, sliceType uint32) {
	if nalTyp == nalIDR {
		r.u1() // no_output_of_prior_pics_flag
		r.u1() // long_term_reference_flag
		return
	}
	if !r.u1() { // adaptive_ref_pic_marking_mode_flag
		return
	}
	for {
		mmco := r.ue()
		switch mmco {
		case 0:
			return
		case 1:
			r.ue()
		case 2:
			r.ue()
		case 3:
			r.ue()
			r.ue()
		case 4:
			r.ue()
		case 5:
			// no extra data
		case 6:
			r.ue()
		default:
			return
		}
	}
}
