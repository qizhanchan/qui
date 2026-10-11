package hevc

// Parameter sets (spec 7.3.2, 7.4.3).

// stRPS is a short-term reference picture set.
type stRPS struct {
	numNeg, numPos int
	deltaPOC       [32]int // negatives first (decreasing), then positives
	used           [32]bool
}

func (s *stRPS) num() int { return s.numNeg + s.numPos }

// SPS is a sequence parameter set (the fields the decoder uses).
type SPS struct {
	ID                   int
	ChromaFormat         int
	Width, Height        int // pic_width/height_in_luma_samples
	ConfLeft, ConfRight  int // conformance window, luma samples
	ConfTop, ConfBottom  int
	BitDepth, BitDepthC  int
	Log2MaxPOCLsb        int
	MaxDecPicBuffering   int // HighestTid
	MaxNumReorder        int
	MaxLatencyIncrease   int
	Log2MinCb, Log2Ctb   int
	Log2MinTb, Log2MaxTb int
	MaxTrDepthInter      int
	MaxTrDepthIntra      int
	ScalingListEnabled   bool
	ScalingList          *scalingList // nil = flat (or default when enabled without data)
	AMP                  bool
	SAO                  bool
	PCM                  bool
	PCMBitDepth          int
	PCMBitDepthC         int
	Log2MinPCM           int
	Log2MaxPCM           int
	PCMLoopFilterOff     bool
	stRPS                []stRPS
	LongTermRefsPresent  bool
	ltPOCLsb             []int
	ltUsed               []bool
	TemporalMVP          bool
	StrongIntraSmoothing bool

	// VUI colour description.
	FullRange bool
	Matrix    int

	// Derived.
	CtbSize            int
	PicWCtb, PicHCtb   int
	MinCbW, MinCbH     int
	FrameRateN         int // VUI timing (0 if absent)
	FrameRateD         int
	VideoSignalPresent bool
}

// PPS is a picture parameter set.
type PPS struct {
	ID, SPSID                int
	DependentSlices          bool
	OutputFlagPresent        bool
	NumExtraSliceHeaderBits  int
	SignDataHiding           bool
	CabacInitPresent         bool
	NumRefIdxL0Default       int
	NumRefIdxL1Default       int
	InitQP                   int
	ConstrainedIntraPred     bool
	TransformSkip            bool
	CuQPDeltaEnabled         bool
	DiffCuQPDeltaDepth       int
	CbQPOffset, CrQPOffset   int
	SliceChromaQPOffsets     bool
	WeightedPred             bool
	WeightedBipred           bool
	TransquantBypass         bool
	Tiles                    bool
	EntropySync              bool
	colWidths, rowHeights    []int // tiles, in CTBs
	LoopFilterAcrossTiles    bool
	LoopFilterAcrossSlices   bool
	DeblockingOverride       bool
	DeblockingDisabled       bool
	BetaOffset, TcOffset     int // *_div2 * 2
	ScalingList              *scalingList
	ListsModification        bool
	Log2ParMrgLevel          int
	SliceHeaderExtension     bool
	Log2MaxTransformSkipSize int

	// Derived per SPS (tile scan conversion).
	ctbRsToTs, ctbTsToRs []int
	tileID               []int // by TS address
	colBd, rowBd         []int
	minTbAddrZs          []int // per min TB, raster over the picture
	derivedFor           *SPS
}

// profileTierLevel skips profile_tier_level().
func profileTierLevel(r *bitReader, maxSubLayersMinus1 int) {
	r.skip(2 + 1 + 5 + 32 + 4 + 43 + 1)
	r.skip(8) // general_level_idc
	var profPresent, levelPresent [8]bool
	for i := 0; i < maxSubLayersMinus1; i++ {
		profPresent[i] = r.flag()
		levelPresent[i] = r.flag()
	}
	if maxSubLayersMinus1 > 0 {
		for i := maxSubLayersMinus1; i < 8; i++ {
			r.skip(2)
		}
	}
	for i := 0; i < maxSubLayersMinus1; i++ {
		if profPresent[i] {
			r.skip(88)
		}
		if levelPresent[i] {
			r.skip(8)
		}
	}
}

