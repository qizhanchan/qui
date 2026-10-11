package hevc

import (
	"math/bits"

	"github.com/qizhanchan/qui/media/internal/h264"
)

// CABAC decoding engine (spec 9.3.4.3). The arithmetic coder is the same
// as H.264's, so the LPS range and state-transition tables come from the
// H.264 decoder.

var lpsRange, nextMPS, nextLPS = h264.CABACTransitions()

type cabac struct {
	rng, off uint32
	data     []byte
	pos      int // next bit to read
	nbits    int
}

func (c *cabac) bit() uint32 {
	p := c.pos
	c.pos++
	if p >= c.nbits {
		return 0
	}
	return uint32(c.data[p>>3]>>(7-uint(p&7))) & 1
}

func (c *cabac) bitsN(n int) uint32 {
	var v uint32
	for i := 0; i < n; i++ {
		v = v<<1 | c.bit()
	}
	return v
}

// start initialises the engine at byte offset byteOff of data.
func (c *cabac) start(data []byte, byteOff int) {
	c.data, c.nbits = data, len(data)*8
	c.pos = byteOff * 8
	c.rng = 510
	c.off = c.bitsN(9)
}

func (c *cabac) decision(st *uint8) int {
	s := *st
	lps := uint32(lpsRange[s][(c.rng>>6)&3])
	c.rng -= lps
	if c.off < c.rng {
		*st = nextMPS[s]
		if c.rng < 256 {
			c.rng <<= 1
			c.off = c.off<<1 | c.bit()
		}
		return int(s & 1)
	}
	c.off -= c.rng
	c.rng = lps
	*st = nextLPS[s]
	n := bits.LeadingZeros32(lps) - 23
	c.rng <<= uint(n)
	c.off = c.off<<uint(n) | c.bitsN(n)
	return int(s&1) ^ 1
}

func (c *cabac) bypass() int {
	c.off = c.off<<1 | c.bit()
	if c.off >= c.rng {
		c.off -= c.rng
		return 1
	}
	return 0
}

func (c *cabac) bypassBits(n int) int {
	v := 0
	for i := 0; i < n; i++ {
		v = v<<1 | c.bypass()
	}
	return v
}

func (c *cabac) terminate() int {
	c.rng -= 2
	if c.off >= c.rng {
		return 1
	}
	if c.rng < 256 {
		c.rng <<= 1
		c.off = c.off<<1 | c.bit()
	}
	return 0
}

// alignAfterTerminate returns the byte offset of the data following a
// terminating bin equal to 1. The encoder's flush ends with the stop bit,
// which is already the last bit inside the 9-bit offset window, so the
// next data starts at the following byte boundary (HM finish(), FFmpeg
// skip_bytes).
func (c *cabac) alignAfterTerminate() int {
	return (c.pos + 7) >> 3
}

// Context indices.
const (
	ctxSaoMerge         = 0
	ctxSaoType          = 1
	ctxSplitCU          = 2   // 3
	ctxTransquantBypass = 5   // 1
	ctxSkip             = 6   // 3
	ctxCuQPDelta        = 9   // 2
	ctxPredMode         = 11  // 1
	ctxPartMode         = 12  // 4
	ctxPrevIntra        = 16  // 1
	ctxChromaMode       = 17  // 1
	ctxMergeFlag        = 18  // 1
	ctxMergeIdx         = 19  // 1
	ctxInterPredIdc     = 20  // 5
	ctxRefIdx           = 25  // 2
	ctxMvdGt0           = 27  // 1
	ctxMvdGt1           = 28  // 1
	ctxMvpFlag          = 29  // 1
	ctxRqtRootCbf       = 30  // 1
	ctxSplitTransform   = 31  // 3
	ctxCbfLuma          = 34  // 2
	ctxCbfChroma        = 36  // 4
	ctxTransformSkip    = 40  // 2 (luma, chroma)
	ctxLastX            = 42  // 18
	ctxLastY            = 60  // 18
	ctxCodedSubBlock    = 78  // 4
	ctxSig              = 82  // 44
	ctxGt1              = 126 // 24
	ctxGt2              = 150 // 6
	numCtx              = 156
)

