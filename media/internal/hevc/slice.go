package hevc

// Slice segment header (spec 7.3.6).

const (
	sliceB = 0
	sliceP = 1
	sliceI = 2
)

type predWeight struct {
	lumaDenom, chromaDenom int
	// [list][ref]: luma weight/offset, chroma [cb, cr] weight/offset.
	lw, lo [2][16]int
	cw, co [2][16][2]int
	lFlag  [2][16]bool
	cFlag  [2][16]bool
}

type sliceHeader struct {
	nalType       int
	temporal      int
	firstInPic    bool
	noOutputPrior bool
	pps           *PPS
	sps           *SPS
	dependent     bool
	address       int // slice_segment_address (RS CTB)
	typ           int
	picOutput     bool
	pocLsb        int
	rps           stRPS // the short-term RPS in use
	rpsBits       int
	// long-term entries
	numLT                  int
	ltLsb                  [32]int
	ltMsb                  [32]bool // delta_poc_msb_present_flag
	ltDeltaMsb             [32]int  // DeltaPocMsbCycleLt
	ltUsed                 [32]bool
	temporalMVP            bool
	saoLuma, saoChroma     bool
	numRefIdx              [2]int
	listMod                [2]bool
	listEntry              [2][16]int
	mvdL1Zero              bool
	cabacInit              bool
	colFromL0              bool
	colRefIdx              int
	pw                     predWeight
	hasPW                  bool
	maxMergeCand           int
	qpDelta                int
	cbQPOffset, crQPOffset int
	deblockDisabled        bool
	betaOffset, tcOffset   int
	lfAcrossSlices         bool
	entryPoints            []int
	dataOffset             int // byte offset of slice data in the RBSP

	refList [2][]refEntry
}

// refEntry is one entry of a reference picture list.
type refEntry struct {
	pic *picture
	poc int
	lt  bool
}

func (h *sliceHeader) isIRAP() bool { return h.nalType >= 16 && h.nalType <= 23 }
func (h *sliceHeader) isIDR() bool  { return h.nalType == nalIDRWRADL || h.nalType == nalIDRNLP }

