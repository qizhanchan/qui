package vp9

// Inverse transforms, bit-exact with libvpx's C reference (vpx_dsp/inv_txfm.c).

const (
	c1  = 16364
	c2  = 16305
	c3  = 16207
	c4  = 16069
	c5  = 15893
	c6  = 15679
	c7  = 15426
	c8  = 15137
	c9  = 14811
	c10 = 14449
	c11 = 14053
	c12 = 13623
	c13 = 13160
	c14 = 12665
	c15 = 12140
	c16 = 11585
	c17 = 11003
	c18 = 10394
	c19 = 9760
	c20 = 9102
	c21 = 8423
	c22 = 7723
	c23 = 7005
	c24 = 6270
	c25 = 5520
	c26 = 4756
	c27 = 3981
	c28 = 3196
	c29 = 2404
	c30 = 1606
	c31 = 804

	sinpi19 = 5283
	sinpi29 = 9929
	sinpi39 = 13377
	sinpi49 = 15212
)

func rs(x int) int { return (x + (1 << 13)) >> 14 }

func clipPixel(v int) uint8 {
	if v < 0 {
		return 0
	}
	if v > 255 {
		return 255
	}
	return uint8(v)
}

func idct4(in, out []int) {
	_ = in[3]
	_ = out[3]
	s0 := rs((in[0] + in[2]) * c16)
	s1 := rs((in[0] - in[2]) * c16)
	s2 := rs(in[1]*c24 - in[3]*c8)
	s3 := rs(in[1]*c8 + in[3]*c24)
	out[0] = s0 + s3
	out[1] = s1 + s2
	out[2] = s1 - s2
	out[3] = s0 - s3
}

func iadst4(in, out []int) {
	_ = in[3]
	_ = out[3]
	x0, x1, x2, x3 := in[0], in[1], in[2], in[3]
	if x0|x1|x2|x3 == 0 {
		out[0], out[1], out[2], out[3] = 0, 0, 0, 0
		return
	}
	s0 := sinpi19 * x0
	s1 := sinpi29 * x0
	s2 := sinpi39 * x1
	s3 := sinpi49 * x2
	s4 := sinpi19 * x2
	s5 := sinpi29 * x3
	s6 := sinpi49 * x3
	s7 := x0 - x2 + x3
	s0 = s0 + s3 + s5
	s1 = s1 - s4 - s6
	s3 = s2
	s2 = sinpi39 * s7
	out[0] = rs(s0 + s3)
	out[1] = rs(s1 + s3)
	out[2] = rs(s2)
	out[3] = rs(s0 + s1 - s3)
}

func idct8(in, out []int) {
	_ = in[7]
	_ = out[7]
	var s1, s2 [8]int
	s1[0] = in[0]
	s1[2] = in[4]
	s1[1] = in[2]
	s1[3] = in[6]
	s1[4] = rs(in[1]*c28 - in[7]*c4)
	s1[7] = rs(in[1]*c4 + in[7]*c28)
	s1[5] = rs(in[5]*c12 - in[3]*c20)
	s1[6] = rs(in[5]*c20 + in[3]*c12)

	s2[0] = rs((s1[0] + s1[2]) * c16)
	s2[1] = rs((s1[0] - s1[2]) * c16)
	s2[2] = rs(s1[1]*c24 - s1[3]*c8)
	s2[3] = rs(s1[1]*c8 + s1[3]*c24)
	s2[4] = s1[4] + s1[5]
	s2[5] = s1[4] - s1[5]
	s2[6] = -s1[6] + s1[7]
	s2[7] = s1[6] + s1[7]

	s1[0] = s2[0] + s2[3]
	s1[1] = s2[1] + s2[2]
	s1[2] = s2[1] - s2[2]
	s1[3] = s2[0] - s2[3]
	s1[4] = s2[4]
	s1[5] = rs((s2[6] - s2[5]) * c16)
	s1[6] = rs((s2[5] + s2[6]) * c16)
	s1[7] = s2[7]

	out[0] = s1[0] + s1[7]
	out[1] = s1[1] + s1[6]
	out[2] = s1[2] + s1[5]
	out[3] = s1[3] + s1[4]
	out[4] = s1[3] - s1[4]
	out[5] = s1[2] - s1[5]
	out[6] = s1[1] - s1[6]
	out[7] = s1[0] - s1[7]
}

