package av1

// Encoder headers: the bit writer, OBU framing, and the sequence and
// frame headers. Each writer mirrors its parser in seq.go / header.go
// field for field; the encoder parses what it writes back through the
// decoder, so a mismatch shows up as a wrong reconstruction.

// bitWriter writes the MSB-first fixed-width syntax of the headers.
type bitWriter struct {
	buf  []byte
	nbit int // bits used in the last byte (0..7; 0 = byte boundary)
}

func (w *bitWriter) bit(b int) {
	if w.nbit == 0 {
		w.buf = append(w.buf, 0)
	}
	if b != 0 {
		w.buf[len(w.buf)-1] |= 0x80 >> uint(w.nbit)
	}
	w.nbit = (w.nbit + 1) & 7
}

// f writes v as an n-bit unsigned value, f(n).
func (w *bitWriter) f(n, v int) {
	for i := n - 1; i >= 0; i-- {
		w.bit(v >> uint(i) & 1)
	}
}

func (w *bitWriter) flag(b bool) {
	if b {
		w.bit(1)
	} else {
		w.bit(0)
	}
}

// su writes an n-bit two's-complement value, su(n).
func (w *bitWriter) su(n, v int) { w.f(n, v&(1<<uint(n)-1)) }

// ns writes a non-symmetric unsigned value below n, ns(n).
func (w *bitWriter) ns(n, v int) {
	bw := 0
	for x := n; x != 0; x >>= 1 {
		bw++
	}
	m := (1 << bw) - n
	if v < m {
		w.f(bw-1, v)
		return
	}
	w.f(bw-1, m+(v-m)>>1)
	w.bit((v - m) & 1)
}

// le writes v as n little-endian bytes (byte aligned), le(n).
func (w *bitWriter) le(n, v int) {
	for i := 0; i < n; i++ {
		w.f(8, v>>(8*uint(i))&0xff)
	}
}

// byteAlign pads with zero bits to a byte boundary.
func (w *bitWriter) byteAlign() { w.nbit = 0 }

// trailing writes trailing_bits(): a one bit, then zeros to the byte
// boundary.
func (w *bitWriter) trailing() {
	w.bit(1)
	w.byteAlign()
}

// appendOBU appends an OBU of type typ with obu_size to dst.
func appendOBU(dst []byte, typ int, payload []byte) []byte {
	dst = append(dst, byte(typ<<3|2))
	dst = appendLeb128(dst, uint64(len(payload)))
	return append(dst, payload...)
}

func appendLeb128(dst []byte, v uint64) []byte {
	for {
		b := byte(v & 0x7f)
		v >>= 7
		if v != 0 {
			dst = append(dst, b|0x80)
			continue
		}
		return append(dst, b)
	}
}

// encSeq are the encoder's sequence-level choices.
type encSeq struct {
	width, height int
	level         int // seq_level_idx (31 = unconstrained)
	fullRange     bool
	matrix        int // H.273 matrix_coefficients
	primaries     int
	transfer      int
	orderHintBits int
	filterIntra   bool
	refFrameMVs   bool
}

// seqLevel picks the lowest level whose picture size limits fit w x h
// (Annex A.3; frame rate limits are not checked).
func seqLevel(w, h int) int {
	levels := []struct{ idx, maxPic, maxW, maxH int }{
		{0, 147456, 2048, 1152}, {1, 278784, 2816, 1584}, {4, 665856, 4352, 2448},
		{5, 1065024, 5504, 3096}, {8, 2359296, 6144, 3456}, {12, 8912896, 8192, 4352},
		{16, 35651584, 16384, 8704},
	}
	for _, l := range levels {
		if w*h <= l.maxPic && w <= l.maxW && h <= l.maxH {
			return l.idx
		}
	}
	return 31
}

func bitsFor(v int) int {
	n := 1
	for v>>uint(n) != 0 {
		n++
	}
	return n
}