// parseSliceHeader parses the header of a slice segment. prev is the
// previous independent slice header of the picture (for dependent
// segments).
func parseSliceHeader(r *bitReader, nal []byte, spss []*SPS, ppss []*PPS, prev *sliceHeader) (*sliceHeader, error) {
	h := &sliceHeader{nalType: int(nal[0]>>1) & 63, temporal: int(nal[1]&7) - 1}
	h.firstInPic = r.flag()
	if h.isIRAP() {
		h.noOutputPrior = r.flag()
	}
	ppsID := int(r.ue())
	if ppsID > 63 || ppss[ppsID] == nil {
		return nil, errCorrupt
	}
	p := ppss[ppsID]
	s := spss[p.SPSID]
	if s == nil {
		return nil, errCorrupt
	}
	p.derive(s)
	if !h.firstInPic {
		if p.DependentSlices {
			h.dependent = r.flag()
		}
		h.address = int(r.u(ceilLog2(s.PicWCtb * s.PicHCtb)))
		if h.address >= s.PicWCtb*s.PicHCtb {
			return nil, errCorrupt
		}
	}
	if h.dependent {
		if prev == nil {
			return nil, errCorrupt
		}
		dep := *prev
		dep.dependent = true
		dep.address = h.address
		dep.firstInPic = false
		dep.nalType = h.nalType
		h = &dep
	} else {
		h.pps, h.sps = p, s
		r.skip(p.NumExtraSliceHeaderBits)
		h.typ = int(r.ue())
		if h.typ > 2 {
			return nil, errCorrupt
		}
		h.picOutput = true
		if p.OutputFlagPresent {
			h.picOutput = r.flag()
		}
		if !h.isIDR() {
			h.pocLsb = int(r.u(s.Log2MaxPOCLsb))
			if !r.flag() { // short_term_ref_pic_set_sps_flag
				start := r.pos
				rps, err := parseSTRPS(r, len(s.stRPS), s.stRPS, len(s.stRPS))
				if err != nil {
					return nil, err
				}
				h.rps = rps
				h.rpsBits = r.pos - start
			} else {
				idx := 0
				if len(s.stRPS) > 1 {
					idx = int(r.u(ceilLog2(len(s.stRPS))))
				}
				if idx >= len(s.stRPS) {
					return nil, errCorrupt
				}
				h.rps = s.stRPS[idx]
			}
			if s.LongTermRefsPresent {
				numSPS := 0
				if len(s.ltPOCLsb) > 0 {
					numSPS = int(r.ue())
				}
				numPics := int(r.ue())
				h.numLT = numSPS + numPics
				if h.numLT > 32 || numSPS > len(s.ltPOCLsb) {
					return nil, errCorrupt
				}
				prevMsb := 0
				for i := 0; i < h.numLT; i++ {
					var lsb int
					if i < numSPS {
						idx := 0
						if len(s.ltPOCLsb) > 1 {
							idx = int(r.u(ceilLog2(len(s.ltPOCLsb))))
						}
						lsb, h.ltUsed[i] = s.ltPOCLsb[idx], s.ltUsed[idx]
					} else {
						lsb = int(r.u(s.Log2MaxPOCLsb))
						h.ltUsed[i] = r.flag()
					}
					h.ltLsb[i] = lsb
					d := 0
					if r.flag() { // delta_poc_msb_present_flag
						h.ltMsb[i] = true
						d = int(r.ue())
					}
					if i != 0 && i != numSPS {
						d += prevMsb
					}
					prevMsb = d
					h.ltDeltaMsb[i] = d
				}
			}
			if s.TemporalMVP {
				h.temporalMVP = r.flag()
			}
		}
		if s.SAO {
			h.saoLuma = r.flag()
			h.saoChroma = r.flag()
		}
		if h.typ != sliceI {
			h.numRefIdx[0] = p.NumRefIdxL0Default
			if h.typ == sliceB {
				h.numRefIdx[1] = p.NumRefIdxL1Default
			}
			if r.flag() {
				h.numRefIdx[0] = int(r.ue()) + 1
				if h.typ == sliceB {
					h.numRefIdx[1] = int(r.ue()) + 1
				}
			}
			if h.numRefIdx[0] > 15 || h.numRefIdx[1] > 15 {
				return nil, errCorrupt
			}
			total := h.numPicTotalCurr()
			if p.ListsModification && total > 1 {
				bitsN := ceilLog2(total)
				for l := 0; l < 1+boolInt(h.typ == sliceB); l++ {
					h.listMod[l] = r.flag()
					if h.listMod[l] {
						for i := 0; i < h.numRefIdx[l]; i++ {
							h.listEntry[l][i] = int(r.u(bitsN))
						}
					}
				}
			}
			if h.typ == sliceB {
				h.mvdL1Zero = r.flag()
			}
			if p.CabacInitPresent {
				h.cabacInit = r.flag()
			}
			if h.temporalMVP {
				h.colFromL0 = true
				if h.typ == sliceB {
					h.colFromL0 = r.flag()
				}
				if (h.colFromL0 && h.numRefIdx[0] > 1) || (!h.colFromL0 && h.numRefIdx[1] > 1) {
					h.colRefIdx = int(r.ue())
				}
			}
			if (p.WeightedPred && h.typ == sliceP) || (p.WeightedBipred && h.typ == sliceB) {
				h.hasPW = true
				parsePredWeight(r, h)
			}
			h.maxMergeCand = 5 - int(r.ue())
			if h.maxMergeCand < 1 || h.maxMergeCand > 5 {
				return nil, errCorrupt
			}
		}
		h.qpDelta = int(r.se())
		if p.SliceChromaQPOffsets {
			h.cbQPOffset = int(r.se())
			h.crQPOffset = int(r.se())
		}
		h.deblockDisabled = p.DeblockingDisabled
		h.betaOffset, h.tcOffset = p.BetaOffset, p.TcOffset
		override := false
		if p.DeblockingOverride {
			override = r.flag()
		}
		if override {
			h.deblockDisabled = r.flag()
			if !h.deblockDisabled {
				h.betaOffset = 2 * int(r.se())
				h.tcOffset = 2 * int(r.se())
			}
		}
		h.lfAcrossSlices = p.LoopFilterAcrossSlices
		if p.LoopFilterAcrossSlices && (h.saoLuma || h.saoChroma || !h.deblockDisabled) {
			h.lfAcrossSlices = r.flag()
		}
	}
	h.entryPoints = h.entryPoints[:0]
	if h.pps.Tiles || h.pps.EntropySync {
		n := int(r.ue())
		if n > h.sps.PicWCtb*h.sps.PicHCtb {
			return nil, errCorrupt
		}
		if n > 0 {
			l := int(r.ue()) + 1
			if l > 32 {
				return nil, errCorrupt
			}
			for i := 0; i < n; i++ {
				h.entryPoints = append(h.entryPoints, int(r.u(l))+1)
			}
		}
	}
	if h.pps.SliceHeaderExtension {
		n := int(r.ue())
		r.skip(8 * n)
	}
	// byte_alignment()
	if !r.flag() {
		return nil, errCorrupt
	}
	r.align()
	if r.e != nil {
		return nil, r.e
	}
	h.dataOffset = r.pos >> 3
	return h, nil
}

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

// numPicTotalCurr counts the pictures the current picture may reference.
func (h *sliceHeader) numPicTotalCurr() int {
	n := 0
	for i := 0; i < h.rps.num(); i++ {
		if h.rps.used[i] {
			n++
		}
	}
	for i := 0; i < h.numLT; i++ {
		if h.ltUsed[i] {
			n++
		}
	}
	return n
}

func parsePredWeight(r *bitReader, h *sliceHeader) {
	pw := &h.pw
	pw.lumaDenom = int(r.ue())
	pw.chromaDenom = pw.lumaDenom + int(r.se())
	pw.lumaDenom = clip3(0, 7, pw.lumaDenom)
	pw.chromaDenom = clip3(0, 7, pw.chromaDenom)
	for l := 0; l < 1+boolInt(h.typ == sliceB); l++ {
		n := h.numRefIdx[l]
		for i := 0; i < n; i++ {
			pw.lFlag[l][i] = r.flag()
		}
		for i := 0; i < n; i++ {
			pw.cFlag[l][i] = r.flag()
		}
		for i := 0; i < n; i++ {
			pw.lw[l][i] = 1 << pw.lumaDenom
			pw.lo[l][i] = 0
			if pw.lFlag[l][i] {
				pw.lw[l][i] += int(r.se())
				pw.lo[l][i] = int(r.se())
			}
			for c := 0; c < 2; c++ {
				pw.cw[l][i][c] = 1 << pw.chromaDenom
				pw.co[l][i][c] = 0
			}
			if pw.cFlag[l][i] {
				for c := 0; c < 2; c++ {
					pw.cw[l][i][c] += int(r.se())
					d := int(r.se())
					o := 128 - ((128 * pw.cw[l][i][c]) >> pw.chromaDenom) + d
					pw.co[l][i][c] = clip3(-128, 127, o)
				}
			}
		}
	}
}
