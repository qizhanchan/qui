package h264

// Decoder decodes an H.264 elementary stream NAL unit by NAL unit and
// returns pictures in display order.
//
// Typical use:
//
//	d := avc.New()
//	for each access unit: for each NAL: d.DecodeNAL(nal); then d.EndOfPicture()
//	for f := d.Next(); f != nil; f = d.Next() { ... }
//	d.Flush() at end of stream, then drain Next.
type Decoder struct {
	sps [32]*SPS
	pps [256]*PPS

	dpb    []*picture
	pool   []*picture
	nextID int32

	cur    *picture
	curHdr *sliceHeader
	// pc is the context of the picture being decoded; the other buffer may
	// still be in use by that picture's predecessor's deblocking
	// (pipeline.go). pcOwner[i] is the picture deblocking with pcs[i].
	pcs     [2]picCtx
	pc      *picCtx
	pcIdx   int
	pcOwner [2]*picture
	// syncDeblock deblocks in place instead of pipelining (tests).
	syncDeblock bool
	sc          sliceCtx
	rbsp        []byte

	activeSPS *SPS
	haveRef   bool

	prevPOCMsb, prevPOCLsb                              int
	curPOCMsb, curTopPOC                                int
	prevFrameNum, prevFrameNumOffset                    int
	curFrameNumOffset, prevRefFrameNum                  int
	maxLongTermIdx                                      int
	tag                                                 int64
	out                                                 []*Frame
	outPics                                             []*picture // bumped, awaiting deblock + copy
	lastErr                                             error
	concealedMBs, totalMBs, droppedSlice, concealedRefs int
}

// New returns a decoder.
func New() *Decoder {
	d := &Decoder{maxLongTermIdx: -1}
	d.pc = &d.pcs[0]
	d.sc.d = d
	return d
}

// SetTag sets the value attached to the next picture that starts (see
// Frame.Tag).
func (d *Decoder) SetTag(tag int64) { d.tag = tag }

// SPSInfo returns the most recently activated SPS, or nil.
func (d *Decoder) SPSInfo() *SPS { return d.activeSPS }

// DecodeNAL decodes one NAL unit (header byte first, no start code).
// Errors inside slice data are concealed where possible; the returned error
// reports the problem but the decoder stays usable.
func (d *Decoder) DecodeNAL(nal []byte) (err error) {
	defer func() {
		// Defence in depth: a malformed stream must never take the host
		// process down. The picture's undecoded macroblocks are concealed.
		if r := recover(); r != nil {
			err = errCorruptPanic
		}
	}()
	if len(nal) == 0 || nal[0]&0x80 != 0 {
		return nil
	}
	switch nalType(nal[0]) {
	case nalSPS:
		s, err := ParseSPS(nal)
		if err != nil {
			return err
		}
		d.sps[s.ID] = s
	case nalPPS:
		p, err := ParsePPS(nal, func(id int) *SPS { return d.sps[id] })
		if err != nil {
			return err
		}
		d.pps[p.ID] = p
	case nalAUD, nalEndSeq, nalEndStream:
		d.EndOfPicture()
	case nalIDR, nalSlice:
		return d.decodeSlice(nal)
	case nalDPA, nalDPB, nalDPC:
		return ErrUnsupported
	}
	return nil
}

// DecodeNALs decodes a sequence of NAL units (e.g. one MP4 sample) and ends
// the picture afterwards.
func (d *Decoder) DecodeNALs(nals [][]byte) error {
	var first error
	for _, n := range nals {
		if err := d.DecodeNAL(n); err != nil && first == nil {
			first = err
		}
	}
	d.EndOfPicture()
	return first
}

var errCorruptPanic = errf("h264: corrupt stream (internal decoder fault recovered)")

