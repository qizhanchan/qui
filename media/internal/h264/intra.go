package h264

// Intra prediction (spec 8.3.1–8.3.4). Reference-sample gathering and
// availability live in the macroblock layer; these functions are pure
// predictors over already-assembled neighbour samples.

// Intra16x16 modes.
const (
	I16x16Vertical   = 0
	I16x16Horizontal = 1
	I16x16DC         = 2
	I16x16Plane      = 3
)

// Intra4x4 / Intra8x8 modes.
const (
	I4x4Vertical       = 0
	I4x4Horizontal     = 1
	I4x4DC             = 2
	I4x4DiagDownLeft   = 3
	I4x4DiagDownRight  = 4
	I4x4VerticalRight  = 5
	I4x4HorizontalDown = 6
	I4x4VerticalLeft   = 7
	I4x4HorizontalUp   = 8
)

// Intra chroma modes.
const (
	ChromaDC         = 0
	ChromaHorizontal = 1
	ChromaVertical   = 2
	ChromaPlane      = 3
)

// predict16x16 returns the 16x16 prediction. top/left are nil when the
// neighbouring samples are unavailable. topLeft is p[-1,-1].
func predict16x16(mode int, top, left []byte, topLeft byte) [16][16]byte {
	var pred [16][16]byte
	switch mode {
	case I16x16Vertical:
		if top == nil {
			break
		}
		for y := range 16 {
			for x := range 16 {
				pred[y][x] = top[x]
			}
		}
	case I16x16Horizontal:
		if left == nil {
			break
		}
		for y := range 16 {
			for x := range 16 {
				pred[y][x] = left[y]
			}
		}
	case I16x16DC:
		sum, n := 0, 0
		for _, v := range top {
			sum += int(v)
			n++
		}
		for _, v := range left {
			sum += int(v)
			n++
		}
		var dc byte
		switch {
		case top != nil && left != nil:
			dc = byte((sum + 16) >> 5)
		case n > 0:
			dc = byte((sum + 8) >> 4)
		default:
			dc = 128
		}
		for y := range 16 {
			for x := range 16 {
				pred[y][x] = dc
			}
		}
	case I16x16Plane:
		if top == nil || left == nil {
			break
		}
		iH, iV := 0, 0
		for x := 0; x < 8; x++ {
			var pl int
			if 6-x >= 0 {
				pl = int(top[6-x])
			} else {
				pl = int(topLeft)
			}
			iH += (x + 1) * (int(top[8+x]) - pl)
		}
		for y := 0; y < 8; y++ {
			var pa int
			if 6-y >= 0 {
				pa = int(left[6-y])
			} else {
				pa = int(topLeft)
			}
			iV += (y + 1) * (int(left[8+y]) - pa)
		}
		a := 16 * (int(top[15]) + int(left[15]))
		b := (5*iH + 32) >> 6
		c := (5*iV + 32) >> 6
		for y := range 16 {
			for x := range 16 {
				pred[y][x] = clip8(int32((a + b*(x-7) + c*(y-7) + 16) >> 5))
			}
		}
	}
	return pred
}

