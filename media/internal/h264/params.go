package h264

// SPS is a parsed sequence parameter set (spec 7.3.2.1.1).
type SPS struct {
	ProfileIDC      int
	ConstraintFlags int
	LevelIDC        int
	ID              int

	ChromaFormatIDC     int
	SeparateColourPlane bool
	BitDepthLuma        int
	BitDepthChroma      int
	QPPrimeYZeroBypass  bool

	// ScalingPresent reports seq_scaling_matrix_present_flag; Scaling4 and
	// Scaling8 then hold the resolved lists in raster order.
	ScalingPresent bool
	Scaling4       [6][16]uint8
	Scaling8       [6][64]uint8

	Log2MaxFrameNum         int
	PicOrderCntType         int
	Log2MaxPicOrderCntLsb   int
	DeltaPicOrderAlwaysZero bool
	OffsetForNonRefPic      int
	OffsetForTopToBottom    int
	OffsetForRefFrame       []int

	MaxNumRefFrames       int
	GapsInFrameNumAllowed bool
	PicWidthInMbs         int
	PicHeightInMapUnits   int
	FrameMbsOnly          bool
	MbAdaptiveFrameField  bool
	Direct8x8Inference    bool

	CropLeft, CropRight, CropTop, CropBottom int // in luma samples

	// VUI subset.
	SARNum, SARDen          int
	FullRange               bool
	ColourPrimaries         int
	TransferCharacteristics int
	MatrixCoefficients      int
	NumUnitsInTick          uint32
	TimeScale               uint32
	HasTiming               bool
	BitstreamRestriction    bool
	MaxNumReorderFrames     int
	MaxDecFrameBuffering    int

	// Derived.
	Width, Height int // display size after cropping
}

// FrameHeightInMbs is the coded picture height in macroblocks.
func (s *SPS) FrameHeightInMbs() int {
	if s.FrameMbsOnly {
		return s.PicHeightInMapUnits
	}
	return 2 * s.PicHeightInMapUnits
}

// FPS reports the nominal frame rate from the VUI timing info, or 0.
func (s *SPS) FPS() float64 {
	if !s.HasTiming || s.NumUnitsInTick == 0 {
		return 0
	}
	return float64(s.TimeScale) / (2 * float64(s.NumUnitsInTick))
}

func highProfile(idc int) bool {
	switch idc {
	case 100, 110, 122, 244, 44, 83, 86, 118, 128, 138, 139, 134, 135:
		return true
	}
	return false
}

// Default scaling lists in zig-zag order (spec Table 7-3, 7-4).
var (
	defaultScaling4 = [2][16]uint8{
		{6, 13, 13, 20, 20, 20, 28, 28, 28, 28, 32, 32, 32, 37, 37, 42},
		{10, 14, 14, 20, 20, 20, 24, 24, 24, 24, 27, 27, 27, 30, 30, 34},
	}
	defaultScaling8 = [2][64]uint8{
		{
			6, 10, 10, 13, 11, 13, 16, 16, 16, 16, 18, 18, 18, 18, 18, 23,
			23, 23, 23, 23, 23, 25, 25, 25, 25, 25, 25, 25, 27, 27, 27, 27,
			27, 27, 27, 27, 29, 29, 29, 29, 29, 29, 29, 31, 31, 31, 31, 31,
			31, 33, 33, 33, 33, 33, 36, 36, 36, 36, 38, 38, 38, 40, 40, 42,
		},
		{
			9, 13, 13, 15, 13, 15, 17, 17, 17, 17, 19, 19, 19, 19, 19, 21,
			21, 21, 21, 21, 21, 22, 22, 22, 22, 22, 22, 22, 24, 24, 24, 24,
			24, 24, 24, 24, 25, 25, 25, 25, 25, 25, 25, 27, 27, 27, 27, 27,
			27, 28, 28, 28, 28, 28, 30, 30, 30, 30, 32, 32, 32, 33, 33, 35,
		},
	}
)

func flat4() (l [16]uint8) {
	for i := range l {
		l[i] = 16
	}
	return
}

func flat8() (l [64]uint8) {
	for i := range l {
		l[i] = 16
	}
	return
}

// zigToRaster4 / zigToRaster8 map a zig-zag (frame) scan position to its
// raster position.
func zigToRaster4(l [16]uint8) (out [16]uint8) {
	for i, v := range l {
		out[zigzag4x4[i]] = v
	}
	return
}

func zigToRaster8(l [64]uint8) (out [64]uint8) {
	for i, v := range l {
		out[zigzag8x8[i]] = v
	}
	return
}

