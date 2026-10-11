package h264

// PPS is a parsed picture parameter set (spec 7.3.2.2).
type PPS struct {
	ID               uint32
	SPSID            uint32
	EntropyCoding    bool // true = CABAC
	BottomFieldPIC   bool
	NumSliceGroups   int // num_slice_groups_minus1 + 1
	NumRefIdxL0      int
	NumRefIdxL1      int
	WeightedPred     bool
	WeightedBipred   uint32
	PicInitQP        int32 // pic_init_qp_minus26
	ChromaQPOffset   int32
	DeblockingCtl    bool
	ConstrainedIntra bool
	RedundantPicCnt  bool

	// Optional extension (present when more_rbsp_data).
	Transform8x8Mode     bool
	SecondChromaQPOffset int32
	ChromaFormatIDC      uint32 // copied from the SPS by the decoder
}

// ParsePPS parses a PPS NAL unit (including its 1-byte NAL header).
func ParsePPS(nal []byte) (*PPS, error) {
	if len(nal) < 2 || nalType(nal[0]) != nalPPS {
		return nil, errf("h264: not a PPS NAL unit")
	}
	r := newBitReader(unescapeRBSP(nal[1:]))
	p := &PPS{}
	p.ID = r.ue()
	p.SPSID = r.ue()
	p.EntropyCoding = r.u1()
	p.BottomFieldPIC = r.u1()
	p.NumSliceGroups = int(r.ue()) + 1
	if p.NumSliceGroups > 1 {
		return nil, errf("h264: slice groups (FMO) not supported")
	}
	p.NumRefIdxL0 = int(r.ue()) + 1
	p.NumRefIdxL1 = int(r.ue()) + 1
	p.WeightedPred = r.u1()
	p.WeightedBipred = r.u(2)
	p.PicInitQP = r.se()
	r.se() // pic_init_qs_minus26 (not needed yet)
	p.ChromaQPOffset = r.se()
	p.DeblockingCtl = r.u1()
	p.ConstrainedIntra = r.u1()
	p.RedundantPicCnt = r.u1()
	// Optional PPS extension (present when there is more RBSP data).
	if r.moreRBSPData() {
		p.Transform8x8Mode = r.u1()
		if r.u1() { // pic_scaling_matrix_present_flag
			n := 8
			if p.ChromaFormatIDC == 3 {
				n = 12
			}
			for i := 0; i < n; i++ {
				if r.u1() {
					size := 16
					if i >= 6 {
						size = 64
					}
					skipScalingList(r, size)
				}
			}
		}
		p.SecondChromaQPOffset = r.se()
	}
	if err := r.err(); err != nil {
		return nil, err
	}
	return p, nil
}