// predict4x4 predicts a 4x4 block. ref layout (13 samples):
// [L3,L2,L1,L0, TL, T0..T7] where T4..T7 are the upper-right samples.
func predict4x4(mode int, ref [13]byte, leftAvail, topAvail bool) [4][4]byte {
	var pred [4][4]byte
	l0, l1, l2, l3 := int(ref[3]), int(ref[2]), int(ref[1]), int(ref[0])
	tl := int(ref[4])
	t0, t1, t2, t3 := int(ref[5]), int(ref[6]), int(ref[7]), int(ref[8])
	t4, t5, t6, t7 := int(ref[9]), int(ref[10]), int(ref[11]), int(ref[12])

	switch mode {
	case I4x4Vertical:
		for y := range 4 {
			pred[y][0], pred[y][1], pred[y][2], pred[y][3] = byte(t0), byte(t1), byte(t2), byte(t3)
		}
	case I4x4Horizontal:
		for x := range 4 {
			pred[0][x], pred[1][x], pred[2][x], pred[3][x] = byte(l0), byte(l1), byte(l2), byte(l3)
		}
	case I4x4DC:
		var dc byte
		switch {
		case topAvail && leftAvail:
			dc = byte((t0 + t1 + t2 + t3 + l0 + l1 + l2 + l3 + 4) >> 3)
		case topAvail:
			dc = byte((t0 + t1 + t2 + t3 + 2) >> 2)
		case leftAvail:
			dc = byte((l0 + l1 + l2 + l3 + 2) >> 2)
		default:
			dc = 128
		}
		for y := range 4 {
			for x := range 4 {
				pred[y][x] = dc
			}
		}
	case I4x4DiagDownLeft:
		pred[0][0] = byte((t0 + 2*t1 + t2 + 2) >> 2)
		pred[0][1] = byte((t1 + 2*t2 + t3 + 2) >> 2)
		pred[1][0] = pred[0][1]
		pred[0][2] = byte((t2 + 2*t3 + t4 + 2) >> 2)
		pred[1][1] = pred[0][2]
		pred[2][0] = pred[0][2]
		pred[0][3] = byte((t3 + 2*t4 + t5 + 2) >> 2)
		pred[1][2] = pred[0][3]
		pred[2][1] = pred[0][3]
		pred[3][0] = pred[0][3]
		pred[1][3] = byte((t4 + 2*t5 + t6 + 2) >> 2)
		pred[2][2] = pred[1][3]
		pred[3][1] = pred[1][3]
		pred[2][3] = byte((t5 + 2*t6 + t7 + 2) >> 2)
		pred[3][2] = pred[2][3]
		pred[3][3] = byte((t6 + 2*t7 + t7 + 2) >> 2)
	case I4x4DiagDownRight:
		pred[3][0] = byte((l3 + 2*l2 + l1 + 2) >> 2)
		pred[2][0] = byte((l2 + 2*l1 + l0 + 2) >> 2)
		pred[3][1] = pred[2][0]
		pred[1][0] = byte((l1 + 2*l0 + tl + 2) >> 2)
		pred[2][1] = pred[1][0]
		pred[3][2] = pred[1][0]
		pred[0][0] = byte((l0 + 2*tl + t0 + 2) >> 2)
		pred[1][1] = pred[0][0]
		pred[2][2] = pred[0][0]
		pred[3][3] = pred[0][0]
		pred[0][1] = byte((tl + 2*t0 + t1 + 2) >> 2)
		pred[1][2] = pred[0][1]
		pred[2][3] = pred[0][1]
		pred[0][2] = byte((t0 + 2*t1 + t2 + 2) >> 2)
		pred[1][3] = pred[0][2]
		pred[0][3] = byte((t1 + 2*t2 + t3 + 2) >> 2)
	case I4x4VerticalRight:
		pred[0][0] = byte((tl + t0 + 1) >> 1)
		pred[0][1] = byte((t0 + t1 + 1) >> 1)
		pred[0][2] = byte((t1 + t2 + 1) >> 1)
		pred[0][3] = byte((t2 + t3 + 1) >> 1)
		pred[1][0] = byte((l0 + 2*tl + t0 + 2) >> 2)
		pred[1][1] = byte((tl + 2*t0 + t1 + 2) >> 2)
		pred[1][2] = byte((t0 + 2*t1 + t2 + 2) >> 2)
		pred[1][3] = byte((t1 + 2*t2 + t3 + 2) >> 2)
		pred[2][0] = byte((tl + 2*l0 + l1 + 2) >> 2)
		pred[2][1] = pred[0][0]
		pred[2][2] = pred[0][1]
		pred[2][3] = pred[0][2]
		pred[3][0] = byte((l0 + 2*l1 + l2 + 2) >> 2)
		pred[3][1] = pred[1][0]
		pred[3][2] = pred[1][1]
		pred[3][3] = pred[1][2]
	case I4x4HorizontalDown:
		pred[0][0] = byte((tl + l0 + 1) >> 1)
		pred[0][1] = byte((l0 + 2*tl + t0 + 2) >> 2)
		pred[0][2] = byte((tl + 2*t0 + t1 + 2) >> 2)
		pred[0][3] = byte((t0 + 2*t1 + t2 + 2) >> 2)
		pred[1][0] = byte((l0 + l1 + 1) >> 1)
		pred[1][1] = byte((tl + 2*l0 + l1 + 2) >> 2)
		pred[1][2] = pred[0][0]
		pred[1][3] = pred[0][1]
		pred[2][0] = byte((l1 + l2 + 1) >> 1)
		pred[2][1] = byte((l0 + 2*l1 + l2 + 2) >> 2)
		pred[2][2] = pred[1][0]
		pred[2][3] = pred[1][1]
		pred[3][0] = byte((l2 + l3 + 1) >> 1)
		pred[3][1] = byte((l1 + 2*l2 + l3 + 2) >> 2)
		pred[3][2] = pred[2][0]
		pred[3][3] = pred[2][1]
	case I4x4VerticalLeft:
		pred[0][0] = byte((t0 + t1 + 1) >> 1)
		pred[0][1] = byte((t1 + t2 + 1) >> 1)
		pred[0][2] = byte((t2 + t3 + 1) >> 1)
		pred[0][3] = byte((t3 + t4 + 1) >> 1)
		pred[1][0] = byte((t0 + 2*t1 + t2 + 2) >> 2)
		pred[1][1] = byte((t1 + 2*t2 + t3 + 2) >> 2)
		pred[1][2] = byte((t2 + 2*t3 + t4 + 2) >> 2)
		pred[1][3] = byte((t3 + 2*t4 + t5 + 2) >> 2)
		pred[2][0] = byte((t1 + t2 + 1) >> 1)
		pred[2][1] = byte((t2 + t3 + 1) >> 1)
		pred[2][2] = byte((t3 + t4 + 1) >> 1)
		pred[2][3] = byte((t4 + t5 + 1) >> 1)
		pred[3][0] = byte((t1 + 2*t2 + t3 + 2) >> 2)
		pred[3][1] = byte((t2 + 2*t3 + t4 + 2) >> 2)
		pred[3][2] = byte((t3 + 2*t4 + t5 + 2) >> 2)
		pred[3][3] = byte((t4 + 2*t5 + t6 + 2) >> 2)
	case I4x4HorizontalUp:
		pred[0][0] = byte((l0 + l1 + 1) >> 1)
		pred[0][1] = byte((l0 + 2*l1 + l2 + 2) >> 2)
		pred[0][2] = byte((l1 + l2 + 1) >> 1)
		pred[0][3] = byte((l1 + 2*l2 + l3 + 2) >> 2)
		pred[1][0] = byte((l1 + l2 + 1) >> 1)
		pred[1][1] = byte((l1 + 2*l2 + l3 + 2) >> 2)
		pred[1][2] = byte((l2 + l3 + 1) >> 1)
		pred[1][3] = byte((l2 + 2*l3 + l3 + 2) >> 2)
		pred[2][0] = byte((l2 + l3 + 1) >> 1)
		pred[2][1] = byte((l2 + 2*l3 + l3 + 2) >> 2)
		pred[2][2], pred[2][3] = byte(l3), byte(l3)
		pred[3][0], pred[3][1], pred[3][2], pred[3][3] = byte(l3), byte(l3), byte(l3), byte(l3)
	}
	return pred
}