// parseScalingList reads one scaling_list() (spec 7.3.2.1.1.1) into list
// (zig-zag order). It returns useDefault when the list signals the default.
func parseScalingList(r *bitReader, list []uint8) (useDefault bool) {
	last, next := 8, 8
	for j := range list {
		if next != 0 {
			delta := int(r.se())
			next = (last + delta + 256) % 256
			if j == 0 && next == 0 {
				return true
			}
		}
		if next != 0 {
			list[j] = uint8(next)
		} else {
			list[j] = uint8(last)
		}
		last = int(list[j])
	}
	return false
}

// parseScalingMatrix parses the n scaling lists of an SPS or PPS and
// resolves the fall-back rules (spec Table 7-2). fb4/fb8 are the fall-back
// lists for list 0/3 and 6/7 (rule A: defaults, rule B: the SPS lists).
func parseScalingMatrix(r *bitReader, n int, fb4 *[6][16]uint8, fb8 *[6][64]uint8, s4 *[6][16]uint8, s8 *[6][64]uint8) {
	for i := 0; i < n; i++ {
		present := r.u1()
		if i < 6 {
			var l [16]uint8
			switch {
			case present:
				if parseScalingList(r, l[:]) {
					s4[i] = zigToRaster4(defaultScaling4[i/3])
				} else {
					s4[i] = zigToRaster4(l)
				}
			case i == 0 || i == 3:
				s4[i] = fb4[i]
			default:
				s4[i] = s4[i-1]
			}
		} else {
			k := i - 6
			var l [64]uint8
			switch {
			case present:
				if parseScalingList(r, l[:]) {
					s8[k] = zigToRaster8(defaultScaling8[k%2])
				} else {
					s8[k] = zigToRaster8(l)
				}
			case k < 2:
				s8[k] = fb8[k]
			default:
				s8[k] = s8[k-2]
			}
		}
	}
	// Lists not transmitted at all (8x8 lists when n == 6, or the 4:4:4 Cb/Cr
	// 8x8 lists) still need sensible values.
	if n <= 6 {
		s8[0], s8[1] = fb8[0], fb8[1]
	}
	if n <= 8 {
		s8[2], s8[3], s8[4], s8[5] = s8[0], s8[1], s8[0], s8[1]
	}
}

// fallbackA returns the rule-A fall-back lists: the defaults in raster order.
func fallbackA() (f4 [6][16]uint8, f8 [6][64]uint8) {
	for i := 0; i < 6; i++ {
		f4[i] = zigToRaster4(defaultScaling4[i/3])
		f8[i] = zigToRaster8(defaultScaling8[i%2])
	}
	return
}

