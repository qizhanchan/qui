package h264enc

import "github.com/qizhanchan/qui/media/internal/h264"

// Forward transforms and quantisation (the inverse of spec 8.5). Dequant
// and inverse transforms come from the decoder (h264 kit), so the encoder's
// reconstruction is the decoder's.

// quantMF is the forward multiplication factor per qP%6 and position class
// (0: both even, 1: both odd, 2: mixed), matching normAdjust4.
var quantMF = [6][3]int32{
	{13107, 5243, 8066}, {11916, 4660, 7490}, {10082, 4194, 6554},
	{9362, 3647, 5825}, {8192, 3355, 5243}, {7282, 2893, 4559},
}

var mf4 [52][16]int32

// 8x8: forward factors per qP%6 and class (normAdjust8 classes).
var quantMF8 = [6][6]int32{
	{13107, 11428, 20972, 12222, 16777, 15481},
	{11916, 10826, 19174, 11058, 14980, 14290},
	{10082, 8943, 15978, 9675, 12710, 11985},
	{9362, 8228, 14913, 8931, 11984, 11259},
	{8192, 7346, 13159, 7740, 10486, 9777},
	{7282, 6428, 11570, 6830, 9118, 8640},
}

var mf8 [52][64]int32

// mfRows holds mf4 per row, duplicated for two side-by-side blocks (the
// NEON layout).
var mfRows [52][4][8]uint16

func class4(i int) int {
	r, c := i>>2, i&3
	switch {
	case r&1 == 0 && c&1 == 0:
		return 0
	case r&1 == 1 && c&1 == 1:
		return 1
	}
	return 2
}

func class8(i int) int {
	r, c := i>>3, i&7
	switch {
	case r%4 == 0 && c%4 == 0:
		return 0
	case r%2 == 1 && c%2 == 1:
		return 1
	case r%4 == 2 && c%4 == 2:
		return 2
	case (r%4 == 0 && c%2 == 1) || (r%2 == 1 && c%4 == 0):
		return 3
	case (r%4 == 0 && c%4 == 2) || (r%4 == 2 && c%4 == 0):
		return 4
	}
	return 5
}

var (
	zz4 = h264.ZigZag4x4()
	zz8 = h264.ZigZag8x8()
	dq  = h264.NewDequant(nil)
)

func init() {
	for qp := 0; qp < 52; qp++ {
		for i := 0; i < 16; i++ {
			mf4[qp][i] = quantMF[qp%6][class4(i)]
		}
		for i := 0; i < 64; i++ {
			mf8[qp][i] = quantMF8[qp%6][class8(i)]
		}
		for r := 0; r < 4; r++ {
			for k := 0; k < 8; k++ {
				mfRows[qp][r][k] = uint16(mf4[qp][r*4+k&3])
			}
		}
	}
}

// fdct4 computes the forward 4x4 core transform of the residual src-pred.
func fdct4(out *[16]int32, src []byte, ss int, pred []byte, ps int) {
	var d [16]int32
	for y := 0; y < 4; y++ {
		s, p := src[y*ss:y*ss+4], pred[y*ps:y*ps+4]
		for x := 0; x < 4; x++ {
			d[y*4+x] = int32(s[x]) - int32(p[x])
		}
	}
	fdct4Residual(out, &d)
}

func fdct4Residual(out, d *[16]int32) {
	var t [16]int32
	for y := 0; y < 4; y++ {
		a, b, c, e := d[y*4], d[y*4+1], d[y*4+2], d[y*4+3]
		s03, s12, d03, d12 := a+e, b+c, a-e, b-c
		t[y*4] = s03 + s12
		t[y*4+1] = 2*d03 + d12
		t[y*4+2] = s03 - s12
		t[y*4+3] = d03 - 2*d12
	}
	for x := 0; x < 4; x++ {
		a, b, c, e := t[x], t[4+x], t[8+x], t[12+x]
		s03, s12, d03, d12 := a+e, b+c, a-e, b-c
		out[x] = s03 + s12
		out[4+x] = 2*d03 + d12
		out[8+x] = s03 - s12
		out[12+x] = d03 - 2*d12
	}
}