func iadst8(in, out []int) {
	_ = in[7]
	_ = out[7]
	x0, x1, x2, x3 := in[7], in[0], in[5], in[2]
	x4, x5, x6, x7 := in[3], in[4], in[1], in[6]
	if x0|x1|x2|x3|x4|x5|x6|x7 == 0 {
		for i := range out[:8] {
			out[i] = 0
		}
		return
	}
	s0 := c2*x0 + c30*x1
	s1 := c30*x0 - c2*x1
	s2 := c10*x2 + c22*x3
	s3 := c22*x2 - c10*x3
	s4 := c18*x4 + c14*x5
	s5 := c14*x4 - c18*x5
	s6 := c26*x6 + c6*x7
	s7 := c6*x6 - c26*x7

	x0 = rs(s0 + s4)
	x1 = rs(s1 + s5)
	x2 = rs(s2 + s6)
	x3 = rs(s3 + s7)
	x4 = rs(s0 - s4)
	x5 = rs(s1 - s5)
	x6 = rs(s2 - s6)
	x7 = rs(s3 - s7)

	s0, s1, s2, s3 = x0, x1, x2, x3
	s4 = c8*x4 + c24*x5
	s5 = c24*x4 - c8*x5
	s6 = -c24*x6 + c8*x7
	s7 = c8*x6 + c24*x7

	x0 = s0 + s2
	x1 = s1 + s3
	x2 = s0 - s2
	x3 = s1 - s3
	x4 = rs(s4 + s6)
	x5 = rs(s5 + s7)
	x6 = rs(s4 - s6)
	x7 = rs(s5 - s7)

	s2 = c16 * (x2 + x3)
	s3 = c16 * (x2 - x3)
	s6 = c16 * (x6 + x7)
	s7 = c16 * (x6 - x7)
	x2 = rs(s2)
	x3 = rs(s3)
	x6 = rs(s6)
	x7 = rs(s7)

	out[0] = x0
	out[1] = -x4
	out[2] = x6
	out[3] = -x2
	out[4] = x3
	out[5] = -x7
	out[6] = x5
	out[7] = -x1
}

