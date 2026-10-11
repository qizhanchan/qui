package h264enc

import "github.com/qizhanchan/qui/media/internal/h264"

// High profile: Intra8x8 and the 8x8 inter transform.

// i8Ref gathers and filters the Intra8x8 references of 8x8 block b8.
func (s *sliceEnc) i8Ref(b8 int) (ref [25]byte, left, top bool) {
	bx, by := (b8&1)*8, (b8>>1)*8
	left = bx > 0 || s.availA
	top = by > 0 || s.availB
	var tr, tl bool
	switch b8 {
	case 0:
		tr, tl = s.availB, s.availD
	case 1:
		tr, tl = s.availC, s.availB
	case 2:
		tr, tl = true, s.availA
	case 3:
		tr, tl = false, true
	}
	Y, ys := s.cur.y, s.cur.ys
	o := s.yOff(bx, by)
	var p [25]int
	if left {
		for i := 0; i < 8; i++ {
			p[i] = int(Y[o+(7-i)*ys-1])
		}
	}
	if tl {
		p[8] = int(Y[o-ys-1])
	}
	if top {
		for i := 0; i < 8; i++ {
			p[9+i] = int(Y[o-ys+i])
		}
		for i := 0; i < 8; i++ {
			if tr {
				p[17+i] = int(Y[o-ys+8+i])
			} else {
				p[17+i] = p[16]
			}
		}
	}
	return h264.FilterRef8x8(&p, left, top, tl), left, top
}

// analyseI8 chooses Intra8x8 modes, coding and reconstructing each block.
func (s *sliceEnc) analyseI8() int {
	Y, ys := s.plane()
	src, ss := s.srcY()
	s.t8x8 = true
	s.cbpL = 0
	cost := 0
	var pbuf [64]byte
	for b8 := 0; b8 < 4; b8++ {
		bx, by := (b8&1)*8, (b8>>1)*8
		ref, left, top := s.i8Ref(b8)
		tl := (bx > 0 && by > 0) || (by > 0 && s.availA) || (bx > 0 && by == 0 && s.availB) || (bx == 0 && by == 0 && s.availD)
		pm := s.predIntraMode(bx/4, by/4)
		sb := src[by*ss+bx:]
		best, bestCost := 2, 1<<30
		var bestPred [8][8]byte
		for mode := 0; mode < 9; mode++ {
			if !i4ModeOK(mode, left, top, tl) {
				continue
			}
			p := h264.Predict8x8(mode, ref, left, top)
			for y := 0; y < 8; y++ {
				copy(pbuf[y*8:], p[y][:])
			}
			c := sa8d(sb, ss, pbuf[:], 8)
			if mode == pm {
				c += s.lambda
			} else {
				c += 4 * s.lambda
			}
			if c < bestCost {
				best, bestCost, bestPred = mode, c, p
			}
		}
		for k := 0; k < 4; k++ {
			s.modes[(by/4+k>>1)*4+bx/4+k&1] = int8(best)
		}
		cost += bestCost
		db := Y[by*ys+bx:]
		for y := 0; y < 8; y++ {
			copy(db[y*ys:y*ys+8], bestPred[y][:])
		}
		blk := &s.luma8Lv[b8]
		fdct8(blk, sb, ss, db, ys)
		if quant8(blk, s.qp, true) > 0 {
			s.cbpL |= 1 << uint(b8)
			c := *blk
			dequant8(&c, 0, s.qp)
			h264.IDCT8Add(db, ys, &c)
		}
	}
	return cost
}

// codeI8Final finishes an Intra8x8 macroblock: analyseI8 already coded and
// reconstructed it.
func (s *sliceEnc) codeI8Final() {}

// choose8x8Inter decides the inter transform size by comparing the SA8D
// and SATD of the luma residual.
func (s *sliceEnc) choose8x8Inter() bool {
	src, ss := s.srcY()
	a8, a4 := 0, 0
	for b8 := 0; b8 < 4; b8++ {
		bx, by := (b8&1)*8, (b8>>1)*8
		a8 += sa8d(src[by*ss+bx:], ss, s.predY[by*16+bx:], 16)
		a4 += satd(src[by*ss+bx:], ss, s.predY[by*16+bx:], 16, 8, 8)
	}
	return a8 < a4
}

