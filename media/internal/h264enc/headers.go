package h264enc

import "math"

// Profiles.
const (
	ProfileBaseline = 66 // emitted as Constrained Baseline
	ProfileMain     = 77
	ProfileHigh     = 100
)

const (
	nalSlice = 1
	nalIDR   = 5
	nalSPS   = 7
	nalPPS   = 8

	sliceP = 0
	sliceB = 1
	sliceI = 2

	log2MaxFrameNum = 8
	log2MaxPOCLsb   = 8
)

type levelLimit struct {
	idc                   int
	maxMBPS, maxFS, maxDP int // macroblocks/s, frame size, DPB macroblocks
	maxBR                 int // kbit/s (Baseline/Main; High is 1.25x)
}

var levels = []levelLimit{
	{10, 1485, 99, 396, 64},
	{11, 3000, 396, 900, 192},
	{12, 6000, 396, 2376, 384},
	{13, 11880, 396, 2376, 768},
	{20, 11880, 396, 2376, 2000},
	{21, 19800, 792, 4752, 4000},
	{22, 20250, 1620, 8100, 4000},
	{30, 40500, 1620, 8100, 10000},
	{31, 108000, 3600, 18000, 14000},
	{32, 216000, 5120, 20480, 20000},
	{40, 245760, 8192, 32768, 20000},
	{41, 245760, 8192, 32768, 50000},
	{42, 522240, 8704, 34816, 50000},
	{50, 589824, 22080, 110400, 135000},
	{51, 983040, 36864, 184320, 240000},
	{52, 2073600, 36864, 184320, 240000},
}

// pickLevel returns the lowest level whose limits admit the stream.
func pickLevel(mbW, mbH int, fps float64, dpbFrames int, kbps int, high bool) int {
	fs := mbW * mbH
	for _, l := range levels {
		lim := math.Sqrt(float64(l.maxFS) * 8)
		br := l.maxBR
		if high {
			br = br * 5 / 4
		}
		if fs <= l.maxFS && float64(mbW) <= lim && float64(mbH) <= lim &&
			float64(fs)*fps <= float64(l.maxMBPS) && fs*dpbFrames <= l.maxDP &&
			(kbps == 0 || kbps <= br) {
			return l.idc
		}
	}
	return 52
}

// writeSPS returns the sequence parameter set NAL unit.
func (e *Encoder) writeSPS() []byte {
	c := &e.cfg
	var w bitWriter
	prof := c.Profile
	w.u(8, uint32(prof))
	if prof == ProfileBaseline {
		w.u(8, 0xc0) // constraint_set0 + set1: Constrained Baseline
	} else if prof == ProfileMain {
		w.u(8, 0x40) // constraint_set1
	} else {
		w.u(8, 0)
	}
	w.u(8, uint32(e.level))
	w.ue(0) // seq_parameter_set_id
	if prof >= ProfileHigh {
		w.ue(1)   // chroma_format_idc 4:2:0
		w.ue(0)   // bit_depth_luma_minus8
		w.ue(0)   // bit_depth_chroma_minus8
		w.u(1, 0) // qpprime_y_zero_transform_bypass_flag
		w.u(1, 0) // seq_scaling_matrix_present_flag
	}
	w.ue(log2MaxFrameNum - 4)
	w.ue(uint32(e.pocType))
	if e.pocType == 0 {
		w.ue(log2MaxPOCLsb - 4)
	}
	w.ue(uint32(e.numRefs))
	w.u(1, 0) // gaps_in_frame_num_value_allowed_flag
	w.ue(uint32(e.mbW - 1))
	w.ue(uint32(e.mbH - 1))
	w.u(1, 1) // frame_mbs_only_flag
	w.u(1, 1) // direct_8x8_inference_flag
	cropR, cropB := (e.mbW*16-c.Width)/2, (e.mbH*16-c.Height)/2
	if cropR != 0 || cropB != 0 {
		w.u(1, 1)
		w.ue(0)
		w.ue(uint32(cropR))
		w.ue(0)
		w.ue(uint32(cropB))
	} else {
		w.u(1, 0)
	}
	// VUI
	w.u(1, 1)
	w.u(1, 0) // aspect_ratio_info_present_flag
	w.u(1, 0) // overscan_info_present_flag
	w.u(1, 1) // video_signal_type_present_flag
	w.u(3, 5) // video_format: unspecified
	w.flag(c.FullRange)
	w.u(1, 1) // colour_description_present_flag
	prim := uint32(c.Matrix)
	if prim == 6 {
		prim = 6 // SMPTE 170M primaries for BT.601
	}
	w.u(8, prim)             // colour_primaries
	w.u(8, uint32(c.Matrix)) // transfer_characteristics (1 and 6 coincide)
	w.u(8, uint32(c.Matrix)) // matrix_coefficients
	w.u(1, 0)                // chroma_loc_info_present_flag
	w.u(1, 1)                // timing_info_present_flag
	w.u(32, uint32(c.FPSDen))
	w.u(32, uint32(2*c.FPSNum))
	w.u(1, 1) // fixed_frame_rate_flag
	w.u(1, 0) // nal_hrd_parameters_present_flag
	w.u(1, 0) // vcl_hrd_parameters_present_flag
	w.u(1, 0) // pic_struct_present_flag
	w.u(1, 1) // bitstream_restriction_flag
	w.u(1, 1) // motion_vectors_over_pic_boundaries_flag
	w.ue(0)   // max_bytes_per_pic_denom
	w.ue(0)   // max_bits_per_mb_denom
	w.ue(15)  // log2_max_mv_length_horizontal
	w.ue(15)  // log2_max_mv_length_vertical
	w.ue(uint32(e.reorder))
	w.ue(uint32(e.numRefs + e.reorder))
	w.trailing()
	return nalUnit(3, nalSPS, w.buf)
}