// writeSequenceHeader returns the sequence header OBU payload (spec
// 5.5): profile 0 (8-bit 4:2:0), 64x64 superblocks.
func (s *encSeq) writeSequenceHeader() []byte {
	var w bitWriter
	w.f(3, 0)        // seq_profile
	w.flag(false)    // still_picture
	w.flag(false)    // reduced_still_picture_header
	w.flag(false)    // timing_info_present_flag
	w.flag(false)    // initial_display_delay_present_flag
	w.f(5, 0)        // operating_points_cnt_minus_1
	w.f(12, 0)       // operating_point_idc[0]
	w.f(5, s.level)  // seq_level_idx[0]
	if s.level > 7 { // seq_tier[0]
		w.flag(false)
	}
	wb, hb := bitsFor(s.width-1), bitsFor(s.height-1)
	w.f(4, wb-1)
	w.f(4, hb-1)
	w.f(wb, s.width-1)
	w.f(hb, s.height-1)
	w.flag(false)         // frame_id_numbers_present_flag
	w.flag(false)         // use_128x128_superblock
	w.flag(s.filterIntra) // enable_filter_intra
	w.flag(true)          // enable_intra_edge_filter
	w.flag(false)         // enable_interintra_compound
	w.flag(false)         // enable_masked_compound
	w.flag(false)         // enable_warped_motion
	w.flag(false)         // enable_dual_filter
	w.flag(true)          // enable_order_hint
	w.flag(false)         // enable_jnt_comp
	w.flag(s.refFrameMVs) // enable_ref_frame_mvs
	w.flag(false)         // seq_choose_screen_content_tools
	w.f(1, 0)             // seq_force_screen_content_tools
	w.f(3, s.orderHintBits-1)
	w.flag(false) // enable_superres
	w.flag(true)  // enable_cdef
	w.flag(true)  // enable_restoration
	// color_config
	w.flag(false) // high_bitdepth
	w.flag(false) // mono_chrome
	w.flag(true)  // color_description_present_flag
	w.f(8, s.primaries)
	w.f(8, s.transfer)
	w.f(8, s.matrix)
	w.flag(s.fullRange)
	w.f(2, 0)     // chroma_sample_position: unknown
	w.flag(false) // separate_uv_delta_q
	w.flag(false) // film_grain_params_present
	w.trailing()
	return w.buf
}

// encFrameHdr are the fields of one frame header the encoder sets; the
// rest follow from the sequence header and the reference state.
type encFrameHdr struct {
	showExisting bool
	frameToShow  int

	frameType      int
	showFrame      bool
	showable       bool
	errorRes       bool
	orderHint      int
	primaryRef     int
	refresh        int
	refIdx         [refsPerFrame]int
	allowHP        bool
	interp         int // filterSwitchable or a fixed filter
	switchMotion   bool
	useRefMVs      bool
	tileColsLog2   int
	tileRowsLog2   int
	tileSizeBytes  int
	baseQ          int
	lfLevel        [4]int
	lfSharpness    int
	cdefDamping    int
	cdefBits       int
	cdefYPri       [8]int
	cdefYSec       [8]int
	cdefUVPri      [8]int
	cdefUVSec      [8]int
	lrType         [3]int // restoreNone / Wiener / Sgrproj / Switchable
	lrUnitShift    int    // 0..2: 64 << shift luma units
	lrUVShift      int
	txSelect       bool
	refSelect      bool
	skipMode       bool
	reducedTxSet   bool
	skipModeAllowd bool // set by the encoder from the decoder's derivation
}

