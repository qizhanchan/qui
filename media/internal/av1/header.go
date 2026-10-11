package av1

import "fmt"

// Frame types.
const (
	keyFrame       = 0
	interFrame     = 1
	intraOnlyFrame = 2
	switchFrame    = 3
)

// Reference frames.
const (
	intraFrame   = 0
	lastFrame    = 1
	last2Frame   = 2
	last3Frame   = 3
	goldenFrame  = 4
	bwdrefFrame  = 5
	altref2Frame = 6
	altrefFrame  = 7
	numRefFrames = 8
	refsPerFrame = 7

	primaryRefNone = 7
)

// Interpolation filters.
const (
	filterEightTap       = 0
	filterEightTapSmooth = 1
	filterEightTapSharp  = 2
	filterBilinear       = 3
	filterSwitchable     = 4
)

// Global motion types.
const (
	gmIdentity    = 0
	gmTranslation = 1
	gmRotZoom     = 2
	gmAffine      = 3

	warpedModelPrecBits = 16
)

// Segmentation features.
const (
	segLvlAltQ     = 0
	segLvlAltLFYV  = 1
	segLvlRefFrame = 5
	segLvlSkip     = 6
	segLvlGlobalMV = 7
	segLvlMax      = 8
	maxSegments    = 8
)

var (
	segFeatureBits   = [segLvlMax]int{8, 6, 6, 6, 6, 3, 0, 0}
	segFeatureSigned = [segLvlMax]bool{true, true, true, true, true, false, false, false}
	segFeatureMax    = [segLvlMax]int{255, 63, 63, 63, 63, 7, 0, 0}
)

// Restoration types.
const (
	restoreNone       = 0
	restoreWiener     = 1
	restoreSgrproj    = 2
	restoreSwitchable = 3
)

// TX modes.
const (
	txModeOnly4x4 = 0
	txModeLargest = 1
	txModeSelect  = 2
)

// segmentation holds the segmentation parameters (spec 5.9.14).
type segmentation struct {
	enabled        bool
	updateMap      bool
	temporalUpdate bool
	updateData     bool
	featureEnabled [maxSegments][segLvlMax]bool
	featureData    [maxSegments][segLvlMax]int
	preSkip        bool
	lastActive     int
}

// loopFilterParams holds the deblocking parameters (spec 5.9.11).
type loopFilterParams struct {
	level        [4]int
	sharpness    int
	deltaEnabled bool
	refDeltas    [numRefFrames]int
	modeDeltas   [2]int
}

func (l *loopFilterParams) setDefaultDeltas() {
	l.refDeltas = [numRefFrames]int{1, 0, 0, 0, -1, 0, -1, -1}
	l.modeDeltas = [2]int{}
}

// filmGrain holds the film grain synthesis parameters (spec 5.9.30).
type filmGrain struct {
	apply                 bool
	seed                  int
	update                bool
	numY                  int
	pointY                [16][2]int // value, scaling
	chromaScalingFromLuma bool
	numCb, numCr          int
	pointCb, pointCr      [16][2]int
	scalingShift          int // grain_scaling_minus_8 + 8
	arLag                 int
	arY                   [24]int // ar_coeffs_y_plus_128 - 128
	arCb, arCr            [25]int
	arShift               int // ar_coeff_shift_minus_6 + 6
	grainScaleShift       int
	cbMult, cbLumaMult    int
	cbOffset              int
	crMult, crLumaMult    int
	crOffset              int
	overlap               bool
	clipRestricted        bool
}

// tileInfo is the tile layout (spec 5.9.15).
type tileInfo struct {
	cols, rows         int
	colsLog2, rowsLog2 int
	miColStarts        []int // cols+1 entries
	miRowStarts        []int // rows+1 entries
	contextUpdateID    int
	sizeBytes          int
}