func idct16(in, out []int) {
	_ = in[15]
	_ = out[15]
	var s1, s2 [16]int
	s1[0] = in[0]
	s1[1] = in[8]
	s1[2] = in[4]
	s1[3] = in[12]
	s1[4] = in[2]
	s1[5] = in[10]
	s1[6] = in[6]
	s1[7] = in[14]
	s1[8] = in[1]
	s1[9] = in[9]
	s1[10] = in[5]
	s1[11] = in[13]
	s1[12] = in[3]
	s1[13] = in[11]
	s1[14] = in[7]
	s1[15] = in[15]

	// stage 2
	s2[0], s2[1], s2[2], s2[3] = s1[0], s1[1], s1[2], s1[3]
	s2[4], s2[5], s2[6], s2[7] = s1[4], s1[5], s1[6], s1[7]
	s2[8] = rs(s1[8]*c30 - s1[15]*c2)
	s2[15] = rs(s1[8]*c2 + s1[15]*c30)
	s2[9] = rs(s1[9]*c14 - s1[14]*c18)
	s2[14] = rs(s1[9]*c18 + s1[14]*c14)
	s2[10] = rs(s1[10]*c22 - s1[13]*c10)
	s2[13] = rs(s1[10]*c10 + s1[13]*c22)
	s2[11] = rs(s1[11]*c6 - s1[12]*c26)
	s2[12] = rs(s1[11]*c26 + s1[12]*c6)

	// stage 3
	s1[0], s1[1], s1[2], s1[3] = s2[0], s2[1], s2[2], s2[3]
	s1[4] = rs(s2[4]*c28 - s2[7]*c4)
	s1[7] = rs(s2[4]*c4 + s2[7]*c28)
	s1[5] = rs(s2[5]*c12 - s2[6]*c20)
	s1[6] = rs(s2[5]*c20 + s2[6]*c12)
	s1[8] = s2[8] + s2[9]
	s1[9] = s2[8] - s2[9]
	s1[10] = -s2[10] + s2[11]
	s1[11] = s2[10] + s2[11]
	s1[12] = s2[12] + s2[13]
	s1[13] = s2[12] - s2[13]
	s1[14] = -s2[14] + s2[15]
	s1[15] = s2[14] + s2[15]

	// stage 4
	s2[0] = rs((s1[0] + s1[1]) * c16)
	s2[1] = rs((s1[0] - s1[1]) * c16)
	s2[2] = rs(s1[2]*c24 - s1[3]*c8)
	s2[3] = rs(s1[2]*c8 + s1[3]*c24)
	s2[4] = s1[4] + s1[5]
	s2[5] = s1[4] - s1[5]
	s2[6] = -s1[6] + s1[7]
	s2[7] = s1[6] + s1[7]
	s2[8] = s1[8]
	s2[15] = s1[15]
	s2[9] = rs(-s1[9]*c8 + s1[14]*c24)
	s2[14] = rs(s1[9]*c24 + s1[14]*c8)
	s2[10] = rs(-s1[10]*c24 - s1[13]*c8)
	s2[13] = rs(-s1[10]*c8 + s1[13]*c24)
	s2[11] = s1[11]
	s2[12] = s1[12]

	// stage 5
	s1[0] = s2[0] + s2[3]
	s1[1] = s2[1] + s2[2]
	s1[2] = s2[1] - s2[2]
	s1[3] = s2[0] - s2[3]
	s1[4] = s2[4]
	s1[5] = rs((s2[6] - s2[5]) * c16)
	s1[6] = rs((s2[5] + s2[6]) * c16)
	s1[7] = s2[7]
	s1[8] = s2[8] + s2[11]
	s1[9] = s2[9] + s2[10]
	s1[10] = s2[9] - s2[10]
	s1[11] = s2[8] - s2[11]
	s1[12] = -s2[12] + s2[15]
	s1[13] = -s2[13] + s2[14]
	s1[14] = s2[13] + s2[14]
	s1[15] = s2[12] + s2[15]

	// stage 6
	s2[0] = s1[0] + s1[7]
	s2[1] = s1[1] + s1[6]
	s2[2] = s1[2] + s1[5]
	s2[3] = s1[3] + s1[4]
	s2[4] = s1[3] - s1[4]
	s2[5] = s1[2] - s1[5]
	s2[6] = s1[1] - s1[6]
	s2[7] = s1[0] - s1[7]
	s2[8] = s1[8]
	s2[9] = s1[9]
	s2[10] = rs((-s1[10] + s1[13]) * c16)
	s2[13] = rs((s1[10] + s1[13]) * c16)
	s2[11] = rs((-s1[11] + s1[12]) * c16)
	s2[12] = rs((s1[11] + s1[12]) * c16)
	s2[14] = s1[14]
	s2[15] = s1[15]

	// stage 7
	for i := 0; i < 8; i++ {
		out[i] = s2[i] + s2[15-i]
		out[15-i] = s2[i] - s2[15-i]
	}
}

