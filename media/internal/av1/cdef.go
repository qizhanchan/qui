package av1

// Constrained directional enhancement filter (spec 7.15).

var (
	cdefDirections = [8][2][2]int{
		{{-1, 1}, {-2, 2}}, {{0, 1}, {-1, 2}}, {{0, 1}, {0, 2}}, {{0, 1}, {1, 2}},
		{{1, 1}, {2, 2}}, {{1, 0}, {2, 1}}, {{1, 0}, {2, 0}}, {{1, 0}, {2, -1}},
	}
	cdefPriTaps = [2][2]int32{{4, 2}, {3, 3}}
	cdefSecTaps = [2][2]int32{{2, 1}, {2, 1}}
	cdefDiv     = [9]int32{0, 840, 420, 280, 210, 168, 140, 120, 105}
)

// cdef filters src (the deblocked frame) into dst (initially a copy).
func (d *Decoder) cdef(f *frameState, src, dst *frame) {
	h := f.hdr
	parallelFor((f.miRows+15)/16, func(_, fbRow int) {
		r := fbRow * 16
		for c := 0; c < f.miCols; c += 16 {
			idx := int(f.cdefIdx[f.cdefIndex(r, c)])
			if idx == -1 {
				continue
			}
			for y := r; y < min(r+16, f.miRows); y += 2 {
				for x := c; x < min(c+16, f.miCols); x += 2 {
					d.cdefBlock(f, src, dst, y, x, idx, h)
				}
			}
		}
	})
}

func (d *Decoder) cdefBlock(f *frameState, src, dst *frame, r, c, idx int, h *FrameHeader) {
	m := func(rr, cc int) bool {
		if rr >= f.miRows || cc >= f.miCols {
			return true
		}
		return f.mi[rr*f.miCols+cc].skip
	}
	if m(r, c) && m(r+1, c) && m(r, c+1) && m(r+1, c+1) {
		return
	}
	bd := src.bitDepth
	cs := uint(bd - 8)
	yDir, variance := cdefDirection(&src.planes[0], c*4, r*4, bd)
	priStr := int32(h.cdefYPri[idx]) << cs
	secStr := int32(h.cdefYSec[idx]) << cs
	dir := yDir
	if priStr == 0 {
		dir = 0
	}
	varStr := int32(0)
	if variance>>6 != 0 {
		varStr = min(int32(floorLog2(int(variance>>6))), 12)
	}
	if variance != 0 {
		priStr = (priStr*(4+varStr) + 8) >> 4
	} else {
		priStr = 0
	}
	damping := h.cdefDamping + int(cs)
	cdefFilter(f, src, dst, 0, r, c, priStr, secStr, damping, dir)
	if src.numPlanes > 1 {
		priStr = int32(h.cdefUVPri[idx]) << cs
		secStr = int32(h.cdefUVSec[idx]) << cs
		dir = yDir
		if priStr == 0 {
			dir = 0
		}
		damping = h.cdefDamping + int(cs) - 1
		cdefFilter(f, src, dst, 1, r, c, priStr, secStr, damping, dir)
		cdefFilter(f, src, dst, 2, r, c, priStr, secStr, damping, dir)
	}
}

func floorLog2(x int) int {
	n := -1
	for x > 0 {
		x >>= 1
		n++
	}
	return n
}

func cdefDirection(pb *planeBuf, x0, y0, bd int) (int, int32) {
	var cost [8]int32
	var partial [8][15]int32
	var px [8]int32
	for i := 0; i < 8; i++ {
		o := (y0+i)*pb.stride + x0
		if pb.pix16 != nil {
			for j, v := range pb.pix16[o : o+8] {
				px[j] = int32(v)>>(bd-8) - 128
			}
		} else {
			for j, v := range pb.pix8[o : o+8] {
				px[j] = int32(v) - 128
			}
		}
		for j, x := range px {
			partial[0][i+j] += x
			partial[1][i+j/2] += x
			partial[2][i] += x
			partial[3][3+i-j/2] += x
			partial[4][7+i-j] += x
			partial[5][3-i/2+j] += x
			partial[6][j] += x
			partial[7][i/2+j] += x
		}
	}
	for i := 0; i < 8; i++ {
		cost[2] += partial[2][i] * partial[2][i]
		cost[6] += partial[6][i] * partial[6][i]
	}
	cost[2] *= cdefDiv[8]
	cost[6] *= cdefDiv[8]
	for i := 0; i < 7; i++ {
		cost[0] += (partial[0][i]*partial[0][i] + partial[0][14-i]*partial[0][14-i]) * cdefDiv[i+1]
		cost[4] += (partial[4][i]*partial[4][i] + partial[4][14-i]*partial[4][14-i]) * cdefDiv[i+1]
	}
	cost[0] += partial[0][7] * partial[0][7] * cdefDiv[8]
	cost[4] += partial[4][7] * partial[4][7] * cdefDiv[8]
	for i := 1; i < 8; i += 2 {
		for j := 0; j < 5; j++ {
			cost[i] += partial[i][3+j] * partial[i][3+j]
		}
		cost[i] *= cdefDiv[8]
		for j := 0; j < 3; j++ {
			cost[i] += (partial[i][j]*partial[i][j] + partial[i][10-j]*partial[i][10-j]) * cdefDiv[2*j+2]
		}
	}
	best, dir := int32(0), 0
	for i := 0; i < 8; i++ {
		if cost[i] > best {
			best, dir = cost[i], i
		}
	}
	return dir, (best - cost[(dir+4)&7]) >> 10
}