// FrameHeader is a parsed uncompressed frame header (spec 5.9) plus the
// values derived from it.
type FrameHeader struct {
	ShowExistingFrame bool
	FrameToShow       int
	FrameType         int
	ShowFrame         bool
	Showable          bool
	ErrorResilient    bool
	intra             bool // FrameIsIntra

	disableCDFUpdate      bool
	allowScreenContent    bool
	forceIntegerMV        bool
	currentFrameID        int
	frameSizeOverride     bool
	OrderHint             int
	primaryRefFrame       int
	refreshFlags          int
	refFrameIdx           [refsPerFrame]int
	allowHighPrecisionMV  bool
	interpFilter          int
	switchableMotionMode  bool
	useRefFrameMVs        bool
	allowIntraBC          bool
	disableFrameEndUpdate bool

	Width, Height    int // FrameWidth / FrameHeight (coded, pre-superres)
	UpscaledWidth    int
	RenderWidth      int
	RenderHeight     int
	superresDenom    int
	miCols, miRows   int
	orderHints       [numRefFrames]int
	refFrameSignBias [numRefFrames]bool

	tiles tileInfo

	baseQIdx             int
	deltaQYDc            int
	deltaQUDc, deltaQUAc int
	deltaQVDc, deltaQVAc int
	usingQMatrix         bool
	qmY, qmU, qmV        int
	seg                  segmentation
	deltaQPresent        bool
	deltaQRes            int // log2
	deltaLFPresent       bool
	deltaLFRes           int // log2
	deltaLFMulti         bool
	codedLossless        bool
	allLossless          bool
	lossless             [maxSegments]bool
	segQMLevel           [3][maxSegments]int
	lf                   loopFilterParams
	cdefDamping          int
	cdefBits             int
	cdefYPri, cdefYSec   [8]int
	cdefUVPri, cdefUVSec [8]int
	lrType               [3]int
	usesLR               bool
	lrUnitSize           [3]int
	txMode               int
	referenceSelect      bool
	skipModePresent      bool
	skipModeFrame        [2]int
	allowWarpedMotion    bool
	reducedTxSet         bool
	gmType               [numRefFrames]int
	gmParams             [numRefFrames][6]int32
	grain                filmGrain

	headerBytes int // bytes of the uncompressed header (frame OBU)
}

// refSlot is one of the eight reference frame slots (spec 7.20).
type refSlot struct {
	valid          bool
	frameID        int
	upscaledWidth  int
	frameWidth     int
	frameHeight    int
	renderWidth    int
	renderHeight   int
	miCols, miRows int
	frameType      int
	orderHint      int
	savedOrderHint [numRefFrames]int
	gmParams       [numRefFrames][6]int32
	lf             loopFilterParams
	seg            segmentation
	grain          filmGrain
	showable       bool
	bitDepth       int

	pic *frame // reconstructed picture, motion field, segment ids, CDFs
}

func defaultGmParams() (p [numRefFrames][6]int32) {
	for i := range p {
		p[i][2] = 1 << warpedModelPrecBits
		p[i][5] = 1 << warpedModelPrecBits
	}
	return
}

// relDist is get_relative_dist.
func (s *SequenceHeader) relDist(a, b int) int {
	if !s.EnableOrderHint {
		return 0
	}
	diff := a - b
	m := 1 << (s.OrderHintBits - 1)
	return (diff & (m - 1)) - (diff & m)
}