func iadst16(in, out []int) {
	_ = in[15]
	_ = out[15]
	x0, x1, x2, x3 := in[15], in[0], in[13], in[2]
	x4, x5, x6, x7 := in[11], in[4], in[9], in[6]
	x8, x9, x10, x11 := in[7], in[8], in[5], in[10]
	x12, x13, x14, x15 := in[3], in[12], in[1], in[14]
	if x0|x1|x2|x3|x4|x5|x6|x7|x8|x9|x10|x11|x12|x13|x14|x15 == 0 {
		for i := range out[:16] {
			out[i] = 0
		}
		return
	}
	// stage 1
	s0 := x0*c1 + x1*c31
	s1 := x0*c31 - x1*c1
	s2 := x2*c5 + x3*c27
	s3 := x2*c27 - x3*c5
	s4 := x4*c9 + x5*c23
	s5 := x4*c23 - x5*c9
	s6 := x6*c13 + x7*c19
	s7 := x6*c19 - x7*c13
	s8 := x8*c17 + x9*c15
	s9 := x8*c15 - x9*c17
	s10 := x10*c21 + x11*c11
	s11 := x10*c11 - x11*c21
	s12 := x12*c25 + x13*c7
	s13 := x12*c7 - x13*c25
	s14 := x14*c29 + x15*c3
	s15 := x14*c3 - x15*c29

	x0 = rs(s0 + s8)
	x1 = rs(s1 + s9)
	x2 = rs(s2 + s10)
	x3 = rs(s3 + s11)
	x4 = rs(s4 + s12)
	x5 = rs(s5 + s13)
	x6 = rs(s6 + s14)
	x7 = rs(s7 + s15)
	x8 = rs(s0 - s8)
	x9 = rs(s1 - s9)
	x10 = rs(s2 - s10)
	x11 = rs(s3 - s11)
	x12 = rs(s4 - s12)
	x13 = rs(s5 - s13)
	x14 = rs(s6 - s14)
	x15 = rs(s7 - s15)

	// stage 2
	s0, s1, s2, s3, s4, s5, s6, s7 = x0, x1, x2, x3, x4, x5, x6, x7
	s8 = x8*c4 + x9*c28
	s9 = x8*c28 - x9*c4
	s10 = x10*c20 + x11*c12
	s11 = x10*c12 - x11*c20
	s12 = -x12*c28 + x13*c4
	s13 = x12*c4 + x13*c28
	s14 = -x14*c12 + x15*c20
	s15 = x14*c20 + x15*c12

	x0 = s0 + s4
	x1 = s1 + s5
	x2 = s2 + s6
	x3 = s3 + s7
	x4 = s0 - s4
	x5 = s1 - s5
	x6 = s2 - s6
	x7 = s3 - s7
	x8 = rs(s8 + s12)
	x9 = rs(s9 + s13)
	x10 = rs(s10 + s14)
	x11 = rs(s11 + s15)
	x12 = rs(s8 - s12)
	x13 = rs(s9 - s13)
	x14 = rs(s10 - s14)
	x15 = rs(s11 - s15)

	// stage 3
	s0, s1, s2, s3 = x0, x1, x2, x3
	s4 = x4*c8 + x5*c24
	s5 = x4*c24 - x5*c8
	s6 = -x6*c24 + x7*c8
	s7 = x6*c8 + x7*c24
	s8, s9, s10, s11 = x8, x9, x10, x11
	s12 = x12*c8 + x13*c24
	s13 = x12*c24 - x13*c8
	s14 = -x14*c24 + x15*c8
	s15 = x14*c8 + x15*c24

	x0 = s0 + s2
	x1 = s1 + s3
	x2 = s0 - s2
	x3 = s1 - s3
	x4 = rs(s4 + s6)
	x5 = rs(s5 + s7)
	x6 = rs(s4 - s6)
	x7 = rs(s5 - s7)
	x8 = s8 + s10
	x9 = s9 + s11
	x10 = s8 - s10
	x11 = s9 - s11
	x12 = rs(s12 + s14)
	x13 = rs(s13 + s15)
	x14 = rs(s12 - s14)
	x15 = rs(s13 - s15)

	// stage 4
	s2 = -c16 * (x2 + x3)
	s3 = c16 * (x2 - x3)
	s6 = c16 * (x6 + x7)
	s7 = c16 * (-x6 + x7)
	s10 = c16 * (x10 + x11)
	s11 = c16 * (-x10 + x11)
	s14 = -c16 * (x14 + x15)
	s15 = c16 * (x14 - x15)

	x2 = rs(s2)
	x3 = rs(s3)
	x6 = rs(s6)
	x7 = rs(s7)
	x10 = rs(s10)
	x11 = rs(s11)
	x14 = rs(s14)
	x15 = rs(s15)

	out[0] = x0
	out[1] = -x8
	out[2] = x12
	out[3] = -x4
	out[4] = x6
	out[5] = x14
	out[6] = x10
	out[7] = x2
	out[8] = x3
	out[9] = x11
	out[10] = x15
	out[11] = x7
	out[12] = x5
	out[13] = -x13
	out[14] = x9
	out[15] = -x1
}

