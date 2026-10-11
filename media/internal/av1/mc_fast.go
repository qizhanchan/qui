package av1

// Unscaled block inter prediction: the same arithmetic as the generic
// blockInterPred, specialised for xStep = yStep = 1 << 10 (every output
// sample uses the same filter phase). Blocks whose 8-tap footprint lies
// inside the reference read it in place; others first gather a clamped
// copy. An axis at an integer position reduces to a shift, since its only
// non-zero tap is 128.

const mcEdgeStride = 128 + 7

// mcEdge holds the clamped footprint of a block that reaches past the
// reference edges.
// The extra row lets the NEON kernels read 16 bytes past a row start.
type mcEdge struct {
	pix8    [mcEdgeStride * (128 + 8)]uint8
	pix16   [mcEdgeStride * (128 + 8)]uint16
	inter16 [(128+7)*128 + 8]int16
}

// subpelHalf is subpelFilters with every tap halved (all are even), so
// the 8-bit horizontal pass fits int16: (sum + 4) >> 3 of the full taps
// equals (sum + 2) >> 2 of the halved ones. Filled with subpelFilters.
var subpelHalf [6][16][8]int16

func (t *tile) interPredUnscaled(pb *planeBuf, lastX, lastY, x, y, w, h int, fx, fy *[16][8]int32, fi [2]int, r0, r1 uint, pred []int32) {
	fracX, fracY := (x>>6)&15, (y>>6)&15
	x0, y0 := (x>>10)-3, (y>>10)-3
	inside := x0 >= 0 && y0 >= 0 && x0+w+7 <= lastX+1 && y0+h+7 <= lastY+1
	if pb.pix16 != nil {
		src, stride := pb.pix16, pb.stride
		off := y0*stride + x0
		if !inside {
			src, stride, off = gatherEdge(t.mc.edge.pix16[:], pb.pix16, pb.stride, lastX, lastY, x0, y0, w, h), mcEdgeStride, 0
		}
		mcKernel(src[off:], stride, w, h, fracX, fracY, &fx[fracX], &fy[fracY], r0, r1, t.mc.inter[:], pred)
		return
	}
	src, stride := pb.pix8, pb.stride
	off := y0*stride + x0
	// The NEON kernels read up to 16 bytes from the last group's start.
	if !inside || off+(h+6)*stride+w+16 > len(src) {
		src, stride, off = gatherEdge(t.mc.edge.pix8[:], pb.pix8, pb.stride, lastX, lastY, x0, y0, w, h), mcEdgeStride, 0
	}
	if haveNEON && w&3 == 0 {
		t.mcNEON(src[off:], stride, w, h, fracX, fracY, &subpelHalf[fi[0]][fracX], &subpelHalf[fi[1]][fracY], r1, pred)
		return
	}
	mcKernel(src[off:], stride, w, h, fracX, fracY, &fx[fracX], &fy[fracY], r0, r1, t.mc.inter[:], pred)
}

// gatherEdge copies the (w+7) x (h+7) footprint at (x0, y0) into dst with
// coordinates clamped to [0, lastX] x [0, lastY].
func gatherEdge[T uint8 | uint16](dst, src []T, stride, lastX, lastY, x0, y0, w, h int) []T {
	for r := 0; r < h+7; r++ {
		row := src[clip3(0, lastY, y0+r)*stride:]
		d := dst[r*mcEdgeStride : r*mcEdgeStride+w+7]
		for c := range d {
			d[c] = row[clip3(0, lastX, x0+c)]
		}
	}
	return dst
}

// mcKernel filters the footprint whose top-left (3 rows above and 3
// columns left of the block) is src[0].
func mcKernel[T uint8 | uint16](src []T, stride, w, h, fracX, fracY int, kx, ky *[8]int32, r0, r1 uint, inter, pred []int32) {
	rnd0 := int32(1) << (r0 - 1)
	rnd1 := int32(1) << (r1 - 1)
	// Rows of the horizontal pass that the vertical pass reads.
	r0Row, nRows := 0, h+7
	if fracY == 0 {
		r0Row, nRows = 3, h
	}
	inter = inter[:nRows*w]
	if fracX == 0 {
		sh := 7 - r0
		for r := 0; r < nRows; r++ {
			s := src[(r0Row+r)*stride+3:]
			s = s[:w]
			d := inter[r*w : r*w+w]
			for c, v := range s {
				d[c] = int32(v) << sh
			}
		}
	} else {
		k0, k1, k2, k3, k4, k5, k6, k7 := kx[0], kx[1], kx[2], kx[3], kx[4], kx[5], kx[6], kx[7]
		for r := 0; r < nRows; r++ {
			s := src[(r0Row+r)*stride:]
			s = s[:w+7]
			d := inter[r*w : r*w+w]
			for c := range d {
				p := s[c : c+8 : c+8]
				v := k0*int32(p[0]) + k1*int32(p[1]) + k2*int32(p[2]) + k3*int32(p[3]) +
					k4*int32(p[4]) + k5*int32(p[5]) + k6*int32(p[6]) + k7*int32(p[7])
				d[c] = (v + rnd0) >> r0
			}
		}
	}
	pred = pred[:w*h]
	if fracY == 0 {
		for i, v := range inter {
			pred[i] = (v<<7 + rnd1) >> r1
		}
		return
	}
	k0, k1, k2, k3, k4, k5, k6, k7 := ky[0], ky[1], ky[2], ky[3], ky[4], ky[5], ky[6], ky[7]
	for r := 0; r < h; r++ {
		a := inter[r*w:]
		a = a[:8*w]
		d := pred[r*w : r*w+w]
		for c := range d {
			v := k0*a[c] + k1*a[w+c] + k2*a[2*w+c] + k3*a[3*w+c] +
				k4*a[4*w+c] + k5*a[5*w+c] + k6*a[6*w+c] + k7*a[7*w+c]
			d[c] = (v + rnd1) >> r1
		}
	}
}

// mcNEON is mcKernel for 8-bit samples (r0 = 3) and w a multiple of 4.
func (t *tile) mcNEON(src []uint8, stride, w, h, fracX, fracY int, kx, ky *[8]int16, r1 uint, pred []int32) {
	r0Row, nRows := 0, h+7
	if fracY == 0 {
		r0Row, nRows = 3, h
	}
	inter := t.mc.edge.inter16[:]
	if fracX == 0 {
		for r := 0; r < nRows; r++ {
			s := src[(r0Row+r)*stride+3:]
			s = s[:w]
			d := inter[r*w : r*w+w]
			for c, v := range s {
				d[c] = int16(v) << 4
			}
		}
	} else {
		_ = src[r0Row*stride+(nRows-1)*stride+w+15]
		neonMcH(&inter[0], &src[r0Row*stride], stride, w, nRows, &kx[0])
	}
	pred = pred[:w*h]
	if fracY == 0 {
		rnd1 := int32(1) << (r1 - 1)
		for i, v := range inter[:w*h] {
			pred[i] = (int32(v)<<7 + rnd1) >> r1
		}
		return
	}
	neonMcV(&pred[0], &inter[0], w, h, &ky[0], int(r1-1))
}