// parseSTRPS parses st_ref_pic_set(idx); sets holds the sets parsed so
// far (the SPS sets when idx == len(sets) for a slice header).
func parseSTRPS(r *bitReader, idx int, sets []stRPS, numInSPS int) (stRPS, error) {
	var s stRPS
	interPred := false
	if idx != 0 {
		interPred = r.flag()
	}
	if interPred {
		deltaIdx := 1
		if idx == numInSPS {
			deltaIdx = int(r.ue()) + 1
		}
		refIdx := idx - deltaIdx
		if refIdx < 0 || refIdx >= len(sets) {
			return s, errCorrupt
		}
		ref := &sets[refIdx]
		sign := r.flag()
		absDelta := int(r.ue()) + 1
		deltaRps := absDelta
		if sign {
			deltaRps = -absDelta
		}
		n := ref.num()
		var usedBy, useDelta [33]bool
		for j := 0; j <= n; j++ {
			usedBy[j] = r.flag()
			useDelta[j] = true
			if !usedBy[j] {
				useDelta[j] = r.flag()
			}
		}
		// Derivation 7-61, 7-62. ref deltas: S0 = deltaPOC[0:numNeg], S1 =
		// deltaPOC[numNeg:numNeg+numPos].
		s0 := func(j int) int { return ref.deltaPOC[j] }
		s1 := func(j int) int { return ref.deltaPOC[ref.numNeg+j] }
		var negD, posD [32]int
		var negU, posU [32]bool
		i := 0
		for j := ref.numPos - 1; j >= 0; j-- {
			d := s1(j) + deltaRps
			if d < 0 && useDelta[ref.numNeg+j] && i < 32 {
				negD[i], negU[i] = d, usedBy[ref.numNeg+j]
				i++
			}
		}
		if deltaRps < 0 && useDelta[n] && i < 32 {
			negD[i], negU[i] = deltaRps, usedBy[n]
			i++
		}
		for j := 0; j < ref.numNeg; j++ {
			d := s0(j) + deltaRps
			if d < 0 && useDelta[j] && i < 32 {
				negD[i], negU[i] = d, usedBy[j]
				i++
			}
		}
		s.numNeg = i
		i = 0
		for j := ref.numNeg - 1; j >= 0; j-- {
			d := s0(j) + deltaRps
			if d > 0 && useDelta[j] && i < 32 {
				posD[i], posU[i] = d, usedBy[j]
				i++
			}
		}
		if deltaRps > 0 && useDelta[n] && i < 32 {
			posD[i], posU[i] = deltaRps, usedBy[n]
			i++
		}
		for j := 0; j < ref.numPos; j++ {
			d := s1(j) + deltaRps
			if d > 0 && useDelta[ref.numNeg+j] && i < 32 {
				posD[i], posU[i] = d, usedBy[ref.numNeg+j]
				i++
			}
		}
		s.numPos = i
		if s.numNeg+s.numPos > 32 {
			return s, errCorrupt
		}
		copy(s.deltaPOC[:], negD[:s.numNeg])
		copy(s.used[:], negU[:s.numNeg])
		copy(s.deltaPOC[s.numNeg:], posD[:s.numPos])
		copy(s.used[s.numNeg:], posU[:s.numPos])
		return s, r.e
	}
	s.numNeg = int(r.ue())
	s.numPos = int(r.ue())
	if s.numNeg > 16 || s.numPos > 16 {
		return s, errCorrupt
	}
	poc := 0
	for i := 0; i < s.numNeg; i++ {
		poc -= int(r.ue()) + 1
		s.deltaPOC[i] = poc
		s.used[i] = r.flag()
	}
	poc = 0
	for i := 0; i < s.numPos; i++ {
		poc += int(r.ue()) + 1
		s.deltaPOC[s.numNeg+i] = poc
		s.used[s.numNeg+i] = r.flag()
	}
	return s, r.e
}

