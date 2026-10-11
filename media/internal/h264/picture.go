package h264

// Decoder is the top-level pure-Go H.264 decoder. It handles the
// Baseline/Main CAVLC path: SPS/PPS, I/P/B slices, 4:2:0 chroma and the
// default (no-reordering, unweighted) reference lists. CABAC and the High
// profile 8x8 transform are not yet implemented.
type Decoder struct {
	sps map[uint32]*SPS
	pps map[uint32]*PPS

	// Decoded picture buffer (reference pictures), in decode order.
	dpb []*refPicture

	// Picture-order-count state (spec 8.2.1).
	prevPOCMsb, prevPOCLsb           int
	prevFrameNum, prevFrameNumOffset int

	// Decoded pictures for output reordering.
	pics []decodedPic
}

type decodedPic struct {
	poc   int
	frame *Frame
}

// New returns an empty decoder.
func New() *Decoder {
	return &Decoder{sps: map[uint32]*SPS{}, pps: map[uint32]*PPS{}}
}

// DecodeAnnexB decodes one picture from an Annex-B byte stream.
func (d *Decoder) DecodeAnnexB(data []byte) (*Frame, error) {
	if _, err := d.decodeStream(splitAnnexB(data)); err != nil {
		return nil, err
	}
	if len(d.pics) == 0 {
		return nil, errf("h264: no slice decoded")
	}
	return d.pics[len(d.pics)-1].frame, nil
}

// DecodeAllAnnexB decodes every picture in an Annex-B byte stream and returns
// them in display (picture-order-count) order.
func (d *Decoder) DecodeAllAnnexB(data []byte) ([]*Frame, error) {
	if _, err := d.decodeStream(splitAnnexB(data)); err != nil {
		return nil, err
	}
	order := make([]int, len(d.pics))
	for i := range order {
		order[i] = i
	}
	// Stable sort by POC so pictures with equal POC keep decode order.
	for i := 1; i < len(order); i++ {
		for j := i; j > 0 && d.pics[order[j]].poc < d.pics[order[j-1]].poc; j-- {
			order[j], order[j-1] = order[j-1], order[j]
		}
	}
	out := make([]*Frame, len(d.pics))
	for i, k := range order {
		out[i] = d.pics[k].frame
	}
	return out, nil
}

// DecodeNALUs decodes the pictures in a list of raw NAL units, returning the
// last decoded picture (before reordering).
func (d *Decoder) DecodeNALUs(nalus [][]byte) (*Frame, error) {
	if _, err := d.decodeStream(nalus); err != nil {
		return nil, err
	}
	if len(d.pics) == 0 {
		return nil, errf("h264: no slice decoded")
	}
	return d.pics[len(d.pics)-1].frame, nil
}

func (d *Decoder) decodeStream(nalus [][]byte) (bool, error) {
	got := false
	for _, nal := range nalus {
		if len(nal) == 0 {
			continue
		}
		switch nalType(nal[0]) {
		case nalSPS:
			s, err := ParseSPS(nal)
			if err != nil {
				return got, err
			}
			d.sps[s.ID] = s
		case nalPPS:
			p, err := ParsePPS(nal)
			if err != nil {
				return got, err
			}
			d.pps[p.ID] = p
		case nalIDR, nalSlice:
			if err := d.decodeSlice(nal); err != nil {
				return got, err
			}
			got = true
		}
	}
	return got, nil
}

