package h264

// Dequantization (spec 8.5.9) and inverse transforms (spec 8.5.10–8.5.12).
// Scaling-list support is not yet wired: the default flat list (all 16) is
// used.

// levelScale4x4 is Table 8-13, indexed by qP%6 and the levelScale position
// class (0=(0,0), 1=edges, 2=(1,1)).
var levelScale4x4 = [6][3]int32{
	{10, 13, 16},
	{11, 14, 18},
	{13, 16, 20},
	{14, 18, 23},
	{16, 20, 25},
	{18, 23, 29},
}

const defaultScalingDC = 16

func levelScaleIdx(row, col int) int {
	r, c := row&1, col&1
	if r == 0 && c == 0 {
		return 0
	}
	if r == 1 && c == 1 {
		return 2
	}
	return 1
}

// dequant4x4 inverse-quantizes a raster-order 4x4 block (default scaling).
func dequant4x4(coeffs [16]int32, qp int) [16]int32 {
	qpPer, qpRem := qp/6, qp%6
	var out [16]int32
	for i := 0; i < 16; i++ {
		if coeffs[i] == 0 {
			continue
		}
		s := levelScale4x4[qpRem][levelScaleIdx(i/4, i%4)] * defaultScalingDC
		if qpPer >= 4 {
			out[i] = coeffs[i] * s << uint(qpPer-4)
		} else {
			out[i] = (coeffs[i]*s + (1 << uint(3-qpPer))) >> uint(4-qpPer)
		}
	}
	return out
}

// dequantDC4x4 inverse-quantizes the 16 Intra16x16 luma DC coefficients
// after the inverse Hadamard transform (spec 8.5.9.2).
func dequantDC4x4(coeffs [16]int32, qp int) [16]int32 {
	qpPer, qpRem := qp/6, qp%6
	s := levelScale4x4[qpRem][0] * defaultScalingDC
	var out [16]int32
	if qpPer >= 6 {
		for i := 0; i < 16; i++ {
			out[i] = coeffs[i] * s << uint(qpPer-6)
		}
	} else {
		for i := 0; i < 16; i++ {
			out[i] = (coeffs[i]*s + (1 << uint(5-qpPer))) >> uint(6-qpPer)
		}
	}
	return out
}

// dequantChromaDC2x2 inverse-quantizes the 4 chroma DC coefficients after
// the 2x2 Hadamard transform (no rounding bias, matching reference).
func dequantChromaDC2x2(coeffs [4]int32, qpc int) [4]int32 {
	qpPer, qpRem := qpc/6, qpc%6
	s := levelScale4x4[qpRem][0] * defaultScalingDC
	var out [4]int32
	if qpPer >= 5 {
		for i := 0; i < 4; i++ {
			out[i] = coeffs[i] * s << uint(qpPer-5)
		}
	} else {
		for i := 0; i < 4; i++ {
			out[i] = (coeffs[i] * s) >> uint(5-qpPer)
		}
	}
	return out
}

// idct4x4 is the 4x4 inverse integer transform (spec 8.5.12). Input is the
// dequantized block in raster order; output is the spatial residual.
func idct4x4(coeffs [16]int32) [16]int32 {
	var b [16]int32
	copy(b[:], coeffs[:])
	b[0] += 32
	// Rows.
	for i := 0; i < 4; i++ {
		z0 := b[i*4+0] + b[i*4+2]
		z1 := b[i*4+0] - b[i*4+2]
		z2 := (b[i*4+1] >> 1) - b[i*4+3]
		z3 := b[i*4+1] + (b[i*4+3] >> 1)
		b[i*4+0] = z0 + z3
		b[i*4+1] = z1 + z2
		b[i*4+2] = z1 - z2
		b[i*4+3] = z0 - z3
	}
	// Columns.
	var out [16]int32
	for j := 0; j < 4; j++ {
		z0 := b[0*4+j] + b[2*4+j]
		z1 := b[0*4+j] - b[2*4+j]
		z2 := (b[1*4+j] >> 1) - b[3*4+j]
		z3 := b[1*4+j] + (b[3*4+j] >> 1)
		out[0*4+j] = (z0 + z3) >> 6
		out[1*4+j] = (z1 + z2) >> 6
		out[2*4+j] = (z1 - z2) >> 6
		out[3*4+j] = (z0 - z3) >> 6
	}
	return out
}

// hadamard4x4 is the raw 4x4 inverse Hadamard transform (spec 8.5.10.1).
// The QP scaling is applied afterwards by dequantDC4x4.
func hadamard4x4(coeffs [16]int32) [16]int32 {
	var t [16]int32
	for i := 0; i < 4; i++ {
		s0, s1, s2, s3 := coeffs[i*4+0], coeffs[i*4+1], coeffs[i*4+2], coeffs[i*4+3]
		t[i*4+0] = s0 + s1 + s2 + s3
		t[i*4+1] = s0 + s1 - s2 - s3
		t[i*4+2] = s0 - s1 - s2 + s3
		t[i*4+3] = s0 - s1 + s2 - s3
	}
	var out [16]int32
	for j := 0; j < 4; j++ {
		f0, f1, f2, f3 := t[0*4+j], t[1*4+j], t[2*4+j], t[3*4+j]
		out[0*4+j] = f0 + f1 + f2 + f3
		out[1*4+j] = f0 + f1 - f2 - f3
		out[2*4+j] = f0 - f1 - f2 + f3
		out[3*4+j] = f0 - f1 + f2 - f3
	}
	return out
}

// hadamard2x2 is the raw 2x2 inverse Hadamard transform (spec 8.5.11.1).
func hadamard2x2(coeffs [4]int32) [4]int32 {
	return [4]int32{
		coeffs[0] + coeffs[1] + coeffs[2] + coeffs[3],
		coeffs[0] - coeffs[1] + coeffs[2] - coeffs[3],
		coeffs[0] + coeffs[1] - coeffs[2] - coeffs[3],
		coeffs[0] - coeffs[1] - coeffs[2] + coeffs[3],
	}
}

func clip8(v int32) byte {
	if v < 0 {
		return 0
	}
	if v > 255 {
		return 255
	}
	return byte(v)
}