// predict8x8 predicts an 8x8 block. ref layout (25 samples):
// [L7..L0, TL, T0..T7, T8..T15] (already filtered by the caller).
func predict8x8(mode int, ref [25]byte, leftAvail, topAvail bool) [8][8]byte {
	var pred [8][8]byte
	var l [8]int
	for i := range 8 {
		l[i] = int(ref[7-i])
	}
	tl := int(ref[8])
	var t [16]int
	for i := range 16 {
		t[i] = int(ref[9+i])
	}

	switch mode {
	case 0:
		for y := range 8 {
			for x := range 8 {
				pred[y][x] = byte(t[x])
			}
		}
	case 1:
		for y := range 8 {
			for x := range 8 {
				pred[y][x] = byte(l[y])
			}
		}
	case 2:
		var dc byte
		switch {
		case topAvail && leftAvail:
			sum := 0
			for i := range 8 {
				sum += t[i] + l[i]
			}
			dc = byte((sum + 8) >> 4)
		case topAvail:
			sum := 0
			for i := range 8 {
				sum += t[i]
			}
			dc = byte((sum + 4) >> 3)
		case leftAvail:
			sum := 0
			for i := range 8 {
				sum += l[i]
			}
			dc = byte((sum + 4) >> 3)
		default:
			dc = 128
		}
		for y := range 8 {
			for x := range 8 {
				pred[y][x] = dc
			}
		}
	case 3:
		for y := range 8 {
			for x := range 8 {
				if x+y == 14 {
					pred[y][x] = byte((t[14] + 3*t[15] + 2) >> 2)
				} else {
					pred[y][x] = byte((t[x+y] + 2*t[x+y+1] + t[x+y+2] + 2) >> 2)
				}
			}
		}
	case 4:
		for y := range 8 {
			for x := range 8 {
				d := x - y
				p0 := diagSample(d-1, l[:], tl, t[:])
				p1 := diagSample(d, l[:], tl, t[:])
				p2 := diagSample(d+1, l[:], tl, t[:])
				pred[y][x] = byte((p0 + 2*p1 + p2 + 2) >> 2)
			}
		}
	case 5:
		predict8x8VerticalRight(&pred, l, tl, t)
	case 6:
		predict8x8HorizontalDown(&pred, l, tl, t)
	case 7:
		for y := range 8 {
			for x := range 8 {
				if y%2 == 0 {
					pred[y][x] = byte((t[x+y/2] + t[x+y/2+1] + 1) >> 1)
				} else {
					pred[y][x] = byte((t[x+y/2] + 2*t[x+y/2+1] + t[x+y/2+2] + 2) >> 2)
				}
			}
		}
	case 8:
		for y := range 8 {
			for x := range 8 {
				z := x + 2*y
				switch {
				case z < 13:
					i := y + (x >> 1)
					if z%2 == 0 {
						pred[y][x] = byte((l[i] + l[i+1] + 1) >> 1)
					} else {
						pred[y][x] = byte((l[i] + 2*l[i+1] + l[i+2] + 2) >> 2)
					}
				case z == 13:
					pred[y][x] = byte((l[6] + 3*l[7] + 2) >> 2)
				default:
					pred[y][x] = byte(l[7])
				}
			}
		}
	}
	return pred
}