// parseFrameHeader parses uncompressed_header() (spec 5.9.2). d supplies
// the sequence header and the reference slot state; temporal and spatial
// IDs come from the OBU extension.
func (d *Decoder) parseFrameHeader(r *bitReader, temporalID, spatialID int) (*FrameHeader, error) {
	s := d.seq
	if s == nil {
		return nil, fmt.Errorf("%w: frame header before sequence header", ErrInvalid)
	}
	h := &FrameHeader{}
	allFrames := (1 << numRefFrames) - 1
	if s.ReducedStillHeader {
		h.FrameType = keyFrame
		h.intra = true
		h.ShowFrame = true
	} else {
		h.ShowExistingFrame = r.flag()
		if h.ShowExistingFrame {
			h.FrameToShow = r.f(3)
			if s.DecoderModelPresent && !s.EqualPictureInterval {
				r.f(s.presentationTimeLen)
			}
			if s.FrameIDNumbersPresent {
				r.f(s.frameIDLen)
			}
			slot := &d.ref[h.FrameToShow]
			if !slot.valid {
				return nil, fmt.Errorf("%w: show_existing_frame of an empty slot", ErrInvalid)
			}
			h.FrameType = slot.frameType
			if h.FrameType == keyFrame {
				h.refreshFlags = allFrames
			}
			if s.FilmGrainPresent {
				h.grain = slot.grain
			}
			return h, nil
		}
		h.FrameType = r.f(2)
		h.intra = h.FrameType == intraOnlyFrame || h.FrameType == keyFrame
		h.ShowFrame = r.flag()
		if h.ShowFrame && s.DecoderModelPresent && !s.EqualPictureInterval {
			r.f(s.presentationTimeLen)
		}
		if h.ShowFrame {
			h.Showable = h.FrameType != keyFrame
		} else {
			h.Showable = r.flag()
		}
		if h.FrameType == switchFrame || (h.FrameType == keyFrame && h.ShowFrame) {
			h.ErrorResilient = true
		} else {
			h.ErrorResilient = r.flag()
		}
	}
	if h.FrameType == keyFrame && h.ShowFrame {
		for i := range d.ref {
			d.ref[i].valid = false
			d.ref[i].orderHint = 0
		}
	}
	h.disableCDFUpdate = r.flag()
	if s.ForceScreenContent == selectScreenContentTools {
		h.allowScreenContent = r.flag()
	} else {
		h.allowScreenContent = s.ForceScreenContent != 0
	}
	if h.allowScreenContent {
		if s.ForceIntegerMV == selectIntegerMV {
			h.forceIntegerMV = r.flag()
		} else {
			h.forceIntegerMV = s.ForceIntegerMV != 0
		}
	}
	if h.intra {
		h.forceIntegerMV = true
	}
	if s.FrameIDNumbersPresent {
		h.currentFrameID = r.f(s.frameIDLen)
		// mark_ref_frames: slots too far behind the current ID are
		// invalid.
		diffLen := s.deltaFrameIDLen
		for i := range d.ref {
			id := d.ref[i].frameID
			if h.currentFrameID > 1<<diffLen {
				if id > h.currentFrameID || id < h.currentFrameID-(1<<diffLen) {
					d.ref[i].valid = false
				}
			} else if id > h.currentFrameID && id < (1<<s.frameIDLen)+h.currentFrameID-(1<<diffLen) {
				d.ref[i].valid = false
			}
		}
	}
	switch {
	case h.FrameType == switchFrame:
		h.frameSizeOverride = true
	case s.ReducedStillHeader:
	default:
		h.frameSizeOverride = r.flag()
	}
	h.OrderHint = r.f(s.OrderHintBits)
	if h.intra || h.ErrorResilient {
		h.primaryRefFrame = primaryRefNone
	} else {
		h.primaryRefFrame = r.f(3)
	}
	if s.DecoderModelPresent {
		if r.flag() { // buffer_removal_time_present_flag
			for op := 0; op < s.OperatingPoints; op++ {
				if !s.opDecoderModel[op] {
					continue
				}
				idc := s.opIdc[op]
				inT := (idc>>temporalID)&1 != 0
				inS := (idc>>(spatialID+8))&1 != 0
				if idc == 0 || (inT && inS) {
					r.f(s.bufferRemovalLen)
				}
			}
		}
	}
	if h.FrameType == switchFrame || (h.FrameType == keyFrame && h.ShowFrame) {
		h.refreshFlags = allFrames
	} else {
		h.refreshFlags = r.f(8)
	}
	if (!h.intra || h.refreshFlags != allFrames) && h.ErrorResilient && s.EnableOrderHint {
		for i := 0; i < numRefFrames; i++ {
			hint := r.f(s.OrderHintBits)
			if hint != d.ref[i].orderHint || !d.ref[i].valid {
				d.invalidateSlot(i, hint)
			}
		}
	}
	if h.intra {
		d.frameSize(r, h)
		d.renderSize(r, h)
		if h.allowScreenContent && h.UpscaledWidth == h.Width {
			h.allowIntraBC = r.flag()
		}
	} else {
		shortSignaling := false
		if s.EnableOrderHint {
			shortSignaling = r.flag()
			if shortSignaling {
				last := r.f(3)
				gold := r.f(3)
				d.setFrameRefs(h, last, gold)
			}
		}
		for i := 0; i < refsPerFrame; i++ {
			if !shortSignaling {
				h.refFrameIdx[i] = r.f(3)
			}
			if s.FrameIDNumbersPresent {
				r.f(s.deltaFrameIDLen)
			}
		}
		for i := 0; i < refsPerFrame; i++ {
			if !d.ref[h.refFrameIdx[i]].valid {
				return nil, fmt.Errorf("%w: reference %d uses an empty slot", ErrInvalid, i)
			}
		}
		if h.frameSizeOverride && !h.ErrorResilient {
			d.frameSizeWithRefs(r, h)
		} else {
			d.frameSize(r, h)
			d.renderSize(r, h)
		}
		if !h.forceIntegerMV {
			h.allowHighPrecisionMV = r.flag()
		}
		if r.flag() {
			h.interpFilter = filterSwitchable
		} else {
			h.interpFilter = r.f(2)
		}
		h.switchableMotionMode = r.flag()
		if !h.ErrorResilient && s.EnableRefFrameMVs {
			h.useRefFrameMVs = r.flag()
		}
		for i := 0; i < refsPerFrame; i++ {
			hint := d.ref[h.refFrameIdx[i]].orderHint
			h.orderHints[lastFrame+i] = hint
			h.refFrameSignBias[lastFrame+i] = s.EnableOrderHint && s.relDist(hint, h.OrderHint) > 0
		}
	}
	if s.ReducedStillHeader || h.disableCDFUpdate {
		h.disableFrameEndUpdate = true
	} else {
		h.disableFrameEndUpdate = r.flag()
	}
	// setup_past_independence / load_previous.
	prevGm := defaultGmParams()
	if h.primaryRefFrame == primaryRefNone {
		h.lf.setDefaultDeltas()
	} else {
		p := &d.ref[h.refFrameIdx[h.primaryRefFrame]]
		prevGm = p.gmParams
		h.lf = p.lf
		h.seg = p.seg
	}
	if err := d.parseTileInfo(r, h); err != nil {
		return nil, err
	}
	d.quantizationParams(r, h)
	d.segmentationParams(r, h)
	if h.baseQIdx > 0 {
		h.deltaQPresent = r.flag()
	}
	if h.deltaQPresent {
		h.deltaQRes = r.f(2)
		if !h.allowIntraBC {
			h.deltaLFPresent = r.flag()
		}
		if h.deltaLFPresent {
			h.deltaLFRes = r.f(2)
			h.deltaLFMulti = r.flag()
		}
	}
	h.codedLossless = true
	for id := 0; id < maxSegments; id++ {
		q := h.qindex(true, id, 0)
		h.lossless[id] = q == 0 && h.deltaQYDc == 0 && h.deltaQUAc == 0 && h.deltaQUDc == 0 && h.deltaQVAc == 0 && h.deltaQVDc == 0
		if !h.lossless[id] {
			h.codedLossless = false
		}
		if h.usingQMatrix {
			if h.lossless[id] {
				h.segQMLevel[0][id], h.segQMLevel[1][id], h.segQMLevel[2][id] = 15, 15, 15
			} else {
				h.segQMLevel[0][id], h.segQMLevel[1][id], h.segQMLevel[2][id] = h.qmY, h.qmU, h.qmV
			}
		}
	}
	h.allLossless = h.codedLossless && h.Width == h.UpscaledWidth
	d.loopFilterParams(r, h)
	d.cdefParams(r, h)
	d.lrParams(r, h)
	if h.codedLossless {
		h.txMode = txModeOnly4x4
	} else if r.flag() {
		h.txMode = txModeSelect
	} else {
		h.txMode = txModeLargest
	}
	if !h.intra {
		h.referenceSelect = r.flag()
	}
	d.skipModeParams(r, h)
	if !h.intra && !h.ErrorResilient && s.EnableWarpedMotion {
		h.allowWarpedMotion = r.flag()
	}
	h.reducedTxSet = r.flag()
	d.globalMotionParams(r, h, &prevGm)
	d.filmGrainParams(r, h)
	if r.overrun() {
		return nil, fmt.Errorf("%w: truncated frame header", ErrInvalid)
	}
	return h, nil
}

