package h264

// Scan-order mapping tables and small derived-value helpers.

// zigzag4x4 maps scan position to raster position for a 4x4 block (Table 8-13).
var zigzag4x4 = [16]int{
	0, 1, 4, 8, 5, 2, 3, 6,
	9, 12, 13, 10, 7, 11, 14, 15,
}

// zigzag4x4AC is zigzag4x4 without the DC position (AC-only blocks).
var zigzag4x4AC = [15]int{
	1, 4, 8, 5, 2, 3, 6,
	9, 12, 13, 10, 7, 11, 14, 15,
}

// inverseRasterX4x4 / inverseRasterY4x4 map a 4x4 block index (0..15, in the
// hierarchical 8x8 scan) to its (x, y) position inside the macroblock.
var inverseRasterX4x4 = [16]int{
	0, 4, 0, 4, 8, 12, 8, 12,
	0, 4, 0, 4, 8, 12, 8, 12,
}

var inverseRasterY4x4 = [16]int{
	0, 0, 4, 4, 0, 0, 4, 4,
	8, 8, 12, 12, 8, 8, 12, 12,
}

// zScanToRaster maps the z-scan 4x4 block index to raster (row*4+col).
var zScanToRaster = [16]int{0, 1, 4, 5, 2, 3, 6, 7, 8, 9, 12, 13, 10, 11, 14, 15}

// topRightNotAvail4x4 marks 4x4 blocks whose upper-right samples lie in a
// later-decoded block inside the same macroblock.
var topRightNotAvail4x4 = [16]bool{
	false, false, false, true,
	false, false, false, false,
	false, false, false, true,
	false, false, false, false,
}

// I16x16 mb_type helpers (Table 7-11).
// mb_type = 1 + predMode + 4*cbpChroma + 12*(cbpLuma != 0)
func i16x16PredMode(mbType int) int  { return (mbType - 1) % 4 }
func i16x16CBPChroma(mbType int) int { return ((mbType - 1) / 4) % 3 }
func i16x16CBPLuma(mbType int) int {
	if (mbType-1)/12 > 0 {
		return 15
	}
	return 0
}

// chromaQP maps luma QP to chroma QP (Table 8-15).
func chromaQP(qpY int) int {
	if qpY < 0 {
		qpY = 0
	}
	if qpY < 30 {
		return qpY
	}
	if qpY > 51 {
		return 51
	}
	table := [22]int{29, 30, 31, 32, 32, 33, 34, 34, 35, 35, 36, 36,
		37, 37, 37, 38, 38, 38, 39, 39, 39, 39}
	return table[qpY-30]
}

// golombToIntra4x4CBP maps the ue(v) code used to signal coded_block_pattern
// in an I_NxN macroblock to the actual CBP (Table 9-4, intra column).
var golombToIntra4x4CBP = [48]uint8{
	47, 31, 15, 0, 23, 27, 29, 30, 7, 11, 13, 14, 39, 43, 45, 46,
	16, 3, 5, 10, 12, 19, 21, 26, 28, 35, 37, 42, 44, 1, 2, 4,
	8, 17, 18, 20, 24, 6, 9, 22, 25, 32, 33, 34, 36, 40, 38, 41,
}

// golombToInterCBP maps the ue(v) code used to signal coded_block_pattern in
// a non-I_NxN macroblock (inter) to the actual CBP (Table 9-4, inter column).
var golombToInterCBP = [48]uint8{
	0, 16, 1, 2, 4, 8, 32, 3, 5, 10, 12, 15, 47, 7, 11, 13,
	14, 6, 9, 31, 35, 37, 42, 44, 33, 34, 36, 40, 39, 43, 45, 46,
	17, 18, 20, 24, 19, 21, 26, 28, 23, 27, 29, 30, 22, 25, 38, 41,
}