func constrain(diff, threshold int32, damping int) int32 {
	if threshold == 0 {
		return 0
	}
	return constrainAdj(diff, threshold, uint(max(0, damping-floorLog2(int(threshold)))))
}

func constrainAdj(diff, threshold int32, adj uint) int32 {
	a := diff
	if a < 0 {
		a = -a
	}
	v := min(a, max(0, threshold-a>>adj))
	if diff < 0 {
		return -v
	}
	return v
}

// cdefNA marks a tap outside the frame (CdefAvailable false).
const cdefNA = int32(1) << 24

func cdefFilter(f *frameState, src, dst *frame, plane, r, c int, priStr, secStr int32, damping, dir int) {
	if priStr == 0 && secStr == 0 {
		// The sum is zero and the sample is its own clamp.
		return
	}
	ss := min(plane, 1)
	x0, y0 := (c*4)>>ss, (r*4)>>ss
	w, h := 8>>ss, 8>>ss
	sp, dp := &src.planes[plane], &dst.planes[plane]
	maxX, maxY := (f.miCols*4)>>ss, (f.miRows*4)>>ss
	cs := uint(src.bitDepth - 8)
	pt := &cdefPriTaps[(priStr>>cs)&1]
	st := &cdefSecTaps[(priStr>>cs)&1]
	// The block with a two-sample border, stride 12.
	const bs = 12
	var blk [bs * bs]int32
	for i := -2; i < h+2; i++ {
		yy := y0 + i
		row := blk[(i+2)*bs : (i+2)*bs+w+4]
		if yy < 0 || yy >= maxY {
			for j := range row {
				row[j] = cdefNA
			}
			continue
		}
		o := yy * sp.stride
		for j := range row {
			xx := x0 + j - 2
			switch {
			case xx < 0 || xx >= maxX:
				row[j] = cdefNA
			case sp.pix16 != nil:
				row[j] = int32(sp.pix16[o+xx])
			default:
				row[j] = int32(sp.pix8[o+xx])
			}
		}
	}
	var priOff [2][2]int
	for k := 0; k < 2; k++ {
		d := &cdefDirections[dir][k]
		priOff[k] = [2]int{d[0]*bs + d[1], -(d[0]*bs + d[1])}
	}
	var secDirs = [2]int{(dir + 2) & 7, (dir - 2) & 7}
	var secOffs [2][4]int
	for k := 0; k < 2; k++ {
		n := 0
		for _, d2 := range secDirs {
			d := &cdefDirections[d2][k]
			o := d[0]*bs + d[1]
			secOffs[k][n], secOffs[k][n+1] = o, -o
			n += 2
		}
	}
	var priAdj, secAdj uint
	if priStr != 0 {
		priAdj = uint(max(0, damping-floorLog2(int(priStr))))
	}
	if secStr != 0 {
		secAdj = uint(max(0, damping-floorLog2(int(secStr))))
	}
	for i := 0; i < h; i++ {
		for j := 0; j < w; j++ {
			ci := (i+2)*bs + j + 2
			x := blk[ci]
			sum := int32(0)
			mx, mn := x, x
			for k := 0; k < 2; k++ {
				for _, o := range priOff[k] {
					p := blk[ci+o]
					if p == cdefNA {
						continue
					}
					if priStr != 0 {
						sum += pt[k] * constrainAdj(p-x, priStr, priAdj)
					}
					mx, mn = max(p, mx), min(p, mn)
				}
				for _, o := range secOffs[k] {
					p := blk[ci+o]
					if p == cdefNA {
						continue
					}
					if secStr != 0 {
						sum += st[k] * constrainAdj(p-x, secStr, secAdj)
					}
					mx, mn = max(p, mx), min(p, mn)
				}
			}
			neg := int32(0)
			if sum < 0 {
				neg = 1
			}
			v := min(max(x+((8+sum-neg)>>4), mn), mx)
			o := (y0+i)*dp.stride + x0 + j
			if dp.pix16 != nil {
				dp.pix16[o] = uint16(v)
			} else {
				dp.pix8[o] = uint8(v)
			}
		}
	}
}