// invalidateSlot handles a reference whose expected order hint differs
// in error-resilient mode: the slot is marked invalid with that hint.
func (d *Decoder) invalidateSlot(i, hint int) {
	d.ref[i].valid = false
	d.ref[i].orderHint = hint
}

func (d *Decoder) superresParams(r *bitReader, h *FrameHeader) {
	h.superresDenom = 8
	if d.seq.EnableSuperres && r.flag() {
		h.superresDenom = r.f(3) + 9
	}
	h.UpscaledWidth = h.Width
	h.Width = (h.UpscaledWidth*8 + h.superresDenom/2) / h.superresDenom
}

func (h *FrameHeader) computeImageSize() {
	h.miCols = 2 * ((h.Width + 7) >> 3)
	h.miRows = 2 * ((h.Height + 7) >> 3)
}

func (d *Decoder) frameSize(r *bitReader, h *FrameHeader) {
	s := d.seq
	if h.frameSizeOverride {
		h.Width = r.f(s.frameWidthBits) + 1
		h.Height = r.f(s.frameHeightBits) + 1
	} else {
		h.Width, h.Height = s.MaxWidth, s.MaxHeight
	}
	d.superresParams(r, h)
	h.computeImageSize()
}

func (d *Decoder) renderSize(r *bitReader, h *FrameHeader) {
	if r.flag() {
		h.RenderWidth = r.f(16) + 1
		h.RenderHeight = r.f(16) + 1
	} else {
		h.RenderWidth, h.RenderHeight = h.UpscaledWidth, h.Height
	}
}

func (d *Decoder) frameSizeWithRefs(r *bitReader, h *FrameHeader) {
	for i := 0; i < refsPerFrame; i++ {
		if r.flag() {
			p := &d.ref[h.refFrameIdx[i]]
			h.UpscaledWidth = p.upscaledWidth
			h.Width = h.UpscaledWidth
			h.Height = p.frameHeight
			h.RenderWidth, h.RenderHeight = p.renderWidth, p.renderHeight
			d.superresParams(r, h)
			h.computeImageSize()
			return
		}
	}
	d.frameSize(r, h)
	d.renderSize(r, h)
}

