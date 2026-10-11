package h264

// zigzag4x4 maps a frame zig-zag scan position to its raster position.
var zigzag4x4 = [16]uint8{0, 1, 4, 8, 5, 2, 3, 6, 9, 12, 13, 10, 7, 11, 14, 15}

// zigzag8x8 is the 8x8 frame zig-zag scan.
var zigzag8x8 = [64]uint8{
	0, 1, 8, 16, 9, 2, 3, 10, 17, 24, 32, 25, 18, 11, 4, 5,
	12, 19, 26, 33, 40, 48, 41, 34, 27, 20, 13, 6, 7, 14, 21, 28,
	35, 42, 49, 56, 57, 50, 43, 36, 29, 22, 15, 23, 30, 37, 44, 51,
	58, 59, 52, 45, 38, 31, 39, 46, 53, 60, 61, 54, 47, 55, 62, 63,
}

// blkX / blkY give the position, in 4x4-block units, of luma 4x4 block
// blkIdx (z-scan order, spec 6.4.3) inside its macroblock.
var (
	blkX = [16]uint8{0, 1, 0, 1, 2, 3, 2, 3, 0, 1, 0, 1, 2, 3, 2, 3}
	blkY = [16]uint8{0, 0, 1, 1, 0, 0, 1, 1, 2, 2, 3, 3, 2, 2, 3, 3}
)

// rasterToBlk maps a raster 4x4 position (y*4+x) to its z-scan index.
var rasterToBlk = [16]uint8{0, 1, 4, 5, 2, 3, 6, 7, 8, 9, 12, 13, 10, 11, 14, 15}

// golombToIntraCBP / golombToInterCBP map the me(v) codeNum of
// coded_block_pattern to the CBP for ChromaArrayType 1/2 (Table 9-4).
var golombToIntraCBP = [48]uint8{
	47, 31, 15, 0, 23, 27, 29, 30, 7, 11, 13, 14, 39, 43, 45, 46,
	16, 3, 5, 10, 12, 19, 21, 26, 28, 35, 37, 42, 44, 1, 2, 4,
	8, 17, 18, 20, 24, 6, 9, 22, 25, 32, 33, 34, 36, 40, 38, 41,
}

var golombToInterCBP = [48]uint8{
	0, 16, 1, 2, 4, 8, 32, 3, 5, 10, 12, 15, 47, 7, 11, 13,
	14, 6, 9, 31, 35, 37, 42, 44, 33, 34, 36, 40, 39, 43, 45, 46,
	17, 18, 20, 24, 19, 21, 26, 28, 23, 27, 29, 30, 22, 25, 38, 41,
}

// chromaQPTable maps qPI (0..51) to QPc (Table 8-15).
var chromaQPTable = [52]uint8{
	0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15,
	16, 17, 18, 19, 20, 21, 22, 23, 24, 25, 26, 27, 28, 29,
	29, 30, 31, 32, 32, 33, 34, 34, 35, 35, 36, 36,
	37, 37, 37, 38, 38, 38, 39, 39, 39, 39,
}

func chromaQP(qp, offset int) int {
	return int(chromaQPTable[clip3(0, 51, qp+offset)])
}

func clip3(lo, hi, v int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

func clipU8(v int32) uint8 {
	if uint32(v) > 255 {
		if v < 0 {
			return 0
		}
		return 255
	}
	return uint8(v)
}

func absInt(v int) int {
	if v < 0 {
		return -v
	}
	return v
}