// ParseSPS parses an SPS NAL unit (including its header byte).
func ParseSPS(nal []byte) (*SPS, error) {
	if len(nal) < 4 || nalType(nal[0]) != nalSPS {
		return nil, errf("h264: not an SPS NAL unit")
	}
	r := newBitReader(unescapeRBSP(nil, nal[1:]))
	s := &SPS{
		ProfileIDC:      int(r.u(8)),
		ConstraintFlags: int(r.u(8)),
		LevelIDC:        int(r.u(8)),
		ID:              int(r.ue()),
		ChromaFormatIDC: 1,
		BitDepthLuma:    8,
		BitDepthChroma:  8,
	}
	if s.ID > 31 {
		return nil, errf("h264: bad seq_parameter_set_id")
	}
	for i := 0; i < 6; i++ {
		s.Scaling4[i] = flat4()
		s.Scaling8[i] = flat8()
	}
	if highProfile(s.ProfileIDC) {
		s.ChromaFormatIDC = int(r.ue())
		if s.ChromaFormatIDC > 3 {
			return nil, errf("h264: bad chroma_format_idc")
		}
		if s.ChromaFormatIDC == 3 {
			s.SeparateColourPlane = r.u1()
		}
		s.BitDepthLuma = int(r.ue()) + 8
		s.BitDepthChroma = int(r.ue()) + 8
		s.QPPrimeYZeroBypass = r.u1()
		if r.u1() {
			s.ScalingPresent = true
			n := 8
			if s.ChromaFormatIDC == 3 {
				n = 12
			}
			f4, f8 := fallbackA()
			parseScalingMatrix(r, n, &f4, &f8, &s.Scaling4, &s.Scaling8)
		}
	}
	s.Log2MaxFrameNum = int(r.ue()) + 4
	s.PicOrderCntType = int(r.ue())
	switch s.PicOrderCntType {
	case 0:
		s.Log2MaxPicOrderCntLsb = int(r.ue()) + 4
	case 1:
		s.DeltaPicOrderAlwaysZero = r.u1()
		s.OffsetForNonRefPic = int(r.se())
		s.OffsetForTopToBottom = int(r.se())
		n := int(r.ue())
		if n > 255 {
			return nil, errf("h264: bad num_ref_frames_in_pic_order_cnt_cycle")
		}
		for i := 0; i < n; i++ {
			s.OffsetForRefFrame = append(s.OffsetForRefFrame, int(r.se()))
		}
	case 2:
	default:
		return nil, errf("h264: bad pic_order_cnt_type")
	}
	if s.Log2MaxFrameNum > 16 || s.Log2MaxPicOrderCntLsb > 16 {
		return nil, errf("h264: bad log2_max field")
	}
	s.MaxNumRefFrames = int(r.ue())
	s.GapsInFrameNumAllowed = r.u1()
	s.PicWidthInMbs = int(r.ue()) + 1
	s.PicHeightInMapUnits = int(r.ue()) + 1
	s.FrameMbsOnly = r.u1()
	if !s.FrameMbsOnly {
		s.MbAdaptiveFrameField = r.u1()
	}
	s.Direct8x8Inference = r.u1()
	var cl, cr, ct, cb int
	if r.u1() {
		cl, cr, ct, cb = int(r.ue()), int(r.ue()), int(r.ue()), int(r.ue())
	}
	if r.u1() {
		parseVUI(r, s)
	}
	if r.err() != nil {
		return nil, r.err()
	}
	if s.PicWidthInMbs > 1024 || s.PicHeightInMapUnits > 1024 {
		return nil, errf("h264: picture too large")
	}

	cux, cuy := 1, 1
	if s.ChromaFormatIDC != 0 && !s.SeparateColourPlane {
		if s.ChromaFormatIDC == 1 || s.ChromaFormatIDC == 2 {
			cux = 2
		}
		if s.ChromaFormatIDC == 1 {
			cuy = 2
		}
	}
	if !s.FrameMbsOnly {
		cuy *= 2
	}
	s.CropLeft, s.CropRight = cl*cux, cr*cux
	s.CropTop, s.CropBottom = ct*cuy, cb*cuy
	s.Width = s.PicWidthInMbs*16 - s.CropLeft - s.CropRight
	s.Height = s.FrameHeightInMbs()*16 - s.CropTop - s.CropBottom
	if s.Width <= 0 || s.Height <= 0 {
		// Bogus cropping: ignore it.
		s.CropLeft, s.CropRight, s.CropTop, s.CropBottom = 0, 0, 0, 0
		s.Width, s.Height = s.PicWidthInMbs*16, s.FrameHeightInMbs()*16
	}
	return s, nil
}

func parseVUI(r *bitReader, s *SPS) {
	s.SARNum, s.SARDen = 1, 1
	if r.u1() { // aspect_ratio_info_present_flag
		idc := int(r.u(8))
		if idc == 255 {
			s.SARNum, s.SARDen = int(r.u(16)), int(r.u(16))
		} else if idc > 0 && idc < len(sarTable) {
			s.SARNum, s.SARDen = sarTable[idc][0], sarTable[idc][1]
		}
	}
	if r.u1() { // overscan_info_present_flag
		r.u1()
	}
	s.ColourPrimaries, s.TransferCharacteristics, s.MatrixCoefficients = 2, 2, 2
	if r.u1() { // video_signal_type_present_flag
		r.skip(3)
		s.FullRange = r.u1()
		if r.u1() {
			s.ColourPrimaries = int(r.u(8))
			s.TransferCharacteristics = int(r.u(8))
			s.MatrixCoefficients = int(r.u(8))
		}
	}
	if r.u1() { // chroma_loc_info_present_flag
		r.ue()
		r.ue()
	}
	if r.u1() { // timing_info_present_flag
		s.NumUnitsInTick = r.u(32)
		s.TimeScale = r.u(32)
		r.u1() // fixed_frame_rate_flag
		s.HasTiming = true
	}
	nal := r.u1()
	if nal {
		skipHRD(r)
	}
	vcl := r.u1()
	if vcl {
		skipHRD(r)
	}
	if nal || vcl {
		r.u1() // low_delay_hrd_flag
	}
	r.u1()      // pic_struct_present_flag
	if r.u1() { // bitstream_restriction_flag
		r.u1() // motion_vectors_over_pic_boundaries_flag
		r.ue() // max_bytes_per_pic_denom
		r.ue() // max_bits_per_mb_denom
		r.ue() // log2_max_mv_length_horizontal
		r.ue() // log2_max_mv_length_vertical
		reorder := int(r.ue())
		buffering := int(r.ue())
		if r.err() == nil && reorder <= 16 && buffering <= 16 {
			s.BitstreamRestriction = true
			s.MaxNumReorderFrames = reorder
			s.MaxDecFrameBuffering = buffering
		}
	}
	// A truncated VUI is tolerated: the fields above are optional extras.
	if r.err() != nil {
		r.e = nil
	}
}

