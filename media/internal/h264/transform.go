package h264

// Inverse quantisation (spec 8.5.9, with scaling matrices) and inverse
// transforms (spec 8.5.10–8.5.13).

var normAdjust4 = [6][3]int32{
	{10, 16, 13}, {11, 18, 14}, {13, 20, 16}, {14, 23, 18}, {16, 25, 20}, {18, 29, 23},
}

var normAdjust8 = [6][6]int32{
	{20, 18, 32, 19, 25, 24},
	{22, 19, 35, 21, 28, 26},
	{26, 23, 42, 24, 33, 31},
	{28, 25, 45, 26, 35, 33},
	{32, 28, 51, 30, 40, 38},
	{36, 32, 58, 34, 46, 43},
}

func norm4Class(i int) int {
	r, c := i>>2, i&3
	switch {
	case r&1 == 0 && c&1 == 0:
		return 0
	case r&1 == 1 && c&1 == 1:
		return 1
	}
	return 2
}

func norm8Class(i int) int {
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

// dequantTables holds LevelScale << (qP/6) for every list, qP and position
// (raster order). Lists: 0..5 = Intra Y/Cb/Cr, Inter Y/Cb/Cr; 8x8: 0 Intra
// Y, 1 Inter Y.
type dequantTables struct {
	s4 [6][16]uint8
	s8 [2][64]uint8
	q4 [6][52][16]int32
	q8 [2][52][64]int32
}

func newDequantTables(pps *PPS) *dequantTables {
	t := &dequantTables{s4: pps.Scaling4}
	t.s8[0], t.s8[1] = pps.Scaling8[0], pps.Scaling8[1]
	for m := 0; m < 6; m++ {
		for qp := 0; qp < 52; qp++ {
			for i := 0; i < 16; i++ {
				t.q4[m][qp][i] = int32(t.s4[m][i]) * normAdjust4[qp%6][norm4Class(i)] << uint(qp/6)
			}
		}
	}
	for m := 0; m < 2; m++ {
		for qp := 0; qp < 52; qp++ {
			for i := 0; i < 64; i++ {
				t.q8[m][qp][i] = int32(t.s8[m][i]) * normAdjust8[qp%6][norm8Class(i)] << uint(qp/6)
			}
		}
	}
	return t
}

func (t *dequantTables) same(pps *PPS) bool {
	return t.s4 == pps.Scaling4 && t.s8[0] == pps.Scaling8[0] && t.s8[1] == pps.Scaling8[1]
}

// idct4Add applies the 4x4 inverse transform to blk (raster, dequantised)
// and adds the result to dst; blk is cleared.
func idct4Add(dst []byte, stride int, blk *[16]int32) {
	if haveNEON && 3*stride+4 <= len(dst) {
		neonIdct4Add(&dst[0], stride, blk)
		return
	}
	idct4AddGo(dst, stride, blk)
}

func idct4AddGo(dst []byte, stride int, blk *[16]int32) {
	var t [16]int32
	for i := 0; i < 4; i++ {
		b0, b1, b2, b3 := blk[i*4], blk[i*4+1], blk[i*4+2], blk[i*4+3]
		z0, z1 := b0+b2, b0-b2
		z2, z3 := (b1>>1)-b3, b1+(b3>>1)
		t[i*4], t[i*4+1], t[i*4+2], t[i*4+3] = z0+z3, z1+z2, z1-z2, z0-z3
	}
	for j := 0; j < 4; j++ {
		b0, b1, b2, b3 := t[j], t[4+j], t[8+j], t[12+j]
		z0, z1 := b0+b2, b0-b2
		z2, z3 := (b1>>1)-b3, b1+(b3>>1)
		d := dst[j:]
		d[0] = clipU8(int32(d[0]) + (z0+z3+32)>>6)
		d[stride] = clipU8(int32(d[stride]) + (z1+z2+32)>>6)
		d[2*stride] = clipU8(int32(d[2*stride]) + (z1-z2+32)>>6)
		d[3*stride] = clipU8(int32(d[3*stride]) + (z0-z3+32)>>6)
	}
	*blk = [16]int32{}
}

// idct8Add is the 8x8 counterpart of idct4Add.
func idct8Add(dst []byte, stride int, blk *[64]int32) {
	var t [64]int32
	for i := 0; i < 8; i++ {
		r := blk[i*8 : i*8+8]
		f := idct8Row(r[0], r[1], r[2], r[3], r[4], r[5], r[6], r[7])
		copy(t[i*8:], f[:])
	}
	for j := 0; j < 8; j++ {
		f := idct8Row(t[j], t[8+j], t[16+j], t[24+j], t[32+j], t[40+j], t[48+j], t[56+j])
		for i := 0; i < 8; i++ {
			p := &dst[i*stride+j]
			*p = clipU8(int32(*p) + (f[i]+32)>>6)
		}
	}
	*blk = [64]int32{}
}

func idct8Row(a0, a1, a2, a3, a4, a5, a6, a7 int32) [8]int32 {
	e0 := a0 + a4
	e1 := -a3 + a5 - a7 - (a7 >> 1)
	e2 := a0 - a4
	e3 := a1 + a7 - a3 - (a3 >> 1)
	e4 := (a2 >> 1) - a6
	e5 := -a1 + a7 + a5 + (a5 >> 1)
	e6 := a2 + (a6 >> 1)
	e7 := a3 + a5 + a1 + (a1 >> 1)
	f0 := e0 + e6
	f1 := e1 + (e7 >> 2)
	f2 := e2 + e4
	f3 := e3 + (e5 >> 2)
	f4 := e2 - e4
	f5 := (e3 >> 2) - e5
	f6 := e0 - e6
	f7 := e7 - (e1 >> 2)
	return [8]int32{f0 + f7, f2 + f5, f4 + f3, f6 + f1, f6 - f1, f4 - f3, f2 - f5, f0 - f7}
}

// hadamard4 is the inverse 4x4 Hadamard of the Intra16x16 DC levels (raster).
func hadamard4(c *[16]int32) {
	var t [16]int32
	for i := 0; i < 4; i++ {
		s0, s1, s2, s3 := c[i*4], c[i*4+1], c[i*4+2], c[i*4+3]
		t[i*4] = s0 + s1 + s2 + s3
		t[i*4+1] = s0 + s1 - s2 - s3
		t[i*4+2] = s0 - s1 - s2 + s3
		t[i*4+3] = s0 - s1 + s2 - s3
	}
	for j := 0; j < 4; j++ {
		f0, f1, f2, f3 := t[j], t[4+j], t[8+j], t[12+j]
		c[j] = f0 + f1 + f2 + f3
		c[4+j] = f0 + f1 - f2 - f3
		c[8+j] = f0 - f1 - f2 + f3
		c[12+j] = f0 - f1 + f2 - f3
	}
}
