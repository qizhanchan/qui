package vp9

import "errors"

// ErrInvalid reports a malformed stream; ErrUnsupported a valid one this
// decoder does not handle (profiles 1–3: 4:4:4 / 4:2:2 / 4:4:0, 10/12-bit).
var (
	ErrInvalid     = errors.New("vp9: invalid bitstream")
	ErrUnsupported = errors.New("vp9: unsupported stream")
)

const (
	keyFrame   = 0
	interFrame = 1

	only4x4      = 0
	allow8x8     = 1
	allow16x16   = 2
	allow32x32   = 3
	txModeSelect = 4

	filterRegular    = 0
	filterSmooth     = 1
	filterSharp      = 2
	filterBilinear   = 3
	filterSwitchable = 4

	singleReference     = 0
	compoundReference   = 1
	referenceModeSelect = 2

	intraFrame  = 0
	lastFrame   = 1
	goldenFrame = 2
	altrefFrame = 3

	segLvlAltQ     = 0
	segLvlAltLF    = 1
	segLvlRefFrame = 2
	segLvlSkip     = 3
)

// maxPixels bounds the frame area (8K); tests lower it.
var maxPixels = 8192 * 4352

var txModeToBiggest = [5]int{0, 1, 2, 3, 3}
var literalToFilter = [4]int{filterSmooth, filterRegular, filterSharp, filterBilinear}

type segmentation struct {
	enabled, updateMap, temporalUpdate, updateData, absDelta bool
	treeProbs                                                [7]uint8
	predProbs                                                [3]uint8
	featureMask                                              [8]uint8
	featureData                                              [8][4]int
}

func (s *segmentation) active(id, feature int) bool {
	return s.enabled && s.featureMask[id]&(1<<feature) != 0
}

func (s *segmentation) clearFeatures() {
	s.featureMask = [8]uint8{}
	s.featureData = [8][4]int{}
	s.absDelta = false
}

type loopFilterParams struct {
	level, sharpness int
	deltaEnabled     bool
	refDeltas        [4]int
	modeDeltas       [2]int
}

type frameHeader struct {
	profile                      int
	showExisting                 bool
	showIdx                      int
	frameType                    int
	showFrame, errorRes          bool
	intraOnlyFlag                bool
	resetContext                 int
	refreshFlags                 int
	refIdx                       [3]int
	signBias                     [4]bool
	width, height                int
	allowHP                      bool
	interpFilter                 int
	refreshContext               bool
	parallelMode                 bool
	contextIdx                   int
	baseQ, dqYDC, dqUVDC, dqUVAC int
	lossless                     bool
	tileColsLog2, tileRowsLog2   int
	headerSize                   int
	uncompSize                   int
	txMode                       int
	refMode                      int
	compFixedRef                 int
	compVarRef                   [2]int
	colorRange                   bool
	colorSpace                   int
}

func (h *frameHeader) intraOnly() bool { return h.frameType == keyFrame || h.intraOnlyFlag }

func (h *frameHeader) compoundAllowed() bool {
	for i := 2; i <= 3; i++ {
		if h.signBias[i] != h.signBias[1] {
			return true
		}
	}
	return false
}

func (h *frameHeader) setupCompoundRefs() {
	switch {
	case h.signBias[lastFrame] == h.signBias[goldenFrame]:
		h.compFixedRef = altrefFrame
		h.compVarRef = [2]int{lastFrame, goldenFrame}
	case h.signBias[lastFrame] == h.signBias[altrefFrame]:
		h.compFixedRef = goldenFrame
		h.compVarRef = [2]int{lastFrame, altrefFrame}
	default:
		h.compFixedRef = lastFrame
		h.compVarRef = [2]int{goldenFrame, altrefFrame}
	}
}

// readColorConfig parses color_config for profile 0 and rejects the rest.
func readColorConfig(b *bitReader, h *frameHeader) error {
	if h.profile >= 2 {
		b.bit() // ten_or_twelve_bit
		return ErrUnsupported
	}
	h.colorSpace = b.f(3)
	if h.colorSpace != 7 { // not CS_RGB
		h.colorRange = b.bit() != 0
		if h.profile == 1 {
			return ErrUnsupported
		}
	} else {
		// RGB is 4:4:4: profile 1/3 only.
		return ErrUnsupported
	}
	return nil
}