// ParseSPS parses a sequence parameter set NAL unit (2-byte header first).
func ParseSPS(nal []byte) (*SPS, error) {
	var r bitReader
	r.reset(unescape(nil, nal[2:]))
	s := &SPS{}
	r.skip(4) // sps_video_parameter_set_id
	maxSub := int(r.u(3))
	r.skip(1)
	profileTierLevel(&r, maxSub)
	s.ID = int(r.ue())
	if s.ID > 15 {
		return nil, errCorrupt
	}
	s.ChromaFormat = int(r.ue())
	if s.ChromaFormat == 3 {
		r.skip(1)
	}
	s.Width = int(r.ue())
	s.Height = int(r.ue())
	if r.flag() {
		sw, sh := 1, 1
		if s.ChromaFormat == 1 || s.ChromaFormat == 2 {
			sw = 2
		}
		if s.ChromaFormat == 1 {
			sh = 2
		}
		s.ConfLeft = int(r.ue()) * sw
		s.ConfRight = int(r.ue()) * sw
		s.ConfTop = int(r.ue()) * sh
		s.ConfBottom = int(r.ue()) * sh
	}
	s.BitDepth = int(r.ue()) + 8
	s.BitDepthC = int(r.ue()) + 8
	s.Log2MaxPOCLsb = int(r.ue()) + 4
	subInfo := r.flag()
	start := maxSub
	if subInfo {
		start = 0
	}
	for i := start; i <= maxSub; i++ {
		s.MaxDecPicBuffering = int(r.ue()) + 1
		s.MaxNumReorder = int(r.ue())
		s.MaxLatencyIncrease = int(r.ue())
	}
	s.Log2MinCb = int(r.ue()) + 3
	s.Log2Ctb = s.Log2MinCb + int(r.ue())
	s.Log2MinTb = int(r.ue()) + 2
	s.Log2MaxTb = s.Log2MinTb + int(r.ue())
	s.MaxTrDepthInter = int(r.ue())
	s.MaxTrDepthIntra = int(r.ue())
	s.ScalingListEnabled = r.flag()
	if s.ScalingListEnabled {
		if r.flag() {
			sl, err := parseScalingList(&r)
			if err != nil {
				return nil, err
			}
			s.ScalingList = sl
		} else {
			s.ScalingList = defaultScalingList()
		}
	}
	s.AMP = r.flag()
	s.SAO = r.flag()
	s.PCM = r.flag()
	if s.PCM {
		s.PCMBitDepth = int(r.u(4)) + 1
		s.PCMBitDepthC = int(r.u(4)) + 1
		s.Log2MinPCM = int(r.ue()) + 3
		s.Log2MaxPCM = s.Log2MinPCM + int(r.ue())
		s.PCMLoopFilterOff = r.flag()
	}
	n := int(r.ue())
	if n > 64 {
		return nil, errCorrupt
	}
	for i := 0; i < n; i++ {
		rps, err := parseSTRPS(&r, i, s.stRPS, n)
		if err != nil {
			return nil, err
		}
		s.stRPS = append(s.stRPS, rps)
	}
	s.LongTermRefsPresent = r.flag()
	if s.LongTermRefsPresent {
		nl := int(r.ue())
		if nl > 32 {
			return nil, errCorrupt
		}
		for i := 0; i < nl; i++ {
			s.ltPOCLsb = append(s.ltPOCLsb, int(r.u(s.Log2MaxPOCLsb)))
			s.ltUsed = append(s.ltUsed, r.flag())
		}
	}
	s.TemporalMVP = r.flag()
	s.StrongIntraSmoothing = r.flag()
	s.Matrix = 2
	if r.flag() {
		parseVUI(&r, s)
	}
	if r.e != nil {
		return nil, r.e
	}
	if s.ChromaFormat != 1 || s.BitDepth != 8 || s.BitDepthC != 8 {
		return nil, errUnsupported
	}
	if s.Log2Ctb < 4 || s.Log2Ctb > 6 || s.Log2MaxTb > 5 || s.Log2MaxTb > s.Log2Ctb || s.Log2MinTb >= s.Log2MinCb ||
		s.Width%(1<<s.Log2MinCb) != 0 || s.Height%(1<<s.Log2MinCb) != 0 || s.Width == 0 || s.Height == 0 {
		return nil, errCorrupt
	}
	s.CtbSize = 1 << s.Log2Ctb
	s.PicWCtb = (s.Width + s.CtbSize - 1) >> s.Log2Ctb
	s.PicHCtb = (s.Height + s.CtbSize - 1) >> s.Log2Ctb
	s.MinCbW = s.Width >> s.Log2MinCb
	s.MinCbH = s.Height >> s.Log2MinCb
	return s, nil
}

