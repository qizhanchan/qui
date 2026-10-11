package h264

import "testing"

func TestParseSliceHeaderIDR(t *testing.T) {
	sps := &SPS{
		Log2MaxFrameNum:       4,
		PicOrderCntType:       0,
		Log2MaxPicOrderCntLsb: 4,
		FrameMbsOnly:          true,
	}
	pps := &PPS{
		DeblockingCtl: true,
		NumRefIdxL0:   1,
		NumRefIdxL1:   1,
	}
	// first_mb(0) slice_type(2=I) pps(0) frame_num(0) idr_pic_id(0)
	// pic_order_cnt_lsb(0) | marking(00) | slice_qp_delta(0)
	// deblock: disable(0) alpha(0) beta(0)
	bits := "1" + "011" + "1" + "0000" + "1" + "0000" + "00" + "1" + "1" + "1" + "1"
	r := newBitReader(packBits(bits))
	h, err := parseSliceHeader(r, sps, pps, 3, nalIDR)
	if err != nil {
		t.Fatalf("parseSliceHeader: %v", err)
	}
	if !h.IsIntra() {
		t.Errorf("IsIntra() = false, slice_type = %d", h.SliceType)
	}
	if h.FirstMbInSlice != 0 || h.SliceType != 2 || h.PPSID != 0 {
		t.Errorf("header = %+v", h)
	}
	if h.FrameNum != 0 || h.IDRPicID != 0 || h.PicOrderCntLsb != 0 {
		t.Errorf("frame_num/idr/poc = %d/%d/%d", h.FrameNum, h.IDRPicID, h.PicOrderCntLsb)
	}
	if h.SliceQPDelta != 0 || h.DisableDeblockingFilterIDC != 0 {
		t.Errorf("qp/deblock = %d/%d", h.SliceQPDelta, h.DisableDeblockingFilterIDC)
	}
}

func TestSliceTypePredicates(t *testing.T) {
	if !sliceTypeIsI(2) || !sliceTypeIsI(7) || sliceTypeIsI(0) {
		t.Error("sliceTypeIsI wrong")
	}
	if !sliceTypeIsP(0) || !sliceTypeIsP(5) || sliceTypeIsP(2) {
		t.Error("sliceTypeIsP wrong")
	}
	if !sliceTypeIsB(1) || !sliceTypeIsB(6) {
		t.Error("sliceTypeIsB wrong")
	}
}