func diagSample(d int, l []int, tl int, t []int) int {
	if d == 0 {
		return tl
	}
	if d > 0 {
		return t[d-1]
	}
	return l[-d-1]
}

func predict8x8VerticalRight(pred *[8][8]byte, l [8]int, tl int, t [16]int) {
	for y := range 8 {
		for x := range 8 {
			z := 2*x - y
			switch {
			case z >= 0 && z%2 == 0:
				i := x - (y >> 1)
				if i == 0 {
					pred[y][x] = byte((tl + t[0] + 1) >> 1)
				} else {
					pred[y][x] = byte((t[i-1] + t[i] + 1) >> 1)
				}
			case z >= 0 && z%2 != 0:
				i := x - (y >> 1) - 1
				switch i {
				case -1:
					pred[y][x] = byte((l[0] + 2*tl + t[0] + 2) >> 2)
				case 0:
					pred[y][x] = byte((tl + 2*t[0] + t[1] + 2) >> 2)
				default:
					pred[y][x] = byte((t[i-1] + 2*t[i] + t[i+1] + 2) >> 2)
				}
			case z == -1:
				pred[y][x] = byte((l[0] + 2*tl + t[0] + 2) >> 2)
			default:
				i := y - 2*x - 2
				if i == 0 {
					pred[y][x] = byte((tl + 2*l[0] + l[1] + 2) >> 2)
				} else {
					pred[y][x] = byte((l[i-1] + 2*l[i] + l[i+1] + 2) >> 2)
				}
			}
		}
	}
}

