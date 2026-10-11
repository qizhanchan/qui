package h264

// 8x8 inverse quantization (spec 8.5.9.3) and inverse transform (spec 8.5.13)
// for the High-profile 8x8 transform, using the default scaling list.

// levelScale8x8 is Table 8-14.
var levelScale8x8 = [6][6]int32{
	{20, 18, 32, 19, 25, 24},
	{22, 19, 35, 21, 28, 26},
	{26, 23, 42, 24, 33, 31},
	{28, 25, 45, 26, 35, 33},
	{32, 28, 51, 30, 40, 38},
	{36, 32, 58, 34, 46, 43},
}

// normAdjust8x8 maps a coefficient position to its levelScale8x8 column.
var normAdjust8x8 = [4][4]int{
	{0, 3, 4, 3},
	{3, 1, 5, 1},
	{4, 5, 2, 5},
	{3, 1, 5, 1},
}

// dequant8x8 inverse-quantizes a raster-order 8x8 block (default scaling).
func dequant8x8(coeffs [64]int32, qp int) [64]int32 {
	qpPer, qpRem := qp/6, qp%6
	var out [64]int32
	for i := 0; i < 64; i++ {
		if coeffs[i] == 0 {
			continue
		}
		v := normAdjust8x8[(i/8)%4][(i%8)%4]
		s := levelScale8x8[qpRem][v] * defaultScalingDC
		if qpPer >= 6 {
			out[i] = coeffs[i] * s << uint(qpPer-6)
		} else {
			out[i] = (coeffs[i]*s + (1 << uint(5-qpPer))) >> uint(6-qpPer)
		}
	}
	return out
}

// idct8x8 is the 8x8 inverse integer transform (spec 8.5.13).
func idct8x8(coeffs [64]int32) [64]int32 {
	var temp [64]int32
	for i := 0; i < 8; i++ {
		a0, a1, a2, a3 := coeffs[i*8+0], coeffs[i*8+1], coeffs[i*8+2], coeffs[i*8+3]
		a4, a5, a6, a7 := coeffs[i*8+4], coeffs[i*8+5], coeffs[i*8+6], coeffs[i*8+7]

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

		temp[i*8+0] = f0 + f7
		temp[i*8+1] = f2 + f5
		temp[i*8+2] = f4 + f3
		temp[i*8+3] = f6 + f1
		temp[i*8+4] = f6 - f1
		temp[i*8+5] = f4 - f3
		temp[i*8+6] = f2 - f5
		temp[i*8+7] = f0 - f7
	}

	var out [64]int32
	for j := 0; j < 8; j++ {
		a0, a1, a2, a3 := temp[0*8+j], temp[1*8+j], temp[2*8+j], temp[3*8+j]
		a4, a5, a6, a7 := temp[4*8+j], temp[5*8+j], temp[6*8+j], temp[7*8+j]

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

		out[0*8+j] = (f0 + f7 + 32) >> 6
		out[1*8+j] = (f2 + f5 + 32) >> 6
		out[2*8+j] = (f4 + f3 + 32) >> 6
		out[3*8+j] = (f6 + f1 + 32) >> 6
		out[4*8+j] = (f6 - f1 + 32) >> 6
		out[5*8+j] = (f4 - f3 + 32) >> 6
		out[6*8+j] = (f2 - f5 + 32) >> 6
		out[7*8+j] = (f0 - f7 + 32) >> 6
	}
	return out
}

// zigzagScan8x8CAVLC maps a CAVLC 8x8 sequential position to a raster position.
var zigzagScan8x8CAVLC = [64]int{
	0, 9, 17, 18, 12, 40, 27, 7,
	35, 57, 29, 30, 58, 38, 53, 47,
	1, 2, 24, 11, 19, 48, 20, 14,
	42, 50, 22, 37, 59, 31, 60, 55,
	8, 3, 32, 4, 26, 41, 13, 21,
	49, 43, 15, 44, 52, 39, 61, 62,
	16, 10, 25, 5, 33, 34, 6, 28,
	56, 36, 23, 51, 45, 46, 54, 63,
}