// dctQuant8 transforms and quantises the four 4x4 blocks of the 8x8
// residual src - pred: lv[k] receives block k (0 top-left, 1 top-right,
// 2 bottom-left, 3 bottom-right) in raster order. It returns a mask of
// the blocks with non-zero levels.
func dctQuant8(lv *[64]int32, src []byte, ss int, pred []byte, ps int, qp int, intra bool) int {
	qbits := 15 + qp/6
	f := 1 << qbits / 6
	if intra {
		f = 1 << qbits / 3
	}
	if haveNEON && 7*ss+8 <= len(src) && 7*ps+8 <= len(pred) {
		mask := 0
		neonDCTQuant8(&src[0], ss, &pred[0], ps, &mfRows[qp], f, qbits, lv, &mask)
		return mask
	}
	mask := 0
	for k := 0; k < 4; k++ {
		bx, by := (k&1)*4, (k>>1)*4
		b := (*[16]int32)(lv[16*k : 16*k+16])
		fdct4(b, src[by*ss+bx:], ss, pred[by*ps+bx:], ps)
		if quant4(b, qp, intra, 0) > 0 {
			mask |= 1 << uint(k)
		}
	}
	return mask
}

// fdct8 computes the forward 8x8 transform of src-pred.
func fdct8(out *[64]int32, src []byte, ss int, pred []byte, ps int) {
	var d [64]int32
	for y := 0; y < 8; y++ {
		s, p := src[y*ss:y*ss+8], pred[y*ps:y*ps+8]
		for x := 0; x < 8; x++ {
			d[y*8+x] = int32(s[x]) - int32(p[x])
		}
	}
	var t [64]int32
	for y := 0; y < 8; y++ {
		r := fdct8Row(d[y*8 : y*8+8])
		copy(t[y*8:], r[:])
	}
	var col [8]int32
	for x := 0; x < 8; x++ {
		for y := 0; y < 8; y++ {
			col[y] = t[y*8+x]
		}
		r := fdct8Row(col[:])
		for y := 0; y < 8; y++ {
			out[y*8+x] = r[y]
		}
	}
}

// fdct8Row is x264's forward 8-point transform (the exact inverse pair of
// the spec's 8x8 IDCT up to the quantiser's normalisation).
func fdct8Row(s []int32) [8]int32 {
	s07, s16, s25, s34 := s[0]+s[7], s[1]+s[6], s[2]+s[5], s[3]+s[4]
	a0, a1, a2, a3 := s07+s34, s16+s25, s07-s34, s16-s25
	d07, d16, d25, d34 := s[0]-s[7], s[1]-s[6], s[2]-s[5], s[3]-s[4]
	a4 := d16 + d25 + (d07 + d07>>1)
	a5 := d07 - d34 - (d25 + d25>>1)
	a6 := d07 + d34 - (d16 + d16>>1)
	a7 := d16 - d25 + (d34 + d34>>1)
	var o [8]int32
	o[0] = a0 + a1
	o[1] = a4 + a7>>2
	o[2] = a2 + a3>>1
	o[3] = a5 + a6>>2
	o[4] = a0 - a1
	o[5] = a6 - a5>>2
	o[6] = a2>>1 - a3
	o[7] = a4>>2 - a7
	return o
}

// quant4 quantises a 4x4 block in place (raster) and returns the number of
// non-zero levels. start = 1 skips the DC (Intra16x16 / chroma AC).
func quant4(c *[16]int32, qp int, intra bool, start int) int {
	mf := &mf4[qp]
	qbits := uint(15 + qp/6)
	f := int32(1) << qbits / 6
	if intra {
		f = int32(1) << qbits / 3
	}
	nz := 0
	for i := start; i < 16; i++ {
		v := c[i]
		if v == 0 {
			continue
		}
		neg := v < 0
		if neg {
			v = -v
		}
		l := (v*mf[i] + f) >> qbits
		if l != 0 {
			nz++
			if neg {
				l = -l
			}
		}
		c[i] = l
	}
	return nz
}