func parseVUI(r *bitReader, s *SPS) {
	if r.flag() { // aspect_ratio_info_present_flag
		if r.u(8) == 255 {
			r.skip(32)
		}
	}
	if r.flag() { // overscan_info_present_flag
		r.skip(1)
	}
	if r.flag() { // video_signal_type_present_flag
		s.VideoSignalPresent = true
		r.skip(3)
		s.FullRange = r.flag()
		if r.flag() {
			r.skip(16)
			s.Matrix = int(r.u(8))
		}
	}
	if r.flag() { // chroma_loc_info_present_flag
		r.ue()
		r.ue()
	}
	r.skip(3)     // neutral_chroma, field_seq, frame_field_info
	if r.flag() { // default_display_window_flag
		r.ue()
		r.ue()
		r.ue()
		r.ue()
	}
	if r.flag() { // vui_timing_info_present_flag
		s.FrameRateD = int(r.u(32))
		s.FrameRateN = int(r.u(32))
		// The remaining VUI (HRD, bitstream restriction) is not needed.
	}
}

// parsePPS parses a picture parameter set NAL unit.
func parsePPS(nal []byte, sps func(int) *SPS) (*PPS, error) {
	var r bitReader
	r.reset(unescape(nil, nal[2:]))
	p := &PPS{}
	p.ID = int(r.ue())
	p.SPSID = int(r.ue())
	if p.ID > 63 || p.SPSID > 15 {
		return nil, errCorrupt
	}
	s := sps(p.SPSID)
	if s == nil {
		return nil, errCorrupt
	}
	p.DependentSlices = r.flag()
	p.OutputFlagPresent = r.flag()
	p.NumExtraSliceHeaderBits = int(r.u(3))
	p.SignDataHiding = r.flag()
	p.CabacInitPresent = r.flag()
	p.NumRefIdxL0Default = int(r.ue()) + 1
	p.NumRefIdxL1Default = int(r.ue()) + 1
	p.InitQP = 26 + int(r.se())
	p.ConstrainedIntraPred = r.flag()
	p.TransformSkip = r.flag()
	p.CuQPDeltaEnabled = r.flag()
	if p.CuQPDeltaEnabled {
		p.DiffCuQPDeltaDepth = int(r.ue())
	}
	p.CbQPOffset = int(r.se())
	p.CrQPOffset = int(r.se())
	p.SliceChromaQPOffsets = r.flag()
	p.WeightedPred = r.flag()
	p.WeightedBipred = r.flag()
	p.TransquantBypass = r.flag()
	p.Tiles = r.flag()
	p.EntropySync = r.flag()
	if p.Tiles {
		cols := int(r.ue()) + 1
		rows := int(r.ue()) + 1
		if cols > s.PicWCtb || rows > s.PicHCtb {
			return nil, errCorrupt
		}
		uniform := r.flag()
		p.colWidths = make([]int, cols)
		p.rowHeights = make([]int, rows)
		if uniform {
			for i := range p.colWidths {
				p.colWidths[i] = ((i+1)*s.PicWCtb)/cols - (i*s.PicWCtb)/cols
			}
			for i := range p.rowHeights {
				p.rowHeights[i] = ((i+1)*s.PicHCtb)/rows - (i*s.PicHCtb)/rows
			}
		} else {
			sum := 0
			for i := 0; i < cols-1; i++ {
				p.colWidths[i] = int(r.ue()) + 1
				sum += p.colWidths[i]
			}
			p.colWidths[cols-1] = s.PicWCtb - sum
			sum = 0
			for i := 0; i < rows-1; i++ {
				p.rowHeights[i] = int(r.ue()) + 1
				sum += p.rowHeights[i]
			}
			p.rowHeights[rows-1] = s.PicHCtb - sum
			if p.colWidths[cols-1] <= 0 || p.rowHeights[rows-1] <= 0 {
				return nil, errCorrupt
			}
		}
		p.LoopFilterAcrossTiles = r.flag()
	} else {
		p.colWidths = []int{s.PicWCtb}
		p.rowHeights = []int{s.PicHCtb}
		p.LoopFilterAcrossTiles = true
	}
	p.LoopFilterAcrossSlices = r.flag()
	if r.flag() { // deblocking_filter_control_present_flag
		p.DeblockingOverride = r.flag()
		p.DeblockingDisabled = r.flag()
		if !p.DeblockingDisabled {
			p.BetaOffset = 2 * int(r.se())
			p.TcOffset = 2 * int(r.se())
		}
	}
	if r.flag() { // pps_scaling_list_data_present_flag
		sl, err := parseScalingList(&r)
		if err != nil {
			return nil, err
		}
		p.ScalingList = sl
	}
	p.ListsModification = r.flag()
	p.Log2ParMrgLevel = int(r.ue()) + 2
	p.SliceHeaderExtension = r.flag()
	p.Log2MaxTransformSkipSize = 2
	if r.flag() { // pps_extension_present_flag
		rangeExt := r.flag()
		r.skip(7)
		if rangeExt {
			return nil, errUnsupported
		}
	}
	if r.e != nil {
		return nil, r.e
	}
	return p, nil
}