func idct32(in, out []int) {
	_ = in[31]
	_ = out[31]
	var s1, s2 [32]int
	// stage 1
	s1[0] = in[0]
	s1[1] = in[16]
	s1[2] = in[8]
	s1[3] = in[24]
	s1[4] = in[4]
	s1[5] = in[20]
	s1[6] = in[12]
	s1[7] = in[28]
	s1[8] = in[2]
	s1[9] = in[18]
	s1[10] = in[10]
	s1[11] = in[26]
	s1[12] = in[6]
	s1[13] = in[22]
	s1[14] = in[14]
	s1[15] = in[30]
	s1[16] = rs(in[1]*c31 - in[31]*c1)
	s1[31] = rs(in[1]*c1 + in[31]*c31)
	s1[17] = rs(in[17]*c15 - in[15]*c17)
	s1[30] = rs(in[17]*c17 + in[15]*c15)
	s1[18] = rs(in[9]*c23 - in[23]*c9)
	s1[29] = rs(in[9]*c9 + in[23]*c23)
	s1[19] = rs(in[25]*c7 - in[7]*c25)
	s1[28] = rs(in[25]*c25 + in[7]*c7)
	s1[20] = rs(in[5]*c27 - in[27]*c5)
	s1[27] = rs(in[5]*c5 + in[27]*c27)
	s1[21] = rs(in[21]*c11 - in[11]*c21)
	s1[26] = rs(in[21]*c21 + in[11]*c11)
	s1[22] = rs(in[13]*c19 - in[19]*c13)
	s1[25] = rs(in[13]*c13 + in[19]*c19)
	s1[23] = rs(in[29]*c3 - in[3]*c29)
	s1[24] = rs(in[29]*c29 + in[3]*c3)

	// stage 2
	copy(s2[:8], s1[:8])
	s2[8] = rs(s1[8]*c30 - s1[15]*c2)
	s2[15] = rs(s1[8]*c2 + s1[15]*c30)
	s2[9] = rs(s1[9]*c14 - s1[14]*c18)
	s2[14] = rs(s1[9]*c18 + s1[14]*c14)
	s2[10] = rs(s1[10]*c22 - s1[13]*c10)
	s2[13] = rs(s1[10]*c10 + s1[13]*c22)
	s2[11] = rs(s1[11]*c6 - s1[12]*c26)
	s2[12] = rs(s1[11]*c26 + s1[12]*c6)
	s2[16] = s1[16] + s1[17]
	s2[17] = s1[16] - s1[17]
	s2[18] = -s1[18] + s1[19]
	s2[19] = s1[18] + s1[19]
	s2[20] = s1[20] + s1[21]
	s2[21] = s1[20] - s1[21]
	s2[22] = -s1[22] + s1[23]
	s2[23] = s1[22] + s1[23]
	s2[24] = s1[24] + s1[25]
	s2[25] = s1[24] - s1[25]
	s2[26] = -s1[26] + s1[27]
	s2[27] = s1[26] + s1[27]
	s2[28] = s1[28] + s1[29]
	s2[29] = s1[28] - s1[29]
	s2[30] = -s1[30] + s1[31]
	s2[31] = s1[30] + s1[31]

	// stage 3
	copy(s1[:4], s2[:4])
	s1[4] = rs(s2[4]*c28 - s2[7]*c4)
	s1[7] = rs(s2[4]*c4 + s2[7]*c28)
	s1[5] = rs(s2[5]*c12 - s2[6]*c20)
	s1[6] = rs(s2[5]*c20 + s2[6]*c12)
	s1[8] = s2[8] + s2[9]
	s1[9] = s2[8] - s2[9]
	s1[10] = -s2[10] + s2[11]
	s1[11] = s2[10] + s2[11]
	s1[12] = s2[12] + s2[13]
	s1[13] = s2[12] - s2[13]
	s1[14] = -s2[14] + s2[15]
	s1[15] = s2[14] + s2[15]
	s1[16] = s2[16]
	s1[31] = s2[31]
	s1[17] = rs(-s2[17]*c4 + s2[30]*c28)
	s1[30] = rs(s2[17]*c28 + s2[30]*c4)
	s1[18] = rs(-s2[18]*c28 - s2[29]*c4)
	s1[29] = rs(-s2[18]*c4 + s2[29]*c28)
	s1[19] = s2[19]
	s1[20] = s2[20]
	s1[21] = rs(-s2[21]*c20 + s2[26]*c12)
	s1[26] = rs(s2[21]*c12 + s2[26]*c20)
	s1[22] = rs(-s2[22]*c12 - s2[25]*c20)
	s1[25] = rs(-s2[22]*c20 + s2[25]*c12)
	s1[23] = s2[23]
	s1[24] = s2[24]
	s1[27] = s2[27]
	s1[28] = s2[28]

	// stage 4
	s2[0] = rs((s1[0] + s1[1]) * c16)
	s2[1] = rs((s1[0] - s1[1]) * c16)
	s2[2] = rs(s1[2]*c24 - s1[3]*c8)
	s2[3] = rs(s1[2]*c8 + s1[3]*c24)
	s2[4] = s1[4] + s1[5]
	s2[5] = s1[4] - s1[5]
	s2[6] = -s1[6] + s1[7]
	s2[7] = s1[6] + s1[7]
	s2[8] = s1[8]
	s2[15] = s1[15]
	s2[9] = rs(-s1[9]*c8 + s1[14]*c24)
	s2[14] = rs(s1[9]*c24 + s1[14]*c8)
	s2[10] = rs(-s1[10]*c24 - s1[13]*c8)
	s2[13] = rs(-s1[10]*c8 + s1[13]*c24)
	s2[11] = s1[11]
	s2[12] = s1[12]
	s2[16] = s1[16] + s1[19]
	s2[17] = s1[17] + s1[18]
	s2[18] = s1[17] - s1[18]
	s2[19] = s1[16] - s1[19]
	s2[20] = -s1[20] + s1[23]
	s2[21] = -s1[21] + s1[22]
	s2[22] = s1[21] + s1[22]
	s2[23] = s1[20] + s1[23]
	s2[24] = s1[24] + s1[27]
	s2[25] = s1[25] + s1[26]
	s2[26] = s1[25] - s1[26]
	s2[27] = s1[24] - s1[27]
	s2[28] = -s1[28] + s1[31]
	s2[29] = -s1[29] + s1[30]
	s2[30] = s1[29] + s1[30]
	s2[31] = s1[28] + s1[31]

	// stage 5
	s1[0] = s2[0] + s2[3]
	s1[1] = s2[1] + s2[2]
	s1[2] = s2[1] - s2[2]
	s1[3] = s2[0] - s2[3]
	s1[4] = s2[4]
	s1[5] = rs((s2[6] - s2[5]) * c16)
	s1[6] = rs((s2[5] + s2[6]) * c16)
	s1[7] = s2[7]
	s1[8] = s2[8] + s2[11]
	s1[9] = s2[9] + s2[10]
	s1[10] = s2[9] - s2[10]
	s1[11] = s2[8] - s2[11]
	s1[12] = -s2[12] + s2[15]
	s1[13] = -s2[13] + s2[14]
	s1[14] = s2[13] + s2[14]
	s1[15] = s2[12] + s2[15]
	s1[16] = s2[16]
	s1[17] = s2[17]
	s1[18] = rs(-s2[18]*c8 + s2[29]*c24)
	s1[29] = rs(s2[18]*c24 + s2[29]*c8)
	s1[19] = rs(-s2[19]*c8 + s2[28]*c24)
	s1[28] = rs(s2[19]*c24 + s2[28]*c8)
	s1[20] = rs(-s2[20]*c24 - s2[27]*c8)
	s1[27] = rs(-s2[20]*c8 + s2[27]*c24)
	s1[21] = rs(-s2[21]*c24 - s2[26]*c8)
	s1[26] = rs(-s2[21]*c8 + s2[26]*c24)
	s1[22] = s2[22]
	s1[23] = s2[23]
	s1[24] = s2[24]
	s1[25] = s2[25]
	s1[30] = s2[30]
	s1[31] = s2[31]

	// stage 6
	s2[0] = s1[0] + s1[7]
	s2[1] = s1[1] + s1[6]
	s2[2] = s1[2] + s1[5]
	s2[3] = s1[3] + s1[4]
	s2[4] = s1[3] - s1[4]
	s2[5] = s1[2] - s1[5]
	s2[6] = s1[1] - s1[6]
	s2[7] = s1[0] - s1[7]
	s2[8] = s1[8]
	s2[9] = s1[9]
	s2[10] = rs((-s1[10] + s1[13]) * c16)
	s2[13] = rs((s1[10] + s1[13]) * c16)
	s2[11] = rs((-s1[11] + s1[12]) * c16)
	s2[12] = rs((s1[11] + s1[12]) * c16)
	s2[14] = s1[14]
	s2[15] = s1[15]
	s2[16] = s1[16] + s1[23]
	s2[17] = s1[17] + s1[22]
	s2[18] = s1[18] + s1[21]
	s2[19] = s1[19] + s1[20]
	s2[20] = s1[19] - s1[20]
	s2[21] = s1[18] - s1[21]
	s2[22] = s1[17] - s1[22]
	s2[23] = s1[16] - s1[23]
	s2[24] = -s1[24] + s1[31]
	s2[25] = -s1[25] + s1[30]
	s2[26] = -s1[26] + s1[29]
	s2[27] = -s1[27] + s1[28]
	s2[28] = s1[27] + s1[28]
	s2[29] = s1[26] + s1[29]
	s2[30] = s1[25] + s1[30]
	s2[31] = s1[24] + s1[31]

	// stage 7
	for i := 0; i < 8; i++ {
		s1[i] = s2[i] + s2[15-i]
		s1[15-i] = s2[i] - s2[15-i]
	}
	s1[16] = s2[16]
	s1[17] = s2[17]
	s1[18] = s2[18]
	s1[19] = s2[19]
	s1[20] = rs((-s2[20] + s2[27]) * c16)
	s1[27] = rs((s2[20] + s2[27]) * c16)
	s1[21] = rs((-s2[21] + s2[26]) * c16)
	s1[26] = rs((s2[21] + s2[26]) * c16)
	s1[22] = rs((-s2[22] + s2[25]) * c16)
	s1[25] = rs((s2[22] + s2[25]) * c16)
	s1[23] = rs((-s2[23] + s2[24]) * c16)
	s1[24] = rs((s2[23] + s2[24]) * c16)
	s1[28] = s2[28]
	s1[29] = s2[29]
	s1[30] = s2[30]
	s1[31] = s2[31]

	// final stage
	for i := 0; i < 16; i++ {
		out[i] = s1[i] + s1[31-i]
		out[31-i] = s1[i] - s1[31-i]
	}
}