func predict8x8HorizontalDown(pred *[8][8]byte, l [8]int, tl int, t [16]int) {
	for y := range 8 {
		for x := range 8 {
			z := 2*y - x
			switch {
			case z >= 0 && z%2 == 0:
				i := y - (x >> 1)
				if i == 0 {
					pred[y][x] = byte((tl + l[0] + 1) >> 1)
				} else {
					pred[y][x] = byte((l[i-1] + l[i] + 1) >> 1)
				}
			case z >= 0 && z%2 != 0:
				i := y - (x >> 1) - 1
				switch i {
				case -1:
					pred[y][x] = byte((t[0] + 2*tl + l[0] + 2) >> 2)
				case 0:
					pred[y][x] = byte((tl + 2*l[0] + l[1] + 2) >> 2)
				default:
					pred[y][x] = byte((l[i-1] + 2*l[i] + l[i+1] + 2) >> 2)
				}
			case z == -1:
				pred[y][x] = byte((t[0] + 2*tl + l[0] + 2) >> 2)
			default:
				i := x - 2*y - 2
				if i == 0 {
					pred[y][x] = byte((tl + 2*t[0] + t[1] + 2) >> 2)
				} else {
					pred[y][x] = byte((t[i-1] + 2*t[i] + t[i+1] + 2) >> 2)
				}
			}
		}
	}
}

// predictChroma predicts an 8x8 chroma block. top/left may be nil.
func predictChroma(mode int, top, left []byte, topLeft byte) [8][8]byte {
	var pred [8][8]byte
	switch mode {
	case ChromaDC:
		predictChromaDC8x8(&pred, top, left)
	case ChromaHorizontal:
		if left != nil {
			for y := range 8 {
				for x := range 8 {
					pred[y][x] = left[y]
				}
			}
		}
	case ChromaVertical:
		if top != nil {
			for y := range 8 {
				for x := range 8 {
					pred[y][x] = top[x]
				}
			}
		}
	case ChromaPlane:
		if top == nil || left == nil {
			break
		}
		iH, iV := 0, 0
		for x := range 4 {
			var ref int
			if 4-2-x >= 0 {
				ref = int(top[4-2-x])
			} else {
				ref = int(topLeft)
			}
			iH += (x + 1) * (int(top[4+x]) - ref)
		}
		for y := range 4 {
			var ref int
			if 4-2-y >= 0 {
				ref = int(left[4-2-y])
			} else {
				ref = int(topLeft)
			}
			iV += (y + 1) * (int(left[4+y]) - ref)
		}
		a := 16 * (int(top[7]) + int(left[7]))
		b := (34*iH + 32) >> 6
		c := (34*iV + 32) >> 6
		for y := range 8 {
			for x := range 8 {
				pred[y][x] = clip8(int32((a + b*(x-3) + c*(y-3) + 16) >> 5))
			}
		}
	}
	return pred
}

// predictChromaDC8x8 applies per-4x4-subblock DC prediction (spec 8.3.4.1).
func predictChromaDC8x8(pred *[8][8]byte, top, left []byte) {
	hasTop, hasLeft := top != nil, left != nil
	for blk := 0; blk < 4; blk++ {
		x0 := (blk % 2) * 4
		y0 := (blk / 2) * 4
		topRow := blk < 2
		leftCol := blk%2 == 0
		useTop, useLeft := false, false
		switch {
		case topRow && leftCol:
			useTop, useLeft = hasTop, hasLeft
		case topRow && !leftCol:
			if hasTop {
				useTop = true
			} else if hasLeft {
				useLeft = true
			}
		case !topRow && leftCol:
			if hasLeft {
				useLeft = true
			} else if hasTop {
				useTop = true
			}
		default:
			useTop, useLeft = hasTop, hasLeft
		}
		sum, n := 0, 0
		if useTop {
			for x := x0; x < x0+4; x++ {
				sum += int(top[x])
				n++
			}
		}
		if useLeft {
			for y := y0; y < y0+4; y++ {
				sum += int(left[y])
				n++
			}
		}
		dc := byte(128)
		if n > 0 {
			dc = byte((sum + n/2) / n)
		}
		for y := y0; y < y0+4; y++ {
			for x := x0; x < x0+4; x++ {
				pred[y][x] = dc
			}
		}
	}
}
