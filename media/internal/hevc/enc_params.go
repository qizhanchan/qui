package hevc

// Parameter sets and slice segment headers written by the encoder. Each
// is parsed back by the decoder's own parsers, so the structures the
// encoder reconstructs with are exactly what a decoder sees.

const nalTrailR = 1

// encSeq holds the sequence-level choices written into VPS / SPS / PPS.
type encSeq struct {
	width, height         int // coded size (multiples of the minimum CU)
	confRight, confBottom int
	log2Ctb               int
	log2MaxTb             int
	maxTrDepthIntra       int
	maxTrDepthInter       int
	amp, sao, tmvp        bool
	sdh                   bool
	log2MaxPOCLsb         int
	maxDecPicBuf          int
	numReorder            int
	level                 int // general_level_idc (30 x level)
	initQP                int
	cuQPDelta             bool
	qgDepth               int
	deblockOff            bool
	betaDiv2, tcDiv2      int
	wpp                   bool
	tileCols, tileRows    int
	dependentSlices       bool
	numRefL0, numRefL1    int
	fpsNum, fpsDen        int
	fullRange             bool
	matrix                int
}

// profileTierLevel writes profile_tier_level(1, 0) for Main.
func (q *encSeq) profileTierLevel(w *bitWriter) {
	w.u(2, 0)           // general_profile_space
	w.u(1, 0)           // general_tier_flag
	w.u(5, 1)           // general_profile_idc: Main
	w.u(32, 0x60000000) // compatibility with Main (1) and Main 10 (2)
	w.u(1, 1)           // general_progressive_source_flag
	w.u(1, 0)           // general_interlaced_source_flag
	w.u(1, 0)           // general_non_packed_constraint_flag
	w.u(1, 1)           // general_frame_only_constraint_flag
	w.u(32, 0)          // general_reserved_zero_43bits
	w.u(11, 0)
	w.u(1, 0) // general_inbld_flag / reserved
	w.u(8, uint32(q.level))
}

func (q *encSeq) vps() []byte {
	var w bitWriter
	w.u(4, 0) // vps_video_parameter_set_id
	w.u(1, 1) // vps_base_layer_internal_flag
	w.u(1, 1) // vps_base_layer_available_flag
	w.u(6, 0) // vps_max_layers_minus1
	w.u(3, 0) // vps_max_sub_layers_minus1
	w.u(1, 1) // vps_temporal_id_nesting_flag
	w.u(16, 0xffff)
	q.profileTierLevel(&w)
	w.u(1, 1) // vps_sub_layer_ordering_info_present_flag
	w.ue(uint32(q.maxDecPicBuf - 1))
	w.ue(uint32(q.numReorder))
	w.ue(0)   // vps_max_latency_increase_plus1
	w.u(6, 0) // vps_max_layer_id
	w.ue(0)   // vps_num_layer_sets_minus1
	w.u(1, 0) // vps_timing_info_present_flag
	w.u(1, 0) // vps_extension_flag
	w.trailing()
	return nalUnit(nalVPS, w.buf)
}

func (q *encSeq) sps() []byte {
	var w bitWriter
	w.u(4, 0) // sps_video_parameter_set_id
	w.u(3, 0) // sps_max_sub_layers_minus1
	w.u(1, 1) // sps_temporal_id_nesting_flag
	q.profileTierLevel(&w)
	w.ue(0) // sps_seq_parameter_set_id
	w.ue(1) // chroma_format_idc 4:2:0
	w.ue(uint32(q.width))
	w.ue(uint32(q.height))
	if q.confRight > 0 || q.confBottom > 0 {
		w.u(1, 1)
		w.ue(0)
		w.ue(uint32(q.confRight / 2))
		w.ue(0)
		w.ue(uint32(q.confBottom / 2))
	} else {
		w.u(1, 0)
	}
	w.ue(0) // bit_depth_luma_minus8
	w.ue(0) // bit_depth_chroma_minus8
	w.ue(uint32(q.log2MaxPOCLsb - 4))
	w.u(1, 1) // sps_sub_layer_ordering_info_present_flag
	w.ue(uint32(q.maxDecPicBuf - 1))
	w.ue(uint32(q.numReorder))
	w.ue(0)                     // sps_max_latency_increase_plus1
	w.ue(0)                     // log2_min_luma_coding_block_size_minus3 (8x8)
	w.ue(uint32(q.log2Ctb - 3)) // log2_diff_max_min_luma_coding_block_size
	w.ue(0)                     // log2_min_luma_transform_block_size_minus2 (4x4)
	w.ue(uint32(q.log2MaxTb - 2))
	w.ue(uint32(q.maxTrDepthInter))
	w.ue(uint32(q.maxTrDepthIntra))
	w.u(1, 0) // scaling_list_enabled_flag
	w.flag(q.amp)
	w.flag(q.sao)
	w.u(1, 0) // pcm_enabled_flag
	w.ue(0)   // num_short_term_ref_pic_sets (all in slice headers)
	w.u(1, 0) // long_term_ref_pics_present_flag
	w.flag(q.tmvp)
	w.u(1, 1) // strong_intra_smoothing_enabled_flag
	w.u(1, 1) // vui_parameters_present_flag
	q.vui(&w)
	w.u(1, 0) // sps_extension_present_flag
	w.trailing()
	return nalUnit(nalSPS, w.buf)
}