// derive computes the tile scan conversion tables for sps (6.5.1, 6.5.2).
func (p *PPS) derive(s *SPS) {
	if p.derivedFor == s {
		return
	}
	p.derivedFor = s
	w, h := s.PicWCtb, s.PicHCtb
	n := w * h
	p.colBd = make([]int, len(p.colWidths)+1)
	for i, c := range p.colWidths {
		p.colBd[i+1] = p.colBd[i] + c
	}
	p.rowBd = make([]int, len(p.rowHeights)+1)
	for i, c := range p.rowHeights {
		p.rowBd[i+1] = p.rowBd[i] + c
	}
	p.ctbRsToTs = make([]int, n)
	p.ctbTsToRs = make([]int, n)
	p.tileID = make([]int, n)
	for rs := 0; rs < n; rs++ {
		tbX, tbY := rs%w, rs/w
		tileX, tileY := 0, 0
		for i := range p.colWidths {
			if tbX >= p.colBd[i] {
				tileX = i
			}
		}
		for j := range p.rowHeights {
			if tbY >= p.rowBd[j] {
				tileY = j
			}
		}
		v := 0
		for i := 0; i < tileX; i++ {
			v += p.rowHeights[tileY] * p.colWidths[i]
		}
		for j := 0; j < tileY; j++ {
			v += w * p.rowHeights[j]
		}
		v += (tbY-p.rowBd[tileY])*p.colWidths[tileX] + tbX - p.colBd[tileX]
		p.ctbRsToTs[rs] = v
		p.ctbTsToRs[v] = rs
	}
	tid := 0
	for j := range p.rowHeights {
		for i := range p.colWidths {
			for y := p.rowBd[j]; y < p.rowBd[j+1]; y++ {
				for x := p.colBd[i]; x < p.colBd[i+1]; x++ {
					p.tileID[p.ctbRsToTs[y*w+x]] = tid
				}
			}
			tid++
		}
	}
	// MinTbAddrZs (6-10).
	shift := s.Log2Ctb - s.Log2MinTb
	tw := w << shift
	th := h << shift
	p.minTbAddrZs = make([]int, tw*th)
	for y := 0; y < th; y++ {
		for x := 0; x < tw; x++ {
			tbX := (x << s.Log2MinTb) >> s.Log2Ctb
			tbY := (y << s.Log2MinTb) >> s.Log2Ctb
			ctbAddrRs := w*tbY + tbX
			v := p.ctbRsToTs[ctbAddrRs] << (shift * 2)
			for i := 0; i < shift; i++ {
				m := 1 << i
				if m&x != 0 {
					v += m * m
				}
				if m&y != 0 {
					v += 2 * m * m
				}
			}
			p.minTbAddrZs[y*tw+x] = v
		}
	}
}