// parseUncompressed reads the uncompressed header. refSize returns the
// dimensions held by a reference slot (for frame_size_with_refs).
func (dec *Decoder) parseUncompressed(data []byte, h *frameHeader) error {
	b := &bitReader{buf: data}
	if b.f(2) != 2 {
		return ErrInvalid
	}
	h.profile = b.bit() | b.bit()<<1
	if h.profile == 3 {
		b.bit()
	}
	if h.profile != 0 {
		return ErrUnsupported
	}
	h.showExisting = b.bit() != 0
	if h.showExisting {
		h.showIdx = b.f(3)
		h.uncompSize = b.bytePos()
		return nil
	}
	h.frameType = b.bit()
	h.showFrame = b.bit() != 0
	h.errorRes = b.bit() != 0
	if h.frameType == keyFrame {
		if b.f(8) != 0x49 || b.f(8) != 0x83 || b.f(8) != 0x42 {
			return ErrInvalid
		}
		if err := readColorConfig(b, h); err != nil {
			return err
		}
		h.width, h.height = b.f(16)+1, b.f(16)+1
		if b.bit() != 0 { // render_size
			b.f(32)
		}
		h.refreshFlags = 0xff
	} else {
		if !h.showFrame {
			h.intraOnlyFlag = b.bit() != 0
		}
		if !h.errorRes {
			h.resetContext = b.f(2)
		}
		if h.intraOnlyFlag {
			if b.f(8) != 0x49 || b.f(8) != 0x83 || b.f(8) != 0x42 {
				return ErrInvalid
			}
			// profile 0: BT.601 4:2:0 8-bit implied.
			h.refreshFlags = b.f(8)
			h.width, h.height = b.f(16)+1, b.f(16)+1
			if b.bit() != 0 {
				b.f(32)
			}
		} else {
			h.refreshFlags = b.f(8)
			for i := 0; i < 3; i++ {
				h.refIdx[i] = b.f(3)
				h.signBias[lastFrame+i] = b.bit() != 0
			}
			found := false
			for i := 0; i < 3; i++ {
				if b.bit() != 0 {
					r := dec.refs[h.refIdx[i]]
					if r == nil {
						return ErrInvalid
					}
					h.width, h.height = r.w, r.h
					found = true
					break
				}
			}
			if !found {
				h.width, h.height = b.f(16)+1, b.f(16)+1
			}
			if b.bit() != 0 {
				b.f(32)
			}
			h.allowHP = b.bit() != 0
			if b.bit() != 0 {
				h.interpFilter = filterSwitchable
			} else {
				h.interpFilter = literalToFilter[b.f(2)]
			}
		}
	}
	if h.width*h.height > maxPixels {
		return ErrUnsupported
	}
	if !h.errorRes {
		h.refreshContext = b.bit() != 0
		h.parallelMode = b.bit() != 0
	} else {
		h.parallelMode = true
	}
	h.contextIdx = b.f(2)
	if h.intraOnly() || h.errorRes {
		dec.setupPastIndependence(h)
	}
	// loop filter
	lf := &dec.lf
	lf.level = b.f(6)
	lf.sharpness = b.f(3)
	lf.deltaEnabled = b.bit() != 0
	if lf.deltaEnabled && b.bit() != 0 {
		for i := 0; i < 4; i++ {
			if b.bit() != 0 {
				lf.refDeltas[i] = b.su(6)
			}
		}
		for i := 0; i < 2; i++ {
			if b.bit() != 0 {
				lf.modeDeltas[i] = b.su(6)
			}
		}
	}
	// quantization
	h.baseQ = b.f(8)
	rd := func() int {
		if b.bit() != 0 {
			return b.su(4)
		}
		return 0
	}
	h.dqYDC, h.dqUVDC, h.dqUVAC = rd(), rd(), rd()
	h.lossless = h.baseQ == 0 && h.dqYDC == 0 && h.dqUVDC == 0 && h.dqUVAC == 0
	// segmentation
	s := &dec.seg
	s.updateMap, s.updateData, s.temporalUpdate = false, false, false
	s.enabled = b.bit() != 0
	if s.enabled {
		s.updateMap = b.bit() != 0
		if s.updateMap {
			for i := range s.treeProbs {
				s.treeProbs[i] = 255
				if b.bit() != 0 {
					s.treeProbs[i] = uint8(b.f(8))
				}
			}
			s.temporalUpdate = b.bit() != 0
			for i := range s.predProbs {
				s.predProbs[i] = 255
				if s.temporalUpdate && b.bit() != 0 {
					s.predProbs[i] = uint8(b.f(8))
				}
			}
		}
		s.updateData = b.bit() != 0
		if s.updateData {
			s.absDelta = b.bit() != 0
			s.featureMask = [8]uint8{}
			s.featureData = [8][4]int{}
			bits := [4]int{8, 6, 2, 0}
			maxv := [4]int{255, 63, 3, 0}
			signed := [4]bool{true, true, false, false}
			for i := 0; i < 8; i++ {
				for j := 0; j < 4; j++ {
					v := 0
					if b.bit() != 0 {
						s.featureMask[i] |= 1 << j
						v = min(b.f(bits[j]), maxv[j])
						if signed[j] && b.bit() != 0 {
							v = -v
						}
					}
					s.featureData[i][j] = v
				}
			}
		}
	}
	// tiles
	miCols := (h.width + 7) >> 3
	sb64Cols := (miCols + 7) >> 3
	minLog2 := 0
	for 64<<minLog2 < sb64Cols {
		minLog2++
	}
	maxLog2 := 1
	for sb64Cols>>maxLog2 >= 4 {
		maxLog2++
	}
	maxLog2--
	h.tileColsLog2 = minLog2
	for h.tileColsLog2 < maxLog2 {
		if b.bit() == 0 {
			break
		}
		h.tileColsLog2++
	}
	h.tileRowsLog2 = b.bit()
	if h.tileRowsLog2 != 0 {
		h.tileRowsLog2 += b.bit()
	}
	h.headerSize = b.f(16)
	if b.err || h.headerSize == 0 {
		return ErrInvalid
	}
	h.uncompSize = b.bytePos()
	return nil
}

// setupPastIndependence resets the state that intra-only and
// error-resilient frames do not inherit (libvpx vp9_setup_past_independence).
func (dec *Decoder) setupPastIndependence(h *frameHeader) {
	dec.seg.clearFeatures()
	clear(dec.segMapLast)
	dec.lf.refDeltas = [4]int{1, 0, -1, -1}
	dec.lf.modeDeltas = [2]int{0, 0}
	dec.fc = defaultFrameContext()
	if h.frameType == keyFrame || h.errorRes || h.resetContext == 3 {
		for i := range dec.contexts {
			dec.contexts[i] = dec.fc
		}
	} else if h.resetContext == 2 {
		dec.contexts[h.contextIdx] = dec.fc
	}
	h.contextIdx = 0
}