// setFrameRefs is the frame_refs_short_signaling derivation (spec 7.8).
func (d *Decoder) setFrameRefs(h *FrameHeader, last, gold int) {
	s := d.seq
	for i := range h.refFrameIdx {
		h.refFrameIdx[i] = -1
	}
	h.refFrameIdx[0] = last
	h.refFrameIdx[goldenFrame-lastFrame] = gold
	var used [numRefFrames]bool
	used[last], used[gold] = true, true
	cur := 1 << (s.OrderHintBits - 1)
	var shifted [numRefFrames]int
	for i := range shifted {
		shifted[i] = cur + s.relDist(d.ref[i].orderHint, h.OrderHint)
	}
	pick := func(backward, latest bool) int {
		ref, best := -1, 0
		for i := 0; i < numRefFrames; i++ {
			hint := shifted[i]
			if used[i] || (hint >= cur) != backward {
				continue
			}
			if ref < 0 || (latest && hint >= best) || (!latest && hint < best) {
				ref, best = i, hint
			}
		}
		return ref
	}
	if ref := pick(true, true); ref >= 0 {
		h.refFrameIdx[altrefFrame-lastFrame], used[ref] = ref, true
	}
	if ref := pick(true, false); ref >= 0 {
		h.refFrameIdx[bwdrefFrame-lastFrame], used[ref] = ref, true
	}
	if ref := pick(true, false); ref >= 0 {
		h.refFrameIdx[altref2Frame-lastFrame], used[ref] = ref, true
	}
	for _, rf := range [...]int{last2Frame, last3Frame, bwdrefFrame, altref2Frame, altrefFrame} {
		if h.refFrameIdx[rf-lastFrame] < 0 {
			if ref := pick(false, true); ref >= 0 {
				h.refFrameIdx[rf-lastFrame], used[ref] = ref, true
			}
		}
	}
	ref, earliest := -1, 0
	for i := 0; i < numRefFrames; i++ {
		if ref < 0 || shifted[i] < earliest {
			ref, earliest = i, shifted[i]
		}
	}
	for i := range h.refFrameIdx {
		if h.refFrameIdx[i] < 0 {
			h.refFrameIdx[i] = ref
		}
	}
}

func tileLog2(blk, target int) int {
	k := 0
	for blk<<k < target {
		k++
	}
	return k
}

func (d *Decoder) parseTileInfo(r *bitReader, h *FrameHeader) error {
	s := d.seq
	sbShift := 4
	if s.Use128x128 {
		sbShift = 5
	}
	sbCols := (h.miCols + (1 << sbShift) - 1) >> sbShift
	sbRows := (h.miRows + (1 << sbShift) - 1) >> sbShift
	sbSize := sbShift + 2
	maxTileWidthSb := 4096 >> sbSize
	maxTileAreaSb := (4096 * 2304) >> (2 * sbSize)
	minLog2TileCols := tileLog2(maxTileWidthSb, sbCols)
	maxLog2TileCols := tileLog2(1, min(sbCols, 64))
	maxLog2TileRows := tileLog2(1, min(sbRows, 64))
	minLog2Tiles := max(minLog2TileCols, tileLog2(maxTileAreaSb, sbRows*sbCols))
	t := &h.tiles
	t.miColStarts, t.miRowStarts = t.miColStarts[:0], t.miRowStarts[:0]
	if r.flag() { // uniform_tile_spacing_flag
		t.colsLog2 = minLog2TileCols
		for t.colsLog2 < maxLog2TileCols && r.flag() {
			t.colsLog2++
		}
		w := (sbCols + (1 << t.colsLog2) - 1) >> t.colsLog2
		for start := 0; start < sbCols; start += w {
			t.miColStarts = append(t.miColStarts, start<<sbShift)
		}
		t.miColStarts = append(t.miColStarts, h.miCols)
		t.cols = len(t.miColStarts) - 1
		minLog2TileRows := max(minLog2Tiles-t.colsLog2, 0)
		t.rowsLog2 = minLog2TileRows
		for t.rowsLog2 < maxLog2TileRows && r.flag() {
			t.rowsLog2++
		}
		ht := (sbRows + (1 << t.rowsLog2) - 1) >> t.rowsLog2
		for start := 0; start < sbRows; start += ht {
			t.miRowStarts = append(t.miRowStarts, start<<sbShift)
		}
		t.miRowStarts = append(t.miRowStarts, h.miRows)
		t.rows = len(t.miRowStarts) - 1
	} else {
		widest := 0
		for start := 0; start < sbCols; {
			t.miColStarts = append(t.miColStarts, start<<sbShift)
			size := r.ns(min(sbCols-start, maxTileWidthSb)) + 1
			widest = max(widest, size)
			start += size
		}
		t.miColStarts = append(t.miColStarts, h.miCols)
		t.cols = len(t.miColStarts) - 1
		t.colsLog2 = tileLog2(1, t.cols)
		area := sbRows * sbCols
		if minLog2Tiles > 0 {
			area >>= minLog2Tiles + 1
		}
		maxTileHeightSb := max(area/widest, 1)
		for start := 0; start < sbRows; {
			t.miRowStarts = append(t.miRowStarts, start<<sbShift)
			size := r.ns(min(sbRows-start, maxTileHeightSb)) + 1
			start += size
		}
		t.miRowStarts = append(t.miRowStarts, h.miRows)
		t.rows = len(t.miRowStarts) - 1
		t.rowsLog2 = tileLog2(1, t.rows)
	}
	if t.cols > 64 || t.rows > 64 {
		return fmt.Errorf("%w: %dx%d tiles", ErrInvalid, t.cols, t.rows)
	}
	t.contextUpdateID, t.sizeBytes = 0, 0
	if t.colsLog2 > 0 || t.rowsLog2 > 0 {
		t.contextUpdateID = r.f(t.rowsLog2 + t.colsLog2)
		t.sizeBytes = r.f(2) + 1
	}
	return nil
}

