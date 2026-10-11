package h264

// SPS is a parsed sequence parameter set (spec 7.3.2.1.1 / 7.4.2.1.1).
type SPS struct {
	ProfileIDC      uint8
	ConstraintFlags uint8
	LevelIDC        uint8
	ID              uint32

	ChromaFormatIDC     uint32 // 0=mono,1=4:2:0,2=4:2:2,3=4:4:4
	SeparateColourPlane bool
	BitDepthLuma        int
	BitDepthChroma      int
	QPPrimeYZeroBypass  bool

	Log2MaxFrameNum         int
	PicOrderCntType         uint32
	Log2MaxPicOrderCntLsb   int
	DeltaPicOrderAlwaysZero bool
	OffsetForNonRefPic      int32
	OffsetForTopToBottom    int32
	NumRefFramesInPOCCycle  int
	OffsetForRefFrame       []int32

	MaxNumRefFrames       int
	GapsInFrameNumAllowed bool
	PicWidthInMbs         int
	PicHeightInMapUnits   int
	FrameMbsOnly          bool
	MbAdaptiveFrameField  bool
	Direct8x8Inference    bool

	FrameCropping                 bool
	FrameCropLeft, FrameCropRight int
	FrameCropTop, FrameCropBottom int
	VUIParametersPresent          bool

	// VUI (subset).
	NumUnitsInTick uint32
	TimeScale      uint32
	FixedFrameRate bool
	HasTimingInfo  bool

	CropUnitX int
	CropUnitY int

	// Derived display size in luma samples.
	Width  int
	Height int
	FPS    float32
}

// highProfile reports whether profile_idc needs the chroma/bit-depth fields.
func highProfile(idc uint8) bool {
	switch idc {
	case 100, 110, 122, 244, 44, 83, 86, 118, 128, 138, 139, 134, 135:
		return true
	}
	return false
}

// ParseSPS parses an SPS NAL unit (including its 1-byte NAL header).
func ParseSPS(nal []byte) (*SPS, error) {
	if len(nal) < 4 {
		return nil, errf("h264: SPS too short")
	}
	if nalType(nal[0]) != nalSPS {
		return nil, errf("h264: not an SPS NAL unit")
	}
	rbsp := unescapeRBSP(nal[1:])
	r := newBitReader(rbsp)

	s := &SPS{
		ProfileIDC:      uint8(r.u(8)),
		ConstraintFlags: uint8(r.u(8)),
		LevelIDC:        uint8(r.u(8)),
		ID:              r.ue(),
		ChromaFormatIDC: 1,
		BitDepthLuma:    8,
		BitDepthChroma:  8,
	}
	if r.err() != nil {
		return nil, r.err()
	}

	if highProfile(s.ProfileIDC) {
		s.ChromaFormatIDC = r.ue()
		if s.ChromaFormatIDC > 3 {
			return nil, errf("h264: bad chroma_format_idc")
		}
		if s.ChromaFormatIDC == 3 {
			s.SeparateColourPlane = r.u1()
		}
		s.BitDepthLuma = int(r.ue()) + 8
		s.BitDepthChroma = int(r.ue()) + 8
		s.QPPrimeYZeroBypass = r.u1()
		if r.u1() { // seq_scaling_matrix_present_flag
			n := 8
			if s.ChromaFormatIDC == 3 {
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
	}

	s.Log2MaxFrameNum = int(r.ue()) + 4
	s.PicOrderCntType = r.ue()
	switch s.PicOrderCntType {
	case 0:
		s.Log2MaxPicOrderCntLsb = int(r.ue()) + 4
	case 1:
		s.DeltaPicOrderAlwaysZero = r.u1()
		s.OffsetForNonRefPic = r.se()
		s.OffsetForTopToBottom = r.se()
		s.NumRefFramesInPOCCycle = int(r.ue())
		for i := 0; i < s.NumRefFramesInPOCCycle; i++ {
			s.OffsetForRefFrame = append(s.OffsetForRefFrame, r.se())
		}
	case 2:
		// nothing
	default:
		return nil, errf("h264: bad pic_order_cnt_type")
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

	s.FrameCropping = r.u1()
	if s.FrameCropping {
		s.FrameCropLeft = int(r.ue())
		s.FrameCropRight = int(r.ue())
		s.FrameCropTop = int(r.ue())
		s.FrameCropBottom = int(r.ue())
	}
	s.VUIParametersPresent = r.u1()
	if s.VUIParametersPresent {
		parseVUI(r, s)
	}
	if err := r.err(); err != nil {
		return nil, err
	}

	s.derive()
	return s, nil
}

// skipScalingList consumes a scaling list of the given size (16 or 64).
func skipScalingList(r *bitReader, size int) {
	lastScale := 8
	nextScale := 8
	for j := 0; j < size; j++ {
		if nextScale != 0 {
			delta := r.se()
			nextScale = (lastScale + int(delta) + 256) % 256
			lastScale = nextScale
		}
	}
}

// parseVUI parses enough of the VUI (spec E.1.1) to recover timing/FPS.
// It stops after timing_info; HRD and the rest are not needed here.
func parseVUI(r *bitReader, s *SPS) {
	if r.u1() { // aspect_ratio_info_present_flag
		if r.u(8) == 255 { // Extended_SAR
			r.skip(16)
			r.skip(16)
		}
	}
	if r.u1() { // overscan_info_present_flag
		r.u1() // overscan_appropriate_flag
	}
	if r.u1() { // video_signal_type_present_flag
		r.skip(3)   // video_format
		r.u1()      // video_full_range_flag
		if r.u1() { // colour_description_present_flag
			r.skip(24) // primaries, transfer, matrix
		}
	}
	if r.u1() { // chroma_loc_info_present_flag
		r.ue()
		r.ue()
	}
	if r.u1() { // timing_info_present_flag
		s.NumUnitsInTick = r.u(32)
		s.TimeScale = r.u(32)
		s.FixedFrameRate = r.u1()
		s.HasTimingInfo = true
	}
}

// derive computes display width/height (with cropping) and FPS.
func (s *SPS) derive() {
	chromaArrayType := s.ChromaFormatIDC
	if s.SeparateColourPlane {
		chromaArrayType = 0
	}
	subW, subH := 1, 1
	switch chromaArrayType {
	case 1:
		subW, subH = 2, 2
	case 2:
		subW, subH = 2, 1
	}
	s.CropUnitX = 1
	if chromaArrayType != 0 {
		s.CropUnitX = subW
	}
	s.CropUnitY = 1
	if chromaArrayType != 0 {
		s.CropUnitY = subH
	}
	if !s.FrameMbsOnly {
		s.CropUnitY *= 2
	}
	frameMbsOnly := 1
	if s.FrameMbsOnly {
		frameMbsOnly = 1
	} else {
		frameMbsOnly = 2
	}
	s.Width = s.PicWidthInMbs*16 - s.CropUnitX*(s.FrameCropLeft+s.FrameCropRight)
	s.Height = frameMbsOnly*s.PicHeightInMapUnits*16 - s.CropUnitY*(s.FrameCropTop+s.FrameCropBottom)
	if s.HasTimingInfo && s.NumUnitsInTick > 0 {
		s.FPS = float32(s.TimeScale) / (2 * float32(s.NumUnitsInTick))
	}
}