// codeInter8 codes the luma residual with 8x8 transforms.
func (s *sliceEnc) codeInter8() {
	Y, ys := s.plane()
	src, ss := s.srcY()
	var score [4]int
	var nz [4]bool
	total := 0
	for b8 := 0; b8 < 4; b8++ {
		bx, by := (b8&1)*8, (b8>>1)*8
		blk := &s.luma8Lv[b8]
		fdct8(blk, src[by*ss+bx:], ss, s.predY[by*16+bx:], 16)
		if quant8(blk, s.qp, false) > 0 {
			nz[b8] = true
			score[b8] = decimateScore64(blk)
		}
		if score[b8] < 4 {
			*blk = [64]int32{}
			nz[b8], score[b8] = false, 0
		}
		total += score[b8]
	}
	if total < 6 {
		for b8 := range s.luma8Lv {
			s.luma8Lv[b8] = [64]int32{}
			nz[b8] = false
		}
	}
	for y := 0; y < 16; y++ {
		copy(Y[y*ys:y*ys+16], s.predY[y*16:y*16+16])
	}
	for b8 := 0; b8 < 4; b8++ {
		if !nz[b8] {
			continue
		}
		bx, by := (b8&1)*8, (b8>>1)*8
		s.cbpL |= 1 << uint(b8)
		c := s.luma8Lv[b8]
		dequant8(&c, 1, s.qp)
		h264.IDCT8Add(Y[by*ys+bx:], ys, &c)
	}
}

var decimateTab8 = [64]uint8{
	3, 3, 3, 3, 2, 2, 2, 2, 2, 2, 2, 2, 1, 1, 1, 1,
	1, 1, 1, 1, 1, 1, 1, 1, 0, 0, 0, 0, 0, 0, 0, 0,
}

// decimateScore64 is decimateScore for an 8x8 block (raster).
func decimateScore64(b *[64]int32) int {
	var sc [64]int32
	for i := 0; i < 64; i++ {
		sc[i] = b[zz8[i]]
	}
	i := 63
	for i >= 0 && sc[i] == 0 {
		i--
	}
	score := 0
	for i >= 0 {
		if sc[i] > 1 || sc[i] < -1 {
			return 9
		}
		run := 0
		i--
		for i >= 0 && sc[i] == 0 {
			i--
			run++
		}
		score += int(decimateTab8[run])
	}
	return score
}

// sa8d is the 8x8 Hadamard-transformed sum of absolute differences.
func sa8d(a []byte, as int, b []byte, bs int) int {
	var d [64]int32
	for y := 0; y < 8; y++ {
		ra, rb := a[y*as:y*as+8], b[y*bs:y*bs+8]
		for x := 0; x < 8; x++ {
			d[y*8+x] = int32(ra[x]) - int32(rb[x])
		}
	}
	for y := 0; y < 8; y++ {
		hadamard8(d[y*8 : y*8+8])
	}
	var col [8]int32
	sum := int32(0)
	for x := 0; x < 8; x++ {
		for y := 0; y < 8; y++ {
			col[y] = d[y*8+x]
		}
		hadamard8(col[:])
		for _, v := range col {
			sum += abs32(v)
		}
	}
	return int(sum+2) >> 2
}

func hadamard8(v []int32) {
	a0, a1 := v[0]+v[1], v[0]-v[1]
	a2, a3 := v[2]+v[3], v[2]-v[3]
	a4, a5 := v[4]+v[5], v[4]-v[5]
	a6, a7 := v[6]+v[7], v[6]-v[7]
	b0, b1, b2, b3 := a0+a2, a1+a3, a0-a2, a1-a3
	b4, b5, b6, b7 := a4+a6, a5+a7, a4-a6, a5-a7
	v[0], v[1], v[2], v[3] = b0+b4, b1+b5, b2+b6, b3+b7
	v[4], v[5], v[6], v[7] = b0-b4, b1-b5, b2-b6, b3-b7
}