type tx1d func(in, out []int)

var txRows = [4][4]tx1d{
	{idct4, idct4, iadst4, iadst4},
	{idct8, idct8, iadst8, iadst8},
	{idct16, idct16, iadst16, iadst16},
	{idct32, idct32, idct32, idct32},
}

var txCols = [4][4]tx1d{
	{idct4, iadst4, idct4, iadst4},
	{idct8, iadst8, idct8, iadst8},
	{idct16, iadst16, idct16, iadst16},
	{idct32, idct32, idct32, idct32},
}

// inverseTransform adds the inverse transform of t.coef to dst and clears
// the coefficients it used.
func (t *tileState) inverseTransform(tx, txType, eob int, so *scanOrder, dst []byte, stride int) {
	n := 4 << tx
	coef := t.coef[:n*n]
	t.itx(tx, txType, eob, n, coef, dst, stride)
	if eob*4 > n*n {
		clear(coef)
	} else {
		for _, p := range so.scan[:eob] {
			coef[p] = 0
		}
	}
}

func (t *tileState) itx(tx, txType, eob, n int, coef []int32, dst []byte, stride int) {
	if t.d.hdr.lossless {
		iwht4x4(coef, dst, stride)
		return
	}
	shift := 4
	switch tx {
	case 1:
		shift = 5
	case 2, 3:
		shift = 6
	}
	rnd := 1 << (shift - 1)
	if eob == 1 && txType == 0 {
		// DC only (vpx_idctNxN_1_add).
		o := rs(int(coef[0]) * c16)
		o = rs(o * c16)
		a := (o + rnd) >> shift
		for y := 0; y < n; y++ {
			row := dst[y*stride : y*stride+n]
			for x := range row {
				row[x] = clipPixel(int(row[x]) + a)
			}
		}
		return
	}
	tmp := t.itxBuf[:n*n]
	in, out := t.itxIn[:n], t.itxOut[:n]
	rowF, colF := txRows[tx][txType], txCols[tx][txType]
	// Rows below the last nonzero one transform to zero.
	lastRow := -1
	for i := n*n - 1; i >= 0; i-- {
		if coef[i] != 0 {
			lastRow = i / n
			break
		}
	}
	clear(tmp[(lastRow+1)*n:])
	for i := 0; i <= lastRow; i++ {
		src := coef[i*n : i*n+n]
		nz := false
		for j, v := range src {
			in[j] = int(v)
			if v != 0 {
				nz = true
			}
		}
		if !nz {
			clear(tmp[i*n : i*n+n])
			continue
		}
		rowF(in, tmp[i*n:i*n+n])
	}
	for j := 0; j < n; j++ {
		for i := 0; i < n; i++ {
			in[i] = tmp[i*n+j]
		}
		colF(in, out)
		for i := 0; i < n; i++ {
			p := &dst[i*stride+j]
			*p = clipPixel(int(*p) + (out[i]+rnd)>>shift)
		}
	}
}

