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

// cdefBand filters the 64x64 blocks of luma rows [64k, 64k+64), in
// parallel by column.
func (d *Decoder) cdefBand(f *frameState, src, dst *frame, k int) {
	h := f.hdr
	r := 16 * k
	parallelFor((f.miCols+15)/16, func(_, fc int) {
		c := 16 * fc
		idx := int(f.cdefIdx[f.cdefIndex(r, c)])
		if idx == -1 {
			return
		}
		for y := r; y < min(r+16, f.miRows); y += 2 {
			for x := c; x < min(c+16, f.miCols); x += 2 {
				d.cdefBlock(f, src, dst, y, x, idx, h)
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

// cdefNA marks a tap outside the frame (CdefAvailable false). As in
// dav1d, its value makes the tap drop out with no test: it is negative,
// so the signed max ignores it; as uint32 it is huge, so the unsigned min
// ignores it; and its distance from any sample exceeds every
// threshold << adjustment, so constrain gives 0.
const cdefNA = -int32(1) << 20

// cdefBS is the stride of cdefFilter's block copy: 8 samples and a
// two-sample border on each side.
const cdefBS = 12

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
	var blk [cdefBS * cdefBS]int32
	cdefLoad(&blk, sp, x0, y0, w, h, maxX, maxY)

	k0 := &cdefDirections[dir]
	po0 := k0[0][0]*cdefBS + k0[0][1]
	po1 := k0[1][0]*cdefBS + k0[1][1]
	sa, sb := &cdefDirections[(dir+2)&7], &cdefDirections[(dir-2)&7]
	so0, so1 := sa[0][0]*cdefBS+sa[0][1], sb[0][0]*cdefBS+sb[0][1]
	so2, so3 := sa[1][0]*cdefBS+sa[1][1], sb[1][0]*cdefBS+sb[1][1]
	pt := &cdefPriTaps[(priStr>>cs)&1]
	pt0, pt1 := pt[0], pt[1]
	var priAdj, secAdj uint
	if priStr != 0 {
		priAdj = uint(max(0, damping-floorLog2(int(priStr))))
	}
	if secStr != 0 {
		secAdj = uint(max(0, damping-floorLog2(int(secStr))))
	}
	for i := 0; i < h; i++ {
		o := (y0+i)*dp.stride + x0
		for j := 0; j < w; j++ {
			ci := (i+2)*cdefBS + j + 2
			x := blk[ci]
			var sum int32
			var v int32
			switch {
			case secStr == 0:
				// Primary taps only (12/16 of the largest step): the
				// result stays within the taps' range, so no clamp.
				sum = pt0*(constrainAdj(blk[ci+po0]-x, priStr, priAdj)+constrainAdj(blk[ci-po0]-x, priStr, priAdj)) +
					pt1*(constrainAdj(blk[ci+po1]-x, priStr, priAdj)+constrainAdj(blk[ci-po1]-x, priStr, priAdj))
				v = x + (8+sum-(sum>>31&1))>>4
			case priStr == 0:
				// Secondary taps only (likewise 12/16): no clamp.
				sum = 2*(constrainAdj(blk[ci+so0]-x, secStr, secAdj)+constrainAdj(blk[ci-so0]-x, secStr, secAdj)+
					constrainAdj(blk[ci+so1]-x, secStr, secAdj)+constrainAdj(blk[ci-so1]-x, secStr, secAdj)) +
					constrainAdj(blk[ci+so2]-x, secStr, secAdj) + constrainAdj(blk[ci-so2]-x, secStr, secAdj) +
					constrainAdj(blk[ci+so3]-x, secStr, secAdj) + constrainAdj(blk[ci-so3]-x, secStr, secAdj)
				v = x + (8+sum-(sum>>31&1))>>4
			default:
				p0, p1, p2, p3 := blk[ci+po0], blk[ci-po0], blk[ci+po1], blk[ci-po1]
				s0, s1, s2, s3 := blk[ci+so0], blk[ci-so0], blk[ci+so1], blk[ci-so1]
				s4, s5, s6, s7 := blk[ci+so2], blk[ci-so2], blk[ci+so3], blk[ci-so3]
				sum = pt0*(constrainAdj(p0-x, priStr, priAdj)+constrainAdj(p1-x, priStr, priAdj)) +
					pt1*(constrainAdj(p2-x, priStr, priAdj)+constrainAdj(p3-x, priStr, priAdj)) +
					2*(constrainAdj(s0-x, secStr, secAdj)+constrainAdj(s1-x, secStr, secAdj)+
						constrainAdj(s2-x, secStr, secAdj)+constrainAdj(s3-x, secStr, secAdj)) +
					constrainAdj(s4-x, secStr, secAdj) + constrainAdj(s5-x, secStr, secAdj) +
					constrainAdj(s6-x, secStr, secAdj) + constrainAdj(s7-x, secStr, secAdj)
				mx := max(x, p0, p1, p2, p3, s0, s1, s2, s3, s4, s5, s6, s7)
				mn := int32(min(uint32(x), uint32(p0), uint32(p1), uint32(p2), uint32(p3),
					uint32(s0), uint32(s1), uint32(s2), uint32(s3), uint32(s4), uint32(s5), uint32(s6), uint32(s7)))
				v = min(max(x+(8+sum-(sum>>31&1))>>4, mn), mx)
			}
			if dp.pix16 != nil {
				dp.pix16[o+j] = uint16(v)
			} else {
				dp.pix8[o+j] = uint8(v)
			}
		}
	}
}

// cdefLoad copies the w x h block at (x0, y0) of sp with a two-sample
// border into blk, marking samples outside the frame cdefNA.
func cdefLoad(blk *[cdefBS * cdefBS]int32, sp *planeBuf, x0, y0, w, h, maxX, maxY int) {
	lo, hi := max(0, x0-2), min(maxX, x0+w+2) // in-frame columns
	for i := -2; i < h+2; i++ {
		yy := y0 + i
		row := blk[(i+2)*cdefBS : (i+2)*cdefBS+w+4]
		if yy < 0 || yy >= maxY {
			for j := range row {
				row[j] = cdefNA
			}
			continue
		}
		for j := 0; j < lo-(x0-2); j++ {
			row[j] = cdefNA
		}
		for j := hi - (x0 - 2); j < len(row); j++ {
			row[j] = cdefNA
		}
		in := row[lo-(x0-2) : hi-(x0-2)]
		o := yy*sp.stride + lo
		if sp.pix16 != nil {
			for j, v := range sp.pix16[o : o+len(in)] {
				in[j] = int32(v)
			}
		} else {
			for j, v := range sp.pix8[o : o+len(in)] {
				in[j] = int32(v)
			}
		}
	}
}