// initValues[initType] lists the context initValues (Tables 9-5..9-37) in
// the order of the ctx* constants.
var initValues = [3][numCtx]uint8{
	{
		153,           // sao_merge
		200,           // sao_type_idx
		139, 141, 157, // split_cu_flag
		154,           // cu_transquant_bypass_flag
		154, 154, 154, // cu_skip_flag (unused)
		154, 154, // cu_qp_delta_abs
		154,                // pred_mode_flag (unused)
		184, 154, 154, 154, // part_mode
		184,                     // prev_intra_luma_pred_flag
		63,                      // intra_chroma_pred_mode
		154,                     // merge_flag (unused)
		154,                     // merge_idx (unused)
		154, 154, 154, 154, 154, // inter_pred_idc (unused)
		154, 154, // ref_idx (unused)
		154,           // abs_mvd_greater0_flag (unused)
		154,           // abs_mvd_greater1_flag (unused)
		154,           // mvp_flag (unused)
		154,           // rqt_root_cbf (unused)
		153, 138, 138, // split_transform_flag
		111, 141, // cbf_luma
		94, 138, 182, 154, // cbf_cb / cbf_cr
		139, 139, // transform_skip_flag
		110, 110, 124, 125, 140, 153, 125, 127, 140, 109, 111, 143, 127, 111, 79, 108, 123, 63,
		110, 110, 124, 125, 140, 153, 125, 127, 140, 109, 111, 143, 127, 111, 79, 108, 123, 63,
		91, 171, 134, 141, // coded_sub_block_flag
		111, 111, 125, 110, 110, 94, 124, 108, 124, 107, 125, 141, 179, 153, 125, 107,
		125, 141, 179, 153, 125, 107, 125, 141, 179, 153, 125, 140, 139, 182, 182, 152,
		136, 152, 136, 153, 136, 139, 111, 136, 139, 111, 141, 111,
		140, 92, 137, 138, 140, 152, 138, 139, 153, 74, 149, 92, 139, 107, 122, 152,
		140, 179, 166, 182, 140, 227, 122, 197,
		138, 153, 136, 167, 152, 152,
	},
	{
		153,
		185,
		107, 139, 126,
		154,
		197, 185, 201,
		154, 154,
		149,
		154, 139, 154, 154,
		154,
		152,
		110,
		122,
		95, 79, 63, 31, 31,
		153, 153,
		140,
		198,
		168,
		79,
		124, 138, 94,
		153, 111,
		149, 107, 167, 154,
		139, 139,
		125, 110, 94, 110, 95, 79, 125, 111, 110, 78, 110, 111, 111, 95, 94, 108, 123, 108,
		125, 110, 94, 110, 95, 79, 125, 111, 110, 78, 110, 111, 111, 95, 94, 108, 123, 108,
		121, 140, 61, 154,
		155, 154, 139, 153, 139, 123, 123, 63, 153, 166, 183, 140, 136, 153, 154, 166,
		183, 140, 136, 153, 154, 166, 183, 140, 136, 153, 154, 170, 153, 123, 123, 107,
		121, 107, 121, 167, 151, 183, 140, 151, 183, 140, 140, 140,
		154, 196, 196, 167, 154, 152, 167, 182, 182, 134, 149, 136, 153, 121, 136, 137,
		169, 194, 166, 167, 154, 167, 137, 182,
		107, 167, 91, 122, 107, 167,
	},
	{
		153,
		160,
		107, 139, 126,
		154,
		197, 185, 201,
		154, 154,
		134,
		154, 139, 154, 154,
		183,
		152,
		154,
		137,
		95, 79, 63, 31, 31,
		153, 153,
		169,
		198,
		168,
		79,
		224, 167, 122,
		153, 111,
		149, 92, 167, 154,
		139, 139,
		125, 110, 124, 110, 95, 94, 125, 111, 111, 79, 125, 126, 111, 111, 79, 108, 123, 93,
		125, 110, 124, 110, 95, 94, 125, 111, 111, 79, 125, 126, 111, 111, 79, 108, 123, 93,
		121, 140, 61, 154,
		170, 154, 139, 153, 139, 123, 123, 63, 124, 166, 183, 140, 136, 153, 154, 166,
		183, 140, 136, 153, 154, 166, 183, 140, 136, 153, 154, 170, 153, 138, 138, 122,
		121, 122, 121, 167, 151, 183, 140, 151, 183, 140, 140, 140,
		154, 196, 167, 167, 154, 152, 167, 182, 182, 134, 149, 136, 153, 121, 136, 122,
		169, 208, 166, 167, 154, 152, 167, 182,
		107, 167, 91, 107, 107, 167,
	},
}

// initContexts sets the context states for a slice (9.3.2.2).
func initContexts(st *[numCtx]uint8, initType, qp int) {
	qp = clip3(0, 51, qp)
	for i, v := range initValues[initType] {
		m := int(v>>4)*5 - 45
		n := int(v&15)<<3 - 16
		pre := clip3(1, 126, ((m*qp)>>4)+n)
		if pre <= 63 {
			st[i] = uint8((63 - pre) << 1)
		} else {
			st[i] = uint8((pre-64)<<1 | 1)
		}
	}
}