// writePPS returns the picture parameter set NAL unit.
func (e *Encoder) writePPS() []byte {
	c := &e.cfg
	var w bitWriter
	w.ue(0) // pic_parameter_set_id
	w.ue(0) // seq_parameter_set_id
	w.flag(c.CABAC)
	w.u(1, 0) // bottom_field_pic_order_in_frame_present_flag
	w.ue(0)   // num_slice_groups_minus1
	w.ue(uint32(e.numRefs - 1))
	w.ue(0)                       // num_ref_idx_l1_default_active_minus1
	w.u(1, 0)                     // weighted_pred_flag
	w.u(2, 0)                     // weighted_bipred_idc
	w.se(int32(e.initQP - 26))    // pic_init_qp_minus26
	w.se(0)                       // pic_init_qs_minus26
	w.se(int32(c.ChromaQPOffset)) // chroma_qp_index_offset
	w.u(1, 1)                     // deblocking_filter_control_present_flag
	w.u(1, 0)                     // constrained_intra_pred_flag
	w.u(1, 0)                     // redundant_pic_cnt_present_flag
	if c.Profile >= ProfileHigh {
		w.flag(c.Transform8x8)
		w.u(1, 0) // pic_scaling_matrix_present_flag
		w.se(int32(c.ChromaQPOffset))
	}
	w.trailing()
	return nalUnit(3, nalPPS, w.buf)
}

// sliceHeader describes one slice being written.
type sliceHeader struct {
	typ        int
	idr        bool
	refIdc     int
	firstMB    int
	frameNum   int
	idrPicID   int
	pocLsb     int
	numRefs    int // num_ref_idx_l0_active
	numRefsL1  int
	qp         int
	directSpat bool
	cabacInit  int
}

func (e *Encoder) writeSliceHeader(w *bitWriter, h *sliceHeader) {
	w.ue(uint32(h.firstMB))
	w.ue(uint32(h.typ + 5))
	w.ue(0) // pic_parameter_set_id
	w.u(log2MaxFrameNum, uint32(h.frameNum&(1<<log2MaxFrameNum-1)))
	if h.idr {
		w.ue(uint32(h.idrPicID))
	}
	if e.pocType == 0 {
		w.u(log2MaxPOCLsb, uint32(h.pocLsb&(1<<log2MaxPOCLsb-1)))
	}
	if h.typ == sliceB {
		w.flag(h.directSpat)
	}
	if h.typ != sliceI {
		override := h.numRefs != e.numRefs || (h.typ == sliceB && h.numRefsL1 != 1)
		w.flag(override)
		if override {
			w.ue(uint32(h.numRefs - 1))
			if h.typ == sliceB {
				w.ue(uint32(h.numRefsL1 - 1))
			}
		}
		w.u(1, 0) // ref_pic_list_modification_flag_l0
		if h.typ == sliceB {
			w.u(1, 0) // ref_pic_list_modification_flag_l1
		}
	}
	if h.refIdc != 0 {
		if h.idr {
			w.u(1, 0) // no_output_of_prior_pics_flag
			w.u(1, 0) // long_term_reference_flag
		} else {
			w.u(1, 0) // adaptive_ref_pic_marking_mode_flag: sliding window
		}
	}
	if e.cfg.CABAC && h.typ != sliceI {
		w.ue(uint32(h.cabacInit))
	}
	w.se(int32(h.qp - e.initQP))
	if e.cfg.NoDeblock {
		w.ue(1)
	} else {
		w.ue(0)
		w.se(int32(e.cfg.DeblockAlpha))
		w.se(int32(e.cfg.DeblockBeta))
	}
}