// quant8 quantises an 8x8 block in place (raster).
func quant8(c *[64]int32, qp int, intra bool) int {
	mf := &mf8[qp]
	qbits := uint(16 + qp/6)
	f := int32(1) << qbits / 6
	if intra {
		f = int32(1) << qbits / 3
	}
	nz := 0
	for i := 0; i < 64; i++ {
		v := c[i]
		if v == 0 {
			continue
		}
		neg := v < 0
		if neg {
			v = -v
		}
		l := int32((int64(v)*int64(mf[i]) + int64(f)) >> qbits)
		if l != 0 {
			nz++
			if neg {
				l = -l
			}
		}
		c[i] = l
	}
	return nz
}

// dequant4 scales levels (raster) as the decoder does; start = 1 leaves
// position 0 untouched.
func dequant4(c *[16]int32, list, qp, start int) {
	q := dq.Scale4(list, qp)
	for i := start; i < 16; i++ {
		if c[i] != 0 {
			c[i] = (c[i]*q[i] + 8) >> 4
		}
	}
}

func dequant8(c *[64]int32, list, qp int) {
	q := dq.Scale8(list, qp)
	for i := 0; i < 64; i++ {
		if c[i] != 0 {
			c[i] = (c[i]*q[i] + 32) >> 6
		}
	}
}

// lumaDC transforms, quantises and reconstructs the Intra16x16 DC: dc holds
// the 16 block DCs (raster of blocks); on return levels holds the quantised
// levels (raster) and dc the dequantised values for each block's position 0.
func lumaDC(dc, levels *[16]int32, qp int) int {
	var t [16]int32
	for i := 0; i < 4; i++ {
		s0, s1, s2, s3 := dc[i*4], dc[i*4+1], dc[i*4+2], dc[i*4+3]
		t[i*4] = s0 + s1 + s2 + s3
		t[i*4+1] = s0 + s1 - s2 - s3
		t[i*4+2] = s0 - s1 - s2 + s3
		t[i*4+3] = s0 - s1 + s2 - s3
	}
	qbits := uint(16 + qp/6)
	f := int32(1) << qbits / 3
	mf := quantMF[qp%6][0]
	nz := 0
	for j := 0; j < 4; j++ {
		f0, f1, f2, f3 := t[j], t[4+j], t[8+j], t[12+j]
		col := [4]int32{f0 + f1 + f2 + f3, f0 + f1 - f2 - f3, f0 - f1 - f2 + f3, f0 - f1 + f2 - f3}
		for i, v := range col {
			v >>= 1
			neg := v < 0
			if neg {
				v = -v
			}
			l := (v*mf + f) >> qbits
			if l != 0 {
				nz++
				if neg {
					l = -l
				}
			}
			levels[i*4+j] = l
		}
	}
	*dc = *levels
	if nz == 0 {
		*dc = [16]int32{}
		return 0
	}
	h264.InverseHadamard4(dc)
	q := dq.Scale4(0, qp)[0]
	for i := range dc {
		dc[i] = (dc[i]*q + 32) >> 6
	}
	return nz
}

// chromaDC is lumaDC's 2x2 counterpart for one chroma plane; list is the
// dequant list (1/2 intra Cb/Cr, 4/5 inter).
func chromaDC(dc, levels *[4]int32, qp int, intra bool, list int) int {
	c0, c1, c2, c3 := dc[0], dc[1], dc[2], dc[3]
	f := [4]int32{c0 + c1 + c2 + c3, c0 - c1 + c2 - c3, c0 + c1 - c2 - c3, c0 - c1 - c2 + c3}
	qbits := uint(16 + qp/6)
	r := int32(1) << qbits / 6
	if intra {
		r = int32(1) << qbits / 3
	}
	mf := quantMF[qp%6][0]
	nz := 0
	for i, v := range f {
		neg := v < 0
		if neg {
			v = -v
		}
		l := (v*mf + r) >> qbits
		if l != 0 {
			nz++
			if neg {
				l = -l
			}
		}
		levels[i] = l
	}
	if nz == 0 {
		*dc = [4]int32{}
		return 0
	}
	l0, l1, l2, l3 := levels[0], levels[1], levels[2], levels[3]
	g := [4]int32{l0 + l1 + l2 + l3, l0 - l1 + l2 - l3, l0 + l1 - l2 - l3, l0 - l1 - l2 + l3}
	q := dq.Scale4(list, qp)[0]
	for i := range g {
		dc[i] = (g[i] * q) >> 5
	}
	return nz
}