func readDeltaQ(r *bitReader) int {
	if r.flag() {
		return r.su(7)
	}
	return 0
}

func (d *Decoder) quantizationParams(r *bitReader, h *FrameHeader) {
	s := d.seq
	h.baseQIdx = r.f(8)
	h.deltaQYDc = readDeltaQ(r)
	if s.NumPlanes() > 1 {
		diffUV := false
		if s.SeparateUVDeltaQ {
			diffUV = r.flag()
		}
		h.deltaQUDc = readDeltaQ(r)
		h.deltaQUAc = readDeltaQ(r)
		if diffUV {
			h.deltaQVDc = readDeltaQ(r)
			h.deltaQVAc = readDeltaQ(r)
		} else {
			h.deltaQVDc, h.deltaQVAc = h.deltaQUDc, h.deltaQUAc
		}
	}
	h.usingQMatrix = r.flag()
	if h.usingQMatrix {
		h.qmY = r.f(4)
		h.qmU = r.f(4)
		if !s.SeparateUVDeltaQ {
			h.qmV = h.qmU
		} else {
			h.qmV = r.f(4)
		}
	}
}

func (d *Decoder) segmentationParams(r *bitReader, h *FrameHeader) {
	g := &h.seg
	g.enabled = r.flag()
	if g.enabled {
		if h.primaryRefFrame == primaryRefNone {
			g.updateMap, g.temporalUpdate, g.updateData = true, false, true
		} else {
			g.updateMap = r.flag()
			g.temporalUpdate = false
			if g.updateMap {
				g.temporalUpdate = r.flag()
			}
			g.updateData = r.flag()
		}
		if g.updateData {
			for i := 0; i < maxSegments; i++ {
				for j := 0; j < segLvlMax; j++ {
					v := 0
					g.featureEnabled[i][j] = r.flag()
					if g.featureEnabled[i][j] {
						lim := segFeatureMax[j]
						if segFeatureSigned[j] {
							v = clip3(-lim, lim, r.su(1+segFeatureBits[j]))
						} else {
							v = clip3(0, lim, r.f(segFeatureBits[j]))
						}
					}
					g.featureData[i][j] = v
				}
			}
		}
	} else {
		g.featureEnabled = [maxSegments][segLvlMax]bool{}
		g.featureData = [maxSegments][segLvlMax]int{}
		g.updateMap, g.temporalUpdate, g.updateData = false, false, false
	}
	g.preSkip, g.lastActive = false, 0
	for i := 0; i < maxSegments; i++ {
		for j := 0; j < segLvlMax; j++ {
			if g.featureEnabled[i][j] {
				g.lastActive = i
				if j >= segLvlRefFrame {
					g.preSkip = true
				}
			}
		}
	}
}

// qindex is get_qindex (spec 7.12.2): ignoreDeltaQ selects the frame's
// base index over the block's current one.
func (h *FrameHeader) qindex(ignoreDeltaQ bool, segID, current int) int {
	if h.seg.enabled && h.seg.featureEnabled[segID][segLvlAltQ] {
		q := h.baseQIdx + h.seg.featureData[segID][segLvlAltQ]
		if !ignoreDeltaQ && h.deltaQPresent {
			q = current + h.seg.featureData[segID][segLvlAltQ]
		}
		return clip3(0, 255, q)
	}
	if !ignoreDeltaQ && h.deltaQPresent {
		return current
	}
	return h.baseQIdx
}

func (d *Decoder) loopFilterParams(r *bitReader, h *FrameHeader) {
	l := &h.lf
	if h.codedLossless || h.allowIntraBC {
		l.level = [4]int{}
		l.setDefaultDeltas()
		return
	}
	l.level[0] = r.f(6)
	l.level[1] = r.f(6)
	l.level[2], l.level[3] = 0, 0
	if d.seq.NumPlanes() > 1 && (l.level[0] != 0 || l.level[1] != 0) {
		l.level[2] = r.f(6)
		l.level[3] = r.f(6)
	}
	l.sharpness = r.f(3)
	l.deltaEnabled = r.flag()
	if l.deltaEnabled && r.flag() { // loop_filter_delta_update
		for i := 0; i < numRefFrames; i++ {
			if r.flag() {
				l.refDeltas[i] = r.su(7)
			}
		}
		for i := 0; i < 2; i++ {
			if r.flag() {
				l.modeDeltas[i] = r.su(7)
			}
		}
	}
}

