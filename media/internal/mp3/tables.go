package mp3

import "math"

// Scale factor band widths per sample-rate index (ISO/IEC 11172-3 Table
// B.8, ISO/IEC 13818-3 Table B.2; the 8 kHz MPEG-2.5 rows follow FFmpeg
// and libmpg123).
var bandSizeLong = [9][22]int{
	{4, 4, 4, 4, 4, 4, 6, 6, 8, 8, 10, 12, 16, 20, 24, 28, 34, 42, 50, 54, 76, 158},
	{4, 4, 4, 4, 4, 4, 6, 6, 6, 8, 10, 12, 16, 18, 22, 28, 34, 40, 46, 54, 54, 192},
	{4, 4, 4, 4, 4, 4, 6, 6, 8, 10, 12, 16, 20, 24, 30, 38, 46, 56, 68, 84, 102, 26},
	{6, 6, 6, 6, 6, 6, 8, 10, 12, 14, 16, 20, 24, 28, 32, 38, 46, 52, 60, 68, 58, 54},
	{6, 6, 6, 6, 6, 6, 8, 10, 12, 14, 16, 18, 22, 26, 32, 38, 46, 54, 62, 70, 76, 36},
	{6, 6, 6, 6, 6, 6, 8, 10, 12, 14, 16, 20, 24, 28, 32, 38, 46, 52, 60, 68, 58, 54},
	{6, 6, 6, 6, 6, 6, 8, 10, 12, 14, 16, 20, 24, 28, 32, 38, 46, 52, 60, 68, 58, 54},
	{6, 6, 6, 6, 6, 6, 8, 10, 12, 14, 16, 20, 24, 28, 32, 38, 46, 52, 60, 68, 58, 54},
	{12, 12, 12, 12, 12, 12, 16, 20, 24, 28, 32, 40, 48, 56, 64, 76, 90, 2, 2, 2, 2, 2},
}

var bandSizeShort = [9][13]int{
	{4, 4, 4, 4, 6, 8, 10, 12, 14, 18, 22, 30, 56},
	{4, 4, 4, 4, 6, 6, 10, 12, 14, 16, 20, 26, 66},
	{4, 4, 4, 4, 6, 8, 12, 16, 20, 26, 34, 42, 12},
	{4, 4, 4, 6, 6, 8, 10, 14, 18, 26, 32, 42, 18},
	{4, 4, 4, 6, 8, 10, 12, 14, 18, 24, 32, 44, 12},
	{4, 4, 4, 6, 8, 10, 12, 14, 18, 24, 30, 40, 18},
	{4, 4, 4, 6, 8, 10, 12, 14, 18, 24, 30, 40, 18},
	{4, 4, 4, 6, 8, 10, 12, 14, 18, 24, 30, 40, 18},
	{8, 8, 8, 12, 16, 20, 24, 28, 36, 2, 2, 2, 26},
}

// bandIndexLong[sr][i] is the first line of long band i (23 entries).
var bandIndexLong [9][23]int

var pretab = [22]int{0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 1, 1, 1, 1, 2, 2, 3, 3, 3, 2, 0}

var slenTable = [2][16]int{
	{0, 0, 0, 0, 3, 1, 1, 1, 2, 2, 2, 3, 3, 3, 4, 4},
	{0, 1, 2, 3, 0, 1, 2, 3, 1, 2, 3, 1, 2, 3, 2, 3},
}

// lsfNsfTable[tindex][blockKind][part]: number of scale factor bands per
// slen partition (ISO/IEC 13818-3 Table B.4); blockKind 0 long, 1 short,
// 2 mixed.
var lsfNsfTable = [6][3][4]int{
	{{6, 5, 5, 5}, {9, 9, 9, 9}, {6, 9, 9, 9}},
	{{6, 5, 7, 3}, {9, 9, 12, 6}, {6, 9, 12, 6}},
	{{11, 10, 0, 0}, {18, 18, 0, 0}, {15, 18, 0, 0}},
	{{7, 7, 7, 0}, {12, 12, 12, 0}, {6, 15, 12, 0}},
	{{6, 6, 6, 3}, {12, 9, 9, 6}, {6, 12, 9, 6}},
	{{8, 8, 5, 0}, {15, 12, 9, 0}, {6, 18, 9, 0}},
}