// satd4 is the sum of absolute Hadamard-transformed differences of a 4x4,
// halved.
func satd4(a []byte, as int, b []byte, bs int) int {
	return (satdRaw(a, as, b, bs, 4, 4) + 1) >> 1
}

// satd returns the SATD of a w x h block (multiples of 4): the raw sum over
// its 4x4 blocks, halved once.
func satd(a []byte, as int, b []byte, bs int, w, h int) int {
	return (satdRaw(a, as, b, bs, w, h) + 1) >> 1
}

func satdRaw(a []byte, as int, b []byte, bs int, w, h int) int {
	if haveNEON && (w == 4 || w == 8 || w == 16) && h&3 == 0 && (h-1)*as+w <= len(a) && (h-1)*bs+w <= len(b) {
		var out int
		neonSATD(&a[0], as, &b[0], bs, w, h, &out)
		return out
	}
	s := 0
	for y := 0; y < h; y += 4 {
		for x := 0; x < w; x += 4 {
			s += satd4Raw(a[y*as+x:], as, b[y*bs+x:], bs)
		}
	}
	return s
}

func satd4Raw(a []byte, as int, b []byte, bs int) int {
	var d [16]int32
	for y := 0; y < 4; y++ {
		ra, rb := a[y*as:y*as+4], b[y*bs:y*bs+4]
		d[y*4] = int32(ra[0]) - int32(rb[0])
		d[y*4+1] = int32(ra[1]) - int32(rb[1])
		d[y*4+2] = int32(ra[2]) - int32(rb[2])
		d[y*4+3] = int32(ra[3]) - int32(rb[3])
	}
	var t [16]int32
	for y := 0; y < 4; y++ {
		s0, s1, s2, s3 := d[y*4], d[y*4+1], d[y*4+2], d[y*4+3]
		a0, a1, a2, a3 := s0+s1, s0-s1, s2+s3, s2-s3
		t[y*4], t[y*4+1], t[y*4+2], t[y*4+3] = a0+a2, a1+a3, a0-a2, a1-a3
	}
	sum := int32(0)
	for x := 0; x < 4; x++ {
		s0, s1, s2, s3 := t[x], t[4+x], t[8+x], t[12+x]
		a0, a1, a2, a3 := s0+s1, s0-s1, s2+s3, s2-s3
		sum += abs32(a0+a2) + abs32(a1+a3) + abs32(a0-a2) + abs32(a1-a3)
	}
	return int(sum)
}

// sad returns the sum of absolute differences of a w x h block.
func sad(a []byte, as int, b []byte, bs int, w, h int) int {
	if haveNEON && (w == 16 || w == 8) && h <= 16 && (h-1)*as+w <= len(a) && (h-1)*bs+w <= len(b) {
		var out int
		neonSAD(&a[0], as, &b[0], bs, w, h, &out)
		return out
	}
	return sadGo(a, as, b, bs, w, h)
}

func sadGo(a []byte, as int, b []byte, bs int, w, h int) int {
	s := 0
	for y := 0; y < h; y++ {
		ra, rb := a[y*as:y*as+w], b[y*bs:y*bs+w]
		for x := range ra {
			d := int(ra[x]) - int(rb[x])
			if d < 0 {
				d = -d
			}
			s += d
		}
	}
	return s
}

// ssd returns the sum of squared differences of a w x h block.
func ssd(a []byte, as int, b []byte, bs int, w, h int) int {
	s := 0
	for y := 0; y < h; y++ {
		ra, rb := a[y*as:y*as+w], b[y*bs:y*bs+w]
		for x := range ra {
			d := int(ra[x]) - int(rb[x])
			s += d * d
		}
	}
	return s
}

func abs32(v int32) int32 {
	if v < 0 {
		return -v
	}
	return v
}

func iabs(v int) int {
	if v < 0 {
		return -v
	}
	return v
}