func (d *Decoder) cdefParams(r *bitReader, h *FrameHeader) {
	if h.codedLossless || h.allowIntraBC || !d.seq.EnableCDEF {
		h.cdefBits = 0
		h.cdefYPri[0], h.cdefYSec[0], h.cdefUVPri[0], h.cdefUVSec[0] = 0, 0, 0, 0
		h.cdefDamping = 3
		return
	}
	h.cdefDamping = r.f(2) + 3
	h.cdefBits = r.f(2)
	for i := 0; i < 1<<h.cdefBits; i++ {
		h.cdefYPri[i] = r.f(4)
		h.cdefYSec[i] = r.f(2)
		if h.cdefYSec[i] == 3 {
			h.cdefYSec[i]++
		}
		if d.seq.NumPlanes() > 1 {
			h.cdefUVPri[i] = r.f(4)
			h.cdefUVSec[i] = r.f(2)
			if h.cdefUVSec[i] == 3 {
				h.cdefUVSec[i]++
			}
		}
	}
}

func (d *Decoder) lrParams(r *bitReader, h *FrameHeader) {
	s := d.seq
	h.lrType = [3]int{}
	h.usesLR = false
	if h.allLossless || h.allowIntraBC || !s.EnableRestoration {
		return
	}
	remap := [4]int{restoreNone, restoreSwitchable, restoreWiener, restoreSgrproj}
	usesChroma := false
	for i := 0; i < s.NumPlanes(); i++ {
		h.lrType[i] = remap[r.f(2)]
		if h.lrType[i] != restoreNone {
			h.usesLR = true
			if i > 0 {
				usesChroma = true
			}
		}
	}
	if !h.usesLR {
		return
	}
	shift := 0
	if s.Use128x128 {
		shift = r.f(1) + 1
	} else {
		shift = r.f(1)
		if shift != 0 {
			shift += r.f(1)
		}
	}
	h.lrUnitSize[0] = 256 >> (2 - shift)
	uvShift := 0
	if s.SubX != 0 && s.SubY != 0 && usesChroma {
		uvShift = r.f(1)
	}
	h.lrUnitSize[1] = h.lrUnitSize[0] >> uvShift
	h.lrUnitSize[2] = h.lrUnitSize[0] >> uvShift
}

func (d *Decoder) skipModeParams(r *bitReader, h *FrameHeader) {
	s := d.seq
	allowed := false
	if !h.intra && h.referenceSelect && s.EnableOrderHint {
		fwd, bwd := -1, -1
		var fwdHint, bwdHint int
		for i := 0; i < refsPerFrame; i++ {
			hint := d.ref[h.refFrameIdx[i]].orderHint
			if s.relDist(hint, h.OrderHint) < 0 {
				if fwd < 0 || s.relDist(hint, fwdHint) > 0 {
					fwd, fwdHint = i, hint
				}
			} else if s.relDist(hint, h.OrderHint) > 0 {
				if bwd < 0 || s.relDist(hint, bwdHint) < 0 {
					bwd, bwdHint = i, hint
				}
			}
		}
		switch {
		case fwd < 0:
		case bwd >= 0:
			allowed = true
			h.skipModeFrame = [2]int{lastFrame + min(fwd, bwd), lastFrame + max(fwd, bwd)}
		default:
			fwd2 := -1
			var fwd2Hint int
			for i := 0; i < refsPerFrame; i++ {
				hint := d.ref[h.refFrameIdx[i]].orderHint
				if s.relDist(hint, fwdHint) < 0 {
					if fwd2 < 0 || s.relDist(hint, fwd2Hint) > 0 {
						fwd2, fwd2Hint = i, hint
					}
				}
			}
			if fwd2 >= 0 {
				allowed = true
				h.skipModeFrame = [2]int{lastFrame + min(fwd, fwd2), lastFrame + max(fwd, fwd2)}
			}
		}
	}
	if allowed {
		h.skipModePresent = r.flag()
	}
}

func (d *Decoder) globalMotionParams(r *bitReader, h *FrameHeader, prev *[numRefFrames][6]int32) {
	h.gmParams = defaultGmParams()
	for i := range h.gmType {
		h.gmType[i] = gmIdentity
	}
	if h.intra {
		return
	}
	for ref := lastFrame; ref <= altrefFrame; ref++ {
		typ := gmIdentity
		if r.flag() {
			if r.flag() {
				typ = gmRotZoom
			} else if r.flag() {
				typ = gmTranslation
			} else {
				typ = gmAffine
			}
		}
		h.gmType[ref] = typ
		if typ >= gmRotZoom {
			d.readGlobalParam(r, h, prev, typ, ref, 2)
			d.readGlobalParam(r, h, prev, typ, ref, 3)
			if typ == gmAffine {
				d.readGlobalParam(r, h, prev, typ, ref, 4)
				d.readGlobalParam(r, h, prev, typ, ref, 5)
			} else {
				h.gmParams[ref][4] = -h.gmParams[ref][3]
				h.gmParams[ref][5] = h.gmParams[ref][2]
			}
		}
		if typ >= gmTranslation {
			d.readGlobalParam(r, h, prev, typ, ref, 0)
			d.readGlobalParam(r, h, prev, typ, ref, 1)
		}
	}
}