// EndOfPicture finishes the picture being decoded (if any). Callers that
// know access-unit boundaries call it after the last NAL of each access
// unit so the picture becomes available without waiting for the next one.
func (d *Decoder) EndOfPicture() {
	defer func() {
		if recover() != nil {
			d.cur, d.curHdr = nil, nil
		}
	}()
	d.finishPicture()
}

// Flush finishes the current picture and releases every picture still
// waiting for output (end of stream or before a seek).
func (d *Decoder) Flush() {
	d.EndOfPicture()
	d.bumpAll()
	d.collect(true)
}

// collect copies bumped pictures out, in order, once their deblocking is
// done. With block it waits for all of them.
func (d *Decoder) collect(block bool) {
	for len(d.outPics) > 0 {
		p := d.outPics[0]
		if !block && !p.prog.finished() {
			return
		}
		p.prog.wait()
		d.out = append(d.out, p.frame())
		p.copyPending = false
		d.outPics[0] = nil
		d.outPics = d.outPics[1:]
		d.recycle(p)
	}
}

// Reset drops all decoder state except parameter sets (used after a seek;
// decoding must restart at an IDR or recovery point).
func (d *Decoder) Reset() {
	d.cur, d.curHdr = nil, nil
	d.dpb = d.dpb[:0]
	d.out = d.out[:0]
	d.outPics = nil
	d.haveRef = false
	d.maxLongTermIdx = -1
	d.prevPOCMsb, d.prevPOCLsb, d.prevFrameNum, d.prevFrameNumOffset, d.prevRefFrameNum = 0, 0, 0, 0, 0
}

// Next pops the next picture in display order, or nil. A picture whose
// deblocking is still running (it overlaps decoding of the next one) is
// returned by a later call; Flush waits for all of them.
func (d *Decoder) Next() *Frame {
	d.collect(false)
	if len(d.out) == 0 {
		return nil
	}
	f := d.out[0]
	copy(d.out, d.out[1:])
	d.out[len(d.out)-1] = nil
	d.out = d.out[:len(d.out)-1]
	return f
}

// DecodeAnnexB decodes a whole Annex-B stream and returns all pictures in
// display order.
func DecodeAnnexB(data []byte) ([]*Frame, error) {
	d := New()
	var frames []*Frame
	var first error
	for _, nal := range SplitAnnexB(data) {
		if err := d.DecodeNAL(nal); err != nil && first == nil {
			first = err
		}
		for f := d.Next(); f != nil; f = d.Next() {
			frames = append(frames, f)
		}
	}
	d.Flush()
	for f := d.Next(); f != nil; f = d.Next() {
		frames = append(frames, f)
	}
	return frames, first
}

func (d *Decoder) decodeSlice(nal []byte) error {
	d.rbsp = unescapeRBSP(d.rbsp, nal[1:])
	sc := &d.sc
	sc.br.reset(d.rbsp)
	h, err := parseSliceHeader(&sc.br, nal[0], d.sps[:], d.pps[:])
	if err != nil {
		return err
	}
	if h.redundantPicCnt > 0 {
		return nil
	}
	if d.cur != nil && d.isNewPicture(h) {
		d.finishPicture()
	}
	if d.cur == nil {
		if !h.idr && !d.haveRef && !h.isI() {
			// No reference yet (stream started mid-GOP): skip until an
			// intra picture arrives.
			d.droppedSlice++
			return nil
		}
		d.startPicture(h)
	}
	lists, err := d.buildRefLists(h, d.cur.poc)
	if err != nil {
		return err
	}
	return sc.decode(h, lists)
}

// isNewPicture detects the first slice of a new primary picture
// (spec 7.4.1.2.4).
func (d *Decoder) isNewPicture(h *sliceHeader) bool {
	p := d.curHdr
	if h.frameNum != p.frameNum || h.ppsID != p.ppsID || (h.nalRefIDC == 0) != (p.nalRefIDC == 0) ||
		h.idr != p.idr || (h.idr && h.idrPicID != p.idrPicID) {
		return true
	}
	switch h.sps.PicOrderCntType {
	case 0:
		if h.pocLsb != p.pocLsb || h.deltaPOCBottom != p.deltaPOCBottom {
			return true
		}
	case 1:
		if h.deltaPOC != p.deltaPOC {
			return true
		}
	}
	// Same header fields but the slice restarts at an already-decoded
	// macroblock: a new picture with identical parameters.
	return d.pc.mbs[h.firstMB].slice >= 0
}