func (q *encSeq) vui(w *bitWriter) {
	w.u(1, 0) // aspect_ratio_info_present_flag
	w.u(1, 0) // overscan_info_present_flag
	w.u(1, 1) // video_signal_type_present_flag
	w.u(3, 5) // video_format: unspecified
	w.flag(q.fullRange)
	w.u(1, 1) // colour_description_present_flag
	prim, trc := 1, 1
	if q.matrix == 6 {
		prim, trc = 6, 6
	}
	w.u(8, uint32(prim))
	w.u(8, uint32(trc))
	w.u(8, uint32(q.matrix))
	w.u(1, 0) // chroma_loc_info_present_flag
	w.u(1, 0) // neutral_chroma_indication_flag
	w.u(1, 0) // field_seq_flag
	w.u(1, 0) // frame_field_info_present_flag
	w.u(1, 0) // default_display_window_flag
	w.u(1, 1) // vui_timing_info_present_flag
	w.u(32, uint32(q.fpsDen))
	w.u(32, uint32(q.fpsNum))
	w.u(1, 0) // vui_poc_proportional_to_timing_flag
	w.u(1, 0) // vui_hrd_parameters_present_flag
	w.u(1, 0) // bitstream_restriction_flag
}

func (q *encSeq) pps() []byte {
	var w bitWriter
	w.ue(0) // pps_pic_parameter_set_id
	w.ue(0) // pps_seq_parameter_set_id
	w.flag(q.dependentSlices)
	w.u(1, 0) // output_flag_present_flag
	w.u(3, 0) // num_extra_slice_header_bits
	w.flag(q.sdh)
	w.u(1, 0) // cabac_init_present_flag
	w.ue(uint32(q.numRefL0 - 1))
	w.ue(uint32(q.numRefL1 - 1))
	w.se(q.initQP - 26)
	w.u(1, 0) // constrained_intra_pred_flag
	w.u(1, 0) // transform_skip_enabled_flag
	w.flag(q.cuQPDelta)
	if q.cuQPDelta {
		w.ue(uint32(q.qgDepth))
	}
	w.se(0)   // pps_cb_qp_offset
	w.se(0)   // pps_cr_qp_offset
	w.u(1, 0) // pps_slice_chroma_qp_offsets_present_flag
	w.u(1, 0) // weighted_pred_flag
	w.u(1, 0) // weighted_bipred_flag
	w.u(1, 0) // transquant_bypass_enabled_flag
	tiles := q.tileCols > 1 || q.tileRows > 1
	w.flag(tiles)
	w.flag(q.wpp)
	if tiles {
		w.ue(uint32(q.tileCols - 1))
		w.ue(uint32(q.tileRows - 1))
		w.u(1, 1) // uniform_spacing_flag
		w.u(1, 1) // loop_filter_across_tiles_enabled_flag
	}
	w.u(1, 1) // pps_loop_filter_across_slices_enabled_flag
	w.u(1, 1) // deblocking_filter_control_present_flag
	w.u(1, 0) // deblocking_filter_override_enabled_flag
	w.flag(q.deblockOff)
	if !q.deblockOff {
		w.se(q.betaDiv2)
		w.se(q.tcDiv2)
	}
	w.u(1, 0) // pps_scaling_list_data_present_flag
	w.u(1, 0) // lists_modification_present_flag
	w.ue(0)   // log2_parallel_merge_level_minus2
	w.u(1, 0) // slice_segment_header_extension_present_flag
	w.u(1, 0) // pps_extension_present_flag
	w.trailing()
	return nalUnit(nalPPS, w.buf)
}