func (d *Decoder) readGlobalParam(r *bitReader, h *FrameHeader, prev *[numRefFrames][6]int32, typ, ref, idx int) {
	absBits, precBits := 12, 15
	if idx < 2 {
		if typ == gmTranslation {
			hp := 0
			if !h.allowHighPrecisionMV {
				hp = 1
			}
			absBits, precBits = 9-hp, 3-hp
		} else {
			absBits, precBits = 12, 6
		}
	}
	precDiff := warpedModelPrecBits - precBits
	round, sub := 0, 0
	if idx%3 == 2 {
		round, sub = 1<<warpedModelPrecBits, 1<<precBits
	}
	mx := 1 << absBits
	ref0 := int(prev[ref][idx]>>precDiff) - sub
	v := decodeSignedSubexpWithRef(r, -mx, mx+1, ref0)
	h.gmParams[ref][idx] = int32(v<<precDiff + round)
}

func decodeSignedSubexpWithRef(r *bitReader, low, high, ref int) int {
	return decodeUnsignedSubexpWithRef(r, high-low, ref-low) + low
}

func decodeUnsignedSubexpWithRef(r *bitReader, mx, ref int) int {
	v := decodeSubexp(r, mx)
	if ref<<1 <= mx {
		return inverseRecenter(ref, v)
	}
	return mx - 1 - inverseRecenter(mx-1-ref, v)
}

func decodeSubexp(r *bitReader, numSyms int) int {
	i, mk, k := 0, 0, 3
	for {
		b2 := k
		if i != 0 {
			b2 = k + i - 1
		}
		a := 1 << b2
		if numSyms <= mk+3*a {
			return r.ns(numSyms-mk) + mk
		}
		if !r.flag() {
			return r.f(b2) + mk
		}
		i++
		mk += a
	}
}

func inverseRecenter(r, v int) int {
	switch {
	case v > 2*r:
		return v
	case v&1 != 0:
		return r - ((v + 1) >> 1)
	}
	return r + (v >> 1)
}

func (d *Decoder) filmGrainParams(r *bitReader, h *FrameHeader) {
	s := d.seq
	g := &h.grain
	*g = filmGrain{}
	if !s.FilmGrainPresent || (!h.ShowFrame && !h.Showable) {
		return
	}
	g.apply = r.flag()
	if !g.apply {
		return
	}
	g.seed = r.f(16)
	g.update = true
	if h.FrameType == interFrame {
		g.update = r.flag()
	}
	if !g.update {
		idx := r.f(3)
		seed := g.seed
		*g = d.ref[idx].grain
		g.seed = seed
		return
	}
	g.numY = r.f(4)
	for i := 0; i < g.numY; i++ {
		g.pointY[i] = [2]int{r.f(8), r.f(8)}
	}
	if !s.MonoChrome {
		g.chromaScalingFromLuma = r.flag()
	}
	if !(s.MonoChrome || g.chromaScalingFromLuma || (s.SubX == 1 && s.SubY == 1 && g.numY == 0)) {
		g.numCb = r.f(4)
		for i := 0; i < g.numCb; i++ {
			g.pointCb[i] = [2]int{r.f(8), r.f(8)}
		}
		g.numCr = r.f(4)
		for i := 0; i < g.numCr; i++ {
			g.pointCr[i] = [2]int{r.f(8), r.f(8)}
		}
	}
	g.scalingShift = r.f(2) + 8
	g.arLag = r.f(2)
	numPosLuma := 2 * g.arLag * (g.arLag + 1)
	numPosChroma := numPosLuma
	if g.numY != 0 {
		numPosChroma++
		for i := 0; i < numPosLuma; i++ {
			g.arY[i] = r.f(8) - 128
		}
	}
	if g.chromaScalingFromLuma || g.numCb != 0 {
		for i := 0; i < numPosChroma; i++ {
			g.arCb[i] = r.f(8) - 128
		}
	}
	if g.chromaScalingFromLuma || g.numCr != 0 {
		for i := 0; i < numPosChroma; i++ {
			g.arCr[i] = r.f(8) - 128
		}
	}
	g.arShift = r.f(2) + 6
	g.grainScaleShift = r.f(2)
	if g.numCb != 0 {
		g.cbMult = r.f(8)
		g.cbLumaMult = r.f(8)
		g.cbOffset = r.f(9)
	}
	if g.numCr != 0 {
		g.crMult = r.f(8)
		g.crLumaMult = r.f(8)
		g.crOffset = r.f(9)
	}
	g.overlap = r.flag()
	g.clipRestricted = r.flag()
}

func clip3(lo, hi, v int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}