func (d *Decoder) startPicture(h *sliceHeader) {
	sps := h.sps
	if d.activeSPS == nil || sps.PicWidthInMbs != d.activeSPS.PicWidthInMbs ||
		sps.FrameHeightInMbs() != d.activeSPS.FrameHeightInMbs() {
		d.bumpAll()
		d.dpb = d.dpb[:0]
		d.pool = d.pool[:0]
		d.haveRef = false
	}
	d.activeSPS = sps
	maxFrameNum := 1 << uint(sps.Log2MaxFrameNum)
	if !h.idr && d.haveRef && h.frameNum != d.prevRefFrameNum && h.frameNum != (d.prevRefFrameNum+1)%maxFrameNum {
		d.fillFrameNumGap(h)
	}
	p := d.newPicture(sps)
	p.frameNum = h.frameNum
	p.idr = h.idr
	p.tag = d.tag
	d.cur, d.curHdr = p, h
	p.poc = d.computePOC(h)
	// Switch to the other context buffer, waiting for the deblocking that
	// still reads it (two pictures back) if needed.
	d.pcIdx ^= 1
	if o := d.pcOwner[d.pcIdx]; o != nil {
		o.prog.wait()
		d.pcOwner[d.pcIdx] = nil
	}
	d.pc = &d.pcs[d.pcIdx]
	d.pc.reset(sps, h.pps)
}

// fillFrameNumGap inserts "non-existing" reference frames for missing
// frame_num values (spec 8.2.5.2). Their samples copy the most recent
// picture, which is also the best concealment for lost frames.
func (d *Decoder) fillFrameNumGap(h *sliceHeader) {
	sps := h.sps
	maxFrameNum := 1 << uint(sps.Log2MaxFrameNum)
	gap := (h.frameNum - d.prevRefFrameNum - 1 + maxFrameNum) % maxFrameNum
	start := (d.prevRefFrameNum + 1) % maxFrameNum
	if keep := max(sps.MaxNumRefFrames, 1); gap > keep {
		start = (h.frameNum - keep + maxFrameNum) % maxFrameNum
		gap = keep
	}
	src := d.fallbackRef()
	for i := 0; i < gap; i++ {
		fn := (start + i) % maxFrameNum
		p := d.newPicture(sps)
		if src != nil {
			src.prog.wait()
			copy(p.y, src.y)
			copy(p.cb, src.cb)
			copy(p.cr, src.cr)
			p.poc = src.poc
		}
		for j := range p.mbType {
			p.mbType[j] = mtIntra16x16
		}
		for l := 0; l < 2; l++ {
			clear(p.mv[l])
			for j := range p.ref[l] {
				p.ref[l][j] = -1
				p.refID[l][j] = -1
			}
		}
		p.frameNum = fn
		p.nonExisting = true
		d.updateFrameNumWrap(fn, sps)
		d.slidingWindow(sps)
		p.refState = refShort
		d.dpb = append(d.dpb, p)
		if d.prevFrameNum > fn {
			d.prevFrameNumOffset += maxFrameNum
		}
		d.prevFrameNum = fn
		d.prevRefFrameNum = fn
	}
}