// writeFrameHeader returns the uncompressed header (spec 5.9) for the
// sequence of d, given d's reference state, without trailing bits.
func (fh *encFrameHdr) write(d *Decoder, w *bitWriter) {
	s := d.seq
	w.flag(fh.showExisting)
	if fh.showExisting {
		w.f(3, fh.frameToShow)
		return
	}
	w.f(2, fh.frameType)
	w.flag(fh.showFrame)
	if !fh.showFrame {
		w.flag(fh.showable)
	}
	intra := fh.frameType == keyFrame || fh.frameType == intraOnlyFrame
	if !(fh.frameType == switchFrame || fh.frameType == keyFrame && fh.showFrame) {
		w.flag(fh.errorRes)
	}
	w.flag(false) // disable_cdf_update
	w.flag(false) // frame_size_override_flag
	w.f(s.OrderHintBits, fh.orderHint)
	if !intra && !fh.errorRes {
		w.f(3, fh.primaryRef)
	}
	if !(fh.frameType == switchFrame || fh.frameType == keyFrame && fh.showFrame) {
		w.f(8, fh.refresh)
	}
	if (!intra || fh.refresh != 0xff) && fh.errorRes && s.EnableOrderHint {
		for i := 0; i < numRefFrames; i++ {
			w.f(s.OrderHintBits, d.ref[i].orderHint)
		}
	}
	if intra {
		w.flag(false) // render_and_frame_size_different
	} else {
		w.flag(false) // frame_refs_short_signaling
		for i := 0; i < refsPerFrame; i++ {
			w.f(3, fh.refIdx[i])
		}
		w.flag(false) // render_and_frame_size_different
		w.flag(fh.allowHP)
		w.flag(fh.interp == filterSwitchable)
		if fh.interp != filterSwitchable {
			w.f(2, fh.interp)
		}
		w.flag(fh.switchMotion)
		if !fh.errorRes && s.EnableRefFrameMVs {
			w.flag(fh.useRefMVs)
		}
	}
	w.flag(false) // disable_frame_end_update_cdf
	// tile_info: uniform spacing.
	mc, mr := 2*((s.MaxWidth+7)>>3), 2*((s.MaxHeight+7)>>3)
	lim := s.tileLimits(mc, mr)
	w.flag(true)
	for l := lim.minLog2Cols; l < lim.maxLog2Cols; l++ {
		w.flag(l < fh.tileColsLog2)
		if l >= fh.tileColsLog2 {
			break
		}
	}
	for l := max(lim.minLog2Tiles-fh.tileColsLog2, 0); l < lim.maxLog2Rows; l++ {
		w.flag(l < fh.tileRowsLog2)
		if l >= fh.tileRowsLog2 {
			break
		}
	}
	if fh.tileColsLog2 > 0 || fh.tileRowsLog2 > 0 {
		w.f(fh.tileColsLog2+fh.tileRowsLog2, 0) // context_update_tile_id
		w.f(2, fh.tileSizeBytes-1)
	}
	// quantization_params
	w.f(8, fh.baseQ)
	w.flag(false) // delta_coded (y dc)
	w.flag(false) // delta_coded (u dc)
	w.flag(false) // delta_coded (u ac)
	w.flag(false) // using_qmatrix
	w.flag(false) // segmentation_enabled
	if fh.baseQ > 0 {
		w.flag(false) // delta_q_present
	}
	lossless := fh.baseQ == 0
	if !lossless {
		w.f(6, fh.lfLevel[0])
		w.f(6, fh.lfLevel[1])
		if fh.lfLevel[0] != 0 || fh.lfLevel[1] != 0 {
			w.f(6, fh.lfLevel[2])
			w.f(6, fh.lfLevel[3])
		}
		w.f(3, fh.lfSharpness)
		w.flag(false) // loop_filter_delta_enabled
		// cdef_params
		w.f(2, fh.cdefDamping-3)
		w.f(2, fh.cdefBits)
		for i := 0; i < 1<<uint(fh.cdefBits); i++ {
			w.f(4, fh.cdefYPri[i])
			w.f(2, min(fh.cdefYSec[i], 3))
			w.f(4, fh.cdefUVPri[i])
			w.f(2, min(fh.cdefUVSec[i], 3))
		}
		// lr_params
		remap := [4]int{restoreNone: 0, restoreSwitchable: 1, restoreWiener: 2, restoreSgrproj: 3}
		uses, usesChroma := false, false
		for p := 0; p < 3; p++ {
			w.f(2, remap[fh.lrType[p]])
			if fh.lrType[p] != restoreNone {
				uses = true
				if p > 0 {
					usesChroma = true
				}
			}
		}
		if uses {
			w.flag(fh.lrUnitShift > 0)
			if fh.lrUnitShift > 0 {
				w.flag(fh.lrUnitShift > 1)
			}
			if usesChroma {
				w.f(1, fh.lrUVShift)
			}
		}
		w.flag(fh.txSelect) // tx_mode_select
	}
	if !intra {
		w.flag(fh.refSelect)
	}
	if fh.skipModeAllowd {
		w.flag(fh.skipMode)
	}
	w.flag(fh.reducedTxSet)
	if !intra {
		for ref := lastFrame; ref <= altrefFrame; ref++ {
			w.flag(false) // is_global
		}
	}
}