func skipHRD(r *bitReader) {
	n := int(r.ue()) + 1
	r.skip(8)
	for i := 0; i < n && r.err() == nil; i++ {
		r.ue()
		r.ue()
		r.u1()
	}
	r.skip(20)
}

var sarTable = [][2]int{
	{0, 1}, {1, 1}, {12, 11}, {10, 11}, {16, 11}, {40, 33}, {24, 11}, {20, 11},
	{32, 11}, {80, 33}, {18, 11}, {15, 11}, {64, 33}, {160, 99}, {4, 3}, {3, 2}, {2, 1},
}

// PPS is a parsed picture parameter set (spec 7.3.2.2).
type PPS struct {
	ID                     int
	SPSID                  int
	EntropyCodingCABAC     bool
	BottomFieldPicOrder    bool
	NumSliceGroups         int
	NumRefIdxL0Default     int
	NumRefIdxL1Default     int
	WeightedPred           bool
	WeightedBipredIDC      int
	PicInitQP              int
	PicInitQS              int
	ChromaQPIndexOffset    int
	DeblockingFilterCtl    bool
	ConstrainedIntraPred   bool
	RedundantPicCntPresent bool
	Transform8x8Mode       bool
	SecondChromaQPOffset   int

	// Resolved scaling lists (raster order); flat unless signalled.
	Scaling4 [6][16]uint8
	Scaling8 [6][64]uint8
}

// ParsePPS parses a PPS NAL unit. The referenced SPS is needed to resolve
// scaling-list fall-back and the 4:4:4 list count; spsByID may return nil
// when it is not known yet (the lists then fall back to flat).
func ParsePPS(nal []byte, spsByID func(int) *SPS) (*PPS, error) {
	if len(nal) < 2 || nalType(nal[0]) != nalPPS {
		return nil, errf("h264: not a PPS NAL unit")
	}
	r := newBitReader(unescapeRBSP(nil, nal[1:]))
	p := &PPS{}
	p.ID = int(r.ue())
	p.SPSID = int(r.ue())
	if p.ID > 255 || p.SPSID > 31 {
		return nil, errf("h264: bad parameter set id")
	}
	p.EntropyCodingCABAC = r.u1()
	p.BottomFieldPicOrder = r.u1()
	p.NumSliceGroups = int(r.ue()) + 1
	if p.NumSliceGroups > 1 {
		return nil, ErrUnsupported
	}
	p.NumRefIdxL0Default = int(r.ue()) + 1
	p.NumRefIdxL1Default = int(r.ue()) + 1
	if p.NumRefIdxL0Default > 32 || p.NumRefIdxL1Default > 32 {
		return nil, errf("h264: bad num_ref_idx_default")
	}
	p.WeightedPred = r.u1()
	p.WeightedBipredIDC = int(r.u(2))
	p.PicInitQP = 26 + int(r.se())
	p.PicInitQS = 26 + int(r.se())
	p.ChromaQPIndexOffset = int(r.se())
	p.DeblockingFilterCtl = r.u1()
	p.ConstrainedIntraPred = r.u1()
	p.RedundantPicCntPresent = r.u1()
	p.SecondChromaQPOffset = p.ChromaQPIndexOffset

	var sps *SPS
	if spsByID != nil {
		sps = spsByID(p.SPSID)
	}
	for i := 0; i < 6; i++ {
		p.Scaling4[i] = flat4()
		p.Scaling8[i] = flat8()
	}
	if sps != nil {
		p.Scaling4, p.Scaling8 = sps.Scaling4, sps.Scaling8
	}
	if r.moreRBSPData() {
		p.Transform8x8Mode = r.u1()
		if r.u1() { // pic_scaling_matrix_present_flag
			chroma := 1
			if sps != nil {
				chroma = sps.ChromaFormatIDC
			}
			n := 6
			if p.Transform8x8Mode {
				if chroma == 3 {
					n += 6
				} else {
					n += 2
				}
			}
			var f4 [6][16]uint8
			var f8 [6][64]uint8
			if sps != nil && sps.ScalingPresent {
				f4, f8 = sps.Scaling4, sps.Scaling8 // fall-back rule B
			} else {
				f4, f8 = fallbackA()
			}
			parseScalingMatrix(r, n, &f4, &f8, &p.Scaling4, &p.Scaling8)
		}
		p.SecondChromaQPOffset = int(r.se())
	}
	if err := r.err(); err != nil {
		return nil, err
	}
	if p.PicInitQP < 0 || p.PicInitQP > 51 {
		return nil, errf("h264: bad pic_init_qp")
	}
	return p, nil
}
