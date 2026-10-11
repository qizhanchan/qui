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
	for r := 0; r < f.miRows; r += 16 {
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
	}
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
	for i := 0; i < 8; i++ {
		for j := 0; j < 8; j++ {
			x := pb.get(x0+j, y0+i)>>(bd-8) - 128
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
	adj := max(0, damping-floorLog2(int(threshold)))
	a := iabs32(diff)
	v := min(a, max(0, threshold-a>>uint(adj)))
	if diff < 0 {
		return -v
	}
	return v
}

func cdefFilter(f *frameState, src, dst *frame, plane, r, c int, priStr, secStr int32, damping, dir int) {
	ss := min(plane, 1)
	x0, y0 := (c*4)>>ss, (r*4)>>ss
	w, h := 8>>ss, 8>>ss
	sp, dp := &src.planes[plane], &dst.planes[plane]
	maxX, maxY := (f.miCols*4)>>ss, (f.miRows*4)>>ss
	cs := uint(src.bitDepth - 8)
	pt := &cdefPriTaps[(priStr>>cs)&1]
	st := &cdefSecTaps[(priStr>>cs)&1]
	for i := 0; i < h; i++ {
		for j := 0; j < w; j++ {
			x := sp.get(x0+j, y0+i)
			sum := int32(0)
			mx, mn := x, x
			for k := 0; k < 2; k++ {
				for sign := -1; sign <= 1; sign += 2 {
					yy := y0 + i + sign*cdefDirections[dir][k][0]
					xx := x0 + j + sign*cdefDirections[dir][k][1]
					if yy >= 0 && yy < maxY && xx >= 0 && xx < maxX {
						p := sp.get(xx, yy)
						sum += pt[k] * constrain(p-x, priStr, damping)
						mx, mn = max(p, mx), min(p, mn)
					}
					for off := -2; off <= 2; off += 4 {
						d2 := (dir + off) & 7
						yy := y0 + i + sign*cdefDirections[d2][k][0]
						xx := x0 + j + sign*cdefDirections[d2][k][1]
						if yy >= 0 && yy < maxY && xx >= 0 && xx < maxX {
							s0 := sp.get(xx, yy)
							sum += st[k] * constrain(s0-x, secStr, damping)
							mx, mn = max(s0, mx), min(s0, mn)
						}
					}
				}
			}
			neg := int32(0)
			if sum < 0 {
				neg = 1
			}
			dp.set(x0+j, y0+i, min(max(x+((8+sum-neg)>>4), mn), mx))
		}
	}
}