// Layer III Huffman table parameters: the decode table each table_select
// uses and its linbits. Tables 4 and 14 do not exist; like FFmpeg they
// decode as all-zero regions.
var huffSelect = [32]struct{ tab, linbits int }{
	{0, 0}, {1, 0}, {2, 0}, {3, 0}, {0, 0}, {5, 0}, {6, 0}, {7, 0},
	{8, 0}, {9, 0}, {10, 0}, {11, 0}, {12, 0}, {13, 0}, {0, 0}, {15, 0},
	{16, 1}, {16, 2}, {16, 3}, {16, 4}, {16, 6}, {16, 8}, {16, 10}, {16, 13},
	{24, 4}, {24, 5}, {24, 6}, {24, 7}, {24, 8}, {24, 9}, {24, 11}, {24, 13},
}

var (
	pow43Table [8207]float64 // |x|^(4/3) for 0..15+8191
	expTable   [512]float64  // 2^((e-400)/4)
	isTable    [2][7]float64 // MPEG-1 intensity stereo (left, right) ratios
	isTableLSF [2][2][16]float64
	csTable    [8]float64 // alias reduction
	caTable    [8]float64
	imdctWin   [4][36]float64 // block types 0..3 (2 = short, 12 taps)
	cos36      [18][36]float64
	cos12      [6][12]float64
	synthWin   [512]float64
	synthCos   [64][32]float64
	l12Scale   [64]float64 // Layer I/II scale factors (index 63 unused)
)

func init() {
	for sr := 0; sr < 9; sr++ {
		k := 0
		for i := 0; i < 22; i++ {
			bandIndexLong[sr][i] = k
			k += bandSizeLong[sr][i]
		}
		bandIndexLong[sr][22] = k
	}
	for i := range pow43Table {
		pow43Table[i] = math.Pow(float64(i), 4.0/3.0)
	}
	for i := range expTable {
		expTable[i] = math.Exp2(float64(i-400) / 4)
	}
	for i := 0; i < 7; i++ {
		var v float64
		if i != 6 {
			f := math.Tan(float64(i) * math.Pi / 12)
			v = f / (1 + f)
		} else {
			v = 1
		}
		isTable[0][i] = v
		isTable[1][6-i] = v
	}
	for i := 0; i < 16; i++ {
		for j := 0; j < 2; j++ {
			e := -(j + 1) * ((i + 1) >> 1)
			f := math.Exp2(float64(e) / 4)
			k := i & 1
			isTableLSF[j][k^1][i] = f
			isTableLSF[j][k][i] = 1
		}
	}
	ci := [8]float64{-0.6, -0.535, -0.33, -0.185, -0.095, -0.041, -0.0142, -0.0037}
	for i, c := range ci {
		sq := math.Sqrt(1 + c*c)
		csTable[i] = 1 / sq
		caTable[i] = c / sq
	}
	for i := 0; i < 36; i++ {
		imdctWin[0][i] = math.Sin(math.Pi / 36 * (float64(i) + 0.5))
	}
	for i := 0; i < 36; i++ {
		switch {
		case i < 18:
			imdctWin[1][i] = math.Sin(math.Pi / 36 * (float64(i) + 0.5))
		case i < 24:
			imdctWin[1][i] = 1
		case i < 30:
			imdctWin[1][i] = math.Sin(math.Pi / 12 * (float64(i-18) + 0.5))
		}
		switch {
		case i < 6:
		case i < 12:
			imdctWin[3][i] = math.Sin(math.Pi / 12 * (float64(i-6) + 0.5))
		case i < 18:
			imdctWin[3][i] = 1
		default:
			imdctWin[3][i] = math.Sin(math.Pi / 36 * (float64(i) + 0.5))
		}
	}
	for i := 0; i < 12; i++ {
		imdctWin[2][i] = math.Sin(math.Pi / 12 * (float64(i) + 0.5))
	}
	for k := 0; k < 18; k++ {
		for i := 0; i < 36; i++ {
			cos36[k][i] = math.Cos(math.Pi / 72 * float64((2*i+1+18)*(2*k+1)))
		}
	}
	for k := 0; k < 6; k++ {
		for i := 0; i < 12; i++ {
			cos12[k][i] = math.Cos(math.Pi / 24 * float64((2*i+1+6)*(2*k+1)))
		}
	}
	for i := 0; i <= 256; i++ {
		v := float64(synthWindowInt[i]) / 65536
		synthWin[i] = v
		if i&63 != 0 {
			v = -v
		}
		if i != 0 {
			synthWin[512-i] = v
		}
	}
	for i := 0; i < 64; i++ {
		for k := 0; k < 32; k++ {
			synthCos[i][k] = math.Cos(float64((16+i)*(2*k+1)) * math.Pi / 64)
		}
	}
	for i := 0; i < 63; i++ {
		l12Scale[i] = math.Exp2(1 - float64(i)/3)
	}
}