// iwht4x4 is the lossless inverse Walsh–Hadamard transform.
func iwht4x4(coef []int32, dst []byte, stride int) {
	var tmp [16]int
	for i := 0; i < 4; i++ {
		ip := coef[i*4:]
		a1 := int(ip[0]) >> 2
		c1 := int(ip[1]) >> 2
		d1 := int(ip[2]) >> 2
		b1 := int(ip[3]) >> 2
		a1 += c1
		d1 -= b1
		e1 := (a1 - d1) >> 1
		b1 = e1 - b1
		c1 = e1 - c1
		a1 -= b1
		d1 += c1
		tmp[i*4], tmp[i*4+1], tmp[i*4+2], tmp[i*4+3] = a1, b1, c1, d1
	}
	for i := 0; i < 4; i++ {
		a1 := tmp[i]
		c1 := tmp[4+i]
		d1 := tmp[8+i]
		b1 := tmp[12+i]
		a1 += c1
		d1 -= b1
		e1 := (a1 - d1) >> 1
		b1 = e1 - b1
		c1 = e1 - c1
		a1 -= b1
		d1 += c1
		dst[i] = clipPixel(int(dst[i]) + a1)
		dst[stride+i] = clipPixel(int(dst[stride+i]) + b1)
		dst[2*stride+i] = clipPixel(int(dst[2*stride+i]) + c1)
		dst[3*stride+i] = clipPixel(int(dst[3*stride+i]) + d1)
	}
}