func (d *Decoder) decodeSlice(nal []byte) error {
	nalTyp := nalType(nal[0])
	refIDC := nalRefIDC(nal[0])
	rbsp := unescapeRBSP(nal[1:])

	// Pre-scan for pic_parameter_set_id so we can resolve the parameter sets.
	pre := newBitReader(rbsp)
	_ = pre.ue() // first_mb_in_slice
	_ = pre.ue() // slice_type
	ppsID := pre.ue()
	if pre.err() != nil {
		return pre.err()
	}
	pps := d.pps[ppsID]
	if pps == nil {
		return errf("h264: unknown PPS id")
	}
	sps := d.sps[pps.SPSID]
	if sps == nil {
		return errf("h264: unknown SPS id")
	}
	if pps.EntropyCoding {
		return errf("h264: CABAC entropy coding not yet supported")
	}
	if sps.ChromaFormatIDC != 1 {
		return errf("h264: only 4:2:0 chroma supported yet")
	}

	r := newBitReader(rbsp)
	hdr, err := parseSliceHeader(r, sps, pps, refIDC, nalTyp)
	if err != nil {
		return err
	}

	isIDR := nalTyp == nalIDR
	isRef := refIDC != 0
	if isIDR {
		d.dpb = nil
	}
	poc := d.computePOC(sps, hdr, isIDR, isRef)

	l0, l1 := d.buildRefLists(poc)
	l0f := framesOf(l0)
	l1f := framesOf(l1)
	var coloc *refPicture
	if len(l1) > 0 {
		coloc = l1[0]
	}

	mbW := sps.PicWidthInMbs
	mbH := sps.PicHeightInMapUnits
	frame := NewFrame(mbW*16, mbH*16)

	sliceQPY := clampInt(26+int(pps.PicInitQP)+int(hdr.SliceQPDelta), 0, 51)

	s := &picState{
		sps:                  sps,
		pps:                  pps,
		mbWidth:              mbW,
		mbHeight:             mbH,
		chromaArrayType:      int(sps.ChromaFormatIDC),
		chromaQpIndexOffset:  int(pps.ChromaQPOffset),
		transform8x8ModeFlag: pps.Transform8x8Mode,
		qpY:                  sliceQPY,
		mbs:                  make([]mbData, mbW*mbH),
		br:                   r,
		refFrames:            l0f,
		refFramesL1:          l1f,
		refPOCL0:             pocsOf(l0),
		refPOCL1:             pocsOf(l1),
		curPOC:               poc,
		directSpatial:        hdr.DirectSpatialMV,
		coloc:                coloc,
		numRefL0:             hdr.NumRefIdxL0Active,
		numRefL1:             hdr.NumRefIdxL1Active,
		weightedPred:         len(hdr.LumaWeightL0) > 0,
		lumaLog2WD:           hdr.LumaLog2WeightDenom,
		chromaLog2WD:         hdr.ChromaLog2WeightDenom,
		lumaWeightL0:         hdr.LumaWeightL0,
		lumaOffsetL0:         hdr.LumaOffsetL0,
		chromaWeightL0:       hdr.ChromaWeightL0,
		chromaOffsetL0:       hdr.ChromaOffsetL0,
		lumaWeightL1:         hdr.LumaWeightL1,
		lumaOffsetL1:         hdr.LumaOffsetL1,
		chromaWeightL1:       hdr.ChromaWeightL1,
		chromaOffsetL1:       hdr.ChromaOffsetL1,
	}
	if err := s.decodeSliceCAVLC(frame, hdr); err != nil {
		return err
	}
	s.deblock(frame)

	if isRef {
		d.addReference(&refPicture{
			frame: frame, poc: poc, frameNum: hdr.FrameNum,
			mvLX: s.mvLX, mvLY: s.mvLY, refLX: s.refLX,
			mvL1X: s.mvL1X, mvL1Y: s.mvL1Y, refL1X: s.refL1X,
		}, sps.MaxNumRefFrames)
	}
	d.pics = append(d.pics, decodedPic{poc: poc, frame: cropFrame(frame, sps.Width, sps.Height)})
	return nil
}

func framesOf(ps []*refPicture) []*Frame {
	out := make([]*Frame, len(ps))
	for i, p := range ps {
		out[i] = p.frame
	}
	return out
}

func pocsOf(ps []*refPicture) []int {
	out := make([]int, len(ps))
	for i, p := range ps {
		out[i] = p.poc
	}
	return out
}