// finishPicture deblocks the current picture, performs reference marking
// and queues it for output.
func (d *Decoder) finishPicture() {
	cur, h := d.cur, d.curHdr
	if cur == nil {
		return
	}
	d.pc.conceal(cur, d)
	switch {
	case debugNoDeblock:
		cur.prog.complete(cur.mbH)
	case debugPicture != nil || d.syncDeblock:
		d.pc.deblock(cur)
		cur.prog.complete(cur.mbH)
	default:
		d.pc.deblockAsync(cur)
		d.pcOwner[d.pcIdx] = cur
	}
	if debugPicture != nil {
		debugPicture(cur, d.pc)
	}
	d.totalMBs += len(d.pc.mbs)

	if h.nalRefIDC != 0 {
		d.markReference(cur, h)
		d.haveRef = true
	} else {
		cur.refState = refUnused
	}
	if cur.mmco5 {
		temp := cur.poc
		cur.poc = 0
		d.curTopPOC -= temp
		d.prevPOCMsb, d.prevPOCLsb = 0, d.curTopPOC
		d.prevFrameNum, d.prevFrameNumOffset, d.prevRefFrameNum = 0, 0, 0
	} else {
		if h.nalRefIDC != 0 {
			d.prevPOCMsb, d.prevPOCLsb = d.curPOCMsb, h.pocLsb
			d.prevRefFrameNum = h.frameNum
		}
		d.prevFrameNum = h.frameNum
		d.prevFrameNumOffset = d.curFrameNumOffset
	}

	if h.idr || cur.mmco5 {
		d.bumpAll()
	}
	cur.needOutput = true
	d.dpb = append(d.dpb, cur)
	d.cur, d.curHdr = nil, nil
	reorder := d.reorderDepth(h.sps)
	for d.waiting() > reorder {
		d.bump()
	}
	d.pruneDPB()
}

func (d *Decoder) waiting() int {
	n := 0
	for _, p := range d.dpb {
		if p.needOutput {
			n++
		}
	}
	return n
}

// bump outputs the waiting picture with the smallest POC.
func (d *Decoder) bump() bool {
	var best *picture
	for _, p := range d.dpb {
		if p.needOutput && (best == nil || p.poc < best.poc) {
			best = p
		}
	}
	if best == nil {
		return false
	}
	best.needOutput = false
	best.copyPending = true
	d.outPics = append(d.outPics, best)
	return true
}

func (d *Decoder) bumpAll() {
	for d.bump() {
	}
	d.pruneDPB()
}

// maxDpbMbs is Table A-1's MaxDpbMbs by level_idc.
func maxDpbFrames(sps *SPS) int {
	var mbs int
	switch sps.LevelIDC {
	case 9, 10:
		mbs = 396
	case 11:
		mbs = 900
		if sps.ConstraintFlags&0x10 != 0 && sps.ProfileIDC != 100 {
			mbs = 396
		}
	case 12, 13, 20:
		mbs = 2376
	case 21:
		mbs = 4752
	case 22, 30:
		mbs = 8100
	case 31:
		mbs = 18000
	case 32:
		mbs = 20480
	case 40, 41:
		mbs = 32768
	case 42:
		mbs = 34816
	case 50:
		mbs = 110400
	case 51, 52:
		mbs = 184320
	default:
		mbs = 696320
	}
	n := mbs / (sps.PicWidthInMbs * sps.FrameHeightInMbs())
	return clip3(max(1, sps.MaxNumRefFrames), 16, n)
}

func (d *Decoder) reorderDepth(sps *SPS) int {
	if sps.BitstreamRestriction {
		return sps.MaxNumReorderFrames
	}
	if sps.ProfileIDC == 66 || (sps.ProfileIDC == 100 && sps.ConstraintFlags&0x0c == 0x0c) {
		// Constrained Baseline / constrained High: no B-pictures.
		if sps.ConstraintFlags&0x40 != 0 || sps.ProfileIDC != 66 {
			return 0
		}
	}
	return maxDpbFrames(sps)
}

// debugPicture is a test hook called before deblocking each picture.
var debugPicture func(p *picture, pc *picCtx)

// debugNoDeblock disables the loop filter (test hook).
var debugNoDeblock bool