// encSlice describes one slice segment header.
type encSlice struct {
	nalType   int
	first     bool
	dependent bool
	address   int // slice_segment_address (raster CTB)
	typ       int
	pocLsb    int
	negDelta  []int // short-term RPS: negative deltas (decreasing)
	negUsed   []bool
	posDelta  []int // positive deltas (increasing)
	posUsed   []bool
	tmvp      bool
	sao       [2]bool // luma, chroma
	numRefIdx [2]int
	mvdL1Zero bool
	colFromL0 bool
	colRefIdx int
	maxMerge  int
	qpDelta   int
	entries   []int // entry point offsets (bytes)
}

// header writes slice_segment_header() including byte_alignment().
func (q *encSeq) header(w *bitWriter, sl *encSlice, picSizeCtbs int) {
	w.flag(sl.first)
	if sl.nalType >= 16 && sl.nalType <= 23 {
		w.u(1, 0) // no_output_of_prior_pics_flag
	}
	w.ue(0) // slice_pic_parameter_set_id
	if !sl.first {
		if q.dependentSlices {
			w.flag(sl.dependent)
		}
		w.u(ceilLog2(picSizeCtbs), uint32(sl.address))
	}
	if !sl.dependent {
		w.ue(uint32(sl.typ))
		idr := sl.nalType == nalIDRWRADL || sl.nalType == nalIDRNLP
		if !idr {
			w.u(q.log2MaxPOCLsb, uint32(sl.pocLsb))
			w.u(1, 0) // short_term_ref_pic_set_sps_flag
			// st_ref_pic_set(num_short_term_ref_pic_sets = 0): explicit.
			w.ue(uint32(len(sl.negDelta)))
			w.ue(uint32(len(sl.posDelta)))
			prev := 0
			for i, d := range sl.negDelta {
				w.ue(uint32(prev - d - 1))
				w.flag(sl.negUsed[i])
				prev = d
			}
			prev = 0
			for i, d := range sl.posDelta {
				w.ue(uint32(d - prev - 1))
				w.flag(sl.posUsed[i])
				prev = d
			}
			if q.tmvp {
				w.flag(sl.tmvp)
			}
		}
		if q.sao {
			w.flag(sl.sao[0])
			w.flag(sl.sao[1])
		}
		if sl.typ != sliceI {
			override := sl.numRefIdx[0] != q.numRefL0 || (sl.typ == sliceB && sl.numRefIdx[1] != q.numRefL1)
			w.flag(override)
			if override {
				w.ue(uint32(sl.numRefIdx[0] - 1))
				if sl.typ == sliceB {
					w.ue(uint32(sl.numRefIdx[1] - 1))
				}
			}
			if sl.typ == sliceB {
				w.flag(sl.mvdL1Zero)
			}
			if sl.tmvp {
				if sl.typ == sliceB {
					w.flag(sl.colFromL0)
				}
				if (sl.colFromL0 && sl.numRefIdx[0] > 1) || (!sl.colFromL0 && sl.numRefIdx[1] > 1) {
					w.ue(uint32(sl.colRefIdx))
				}
			}
			w.ue(uint32(5 - sl.maxMerge))
		}
		w.se(sl.qpDelta)
		if sl.sao[0] || sl.sao[1] || !q.deblockOff {
			w.u(1, 1) // slice_loop_filter_across_slices_enabled_flag
		}
	}
	if q.wpp || q.tileCols > 1 || q.tileRows > 1 {
		w.ue(uint32(len(sl.entries)))
		if len(sl.entries) > 0 {
			mx := 1
			for _, e := range sl.entries {
				mx = max(mx, e)
			}
			l := 1
			for (mx-1)>>uint(l) > 0 {
				l++
			}
			w.ue(uint32(l - 1))
			for _, e := range sl.entries {
				w.u(l, uint32(e-1))
			}
		}
	}
	w.u(1, 1) // byte_alignment()
	w.alignZero()
}
