package av1

// Superres upscaling (spec 7.16) and loop restoration (spec 7.17).

// upscaleFilter is the normative 8-tap superres filter.
var upscaleFilter = [64][8]int32{}

var upscaleFilterFlat = [512]int32{0, 0, 0, 128, 0, 0, 0, 0, 0, 0, -1, 128, 2, -1, 0, 0, 0, 1, -3, 127, 4, -2, 1, 0, 0, 1, -4, 127, 6, -3, 1, 0, 0, 2, -6, 126, 8, -3, 1, 0, 0, 2, -7, 125, 11, -4, 1, 0, -1, 2, -8, 125, 13, -5, 2, 0, -1, 3, -9, 124, 15, -6, 2, 0, -1, 3, -10, 123, 18, -6, 2, -1, -1, 3, -11, 122, 20, -7, 3, -1, -1, 4, -12, 121, 22, -8, 3, -1, -1, 4, -13, 120, 25, -9, 3, -1, -1, 4, -14, 118, 28, -9, 3, -1, -1, 4, -15, 117, 30, -10, 4, -1, -1, 5, -16, 116, 32, -11, 4, -1, -1, 5, -16, 114, 35, -12, 4, -1, -1, 5, -17, 112, 38, -12, 4, -1, -1, 5, -18, 111, 40, -13, 5, -1, -1, 5, -18, 109, 43, -14, 5, -1, -1, 6, -19, 107, 45, -14, 5, -1, -1, 6, -19, 105, 48, -15, 5, -1, -1, 6, -19, 103, 51, -16, 5, -1, -1, 6, -20, 101, 53, -16, 6, -1, -1, 6, -20, 99, 56, -17, 6, -1, -1, 6, -20, 97, 58, -17, 6, -1, -1, 6, -20, 95, 61, -18, 6, -1, -2, 7, -20, 93, 64, -18, 6, -2, -2, 7, -20, 91, 66, -19, 6, -1, -2, 7, -20, 88, 69, -19, 6, -1, -2, 7, -20, 86, 71, -19, 6, -1, -2, 7, -20, 84, 74, -20, 7, -2, -2, 7, -20, 81, 76, -20, 7, -1, -2, 7, -20, 79, 79, -20, 7, -2, -1, 7, -20, 76, 81, -20, 7, -2, -2, 7, -20, 74, 84, -20, 7, -2, -1, 6, -19, 71, 86, -20, 7, -2, -1, 6, -19, 69, 88, -20, 7, -2, -1, 6, -19, 66, 91, -20, 7, -2, -2, 6, -18, 64, 93, -20, 7, -2, -1, 6, -18, 61, 95, -20, 6, -1, -1, 6, -17, 58, 97, -20, 6, -1, -1, 6, -17, 56, 99, -20, 6, -1, -1, 6, -16, 53, 101, -20, 6, -1, -1, 5, -16, 51, 103, -19, 6, -1, -1, 5, -15, 48, 105, -19, 6, -1, -1, 5, -14, 45, 107, -19, 6, -1, -1, 5, -14, 43, 109, -18, 5, -1, -1, 5, -13, 40, 111, -18, 5, -1, -1, 4, -12, 38, 112, -17, 5, -1, -1, 4, -12, 35, 114, -16, 5, -1, -1, 4, -11, 32, 116, -16, 5, -1, -1, 4, -10, 30, 117, -15, 4, -1, -1, 3, -9, 28, 118, -14, 4, -1, -1, 3, -9, 25, 120, -13, 4, -1, -1, 3, -8, 22, 121, -12, 4, -1, -1, 3, -7, 20, 122, -11, 3, -1, -1, 2, -6, 18, 123, -10, 3, -1, 0, 2, -6, 15, 124, -9, 3, -1, 0, 2, -5, 13, 125, -8, 2, -1, 0, 1, -4, 11, 125, -7, 2, 0, 0, 1, -3, 8, 126, -6, 2, 0, 0, 1, -3, 6, 127, -4, 1, 0, 0, 1, -2, 4, 127, -3, 1, 0, 0, 0, -1, 2, 128, -1, 0, 0}

func init() {
	for i := 0; i < 64; i++ {
		for k := 0; k < 8; k++ {
			upscaleFilter[i][k] = upscaleFilterFlat[i*8+k]
		}
	}
}

// cloneFrame returns a copy of f's planes.
func cloneFrame(f *frame) *frame {
	g := *f
	for p := 0; p < f.numPlanes; p++ {
		if f.planes[p].pix16 != nil {
			g.planes[p].pix16 = append([]uint16(nil), f.planes[p].pix16...)
		} else {
			g.planes[p].pix8 = append([]uint8(nil), f.planes[p].pix8...)
		}
	}
	return &g
}

// upscale returns the superres-upscaled frame (spec 7.16).
func upscale(f *frameState, src *frame) *frame {
	h := f.hdr
	dst := newFrame(h.UpscaledWidth, h.Height, src.bitDepth, src.numPlanes)
	for p := 0; p < src.numPlanes; p++ {
		ss := min(p, 1)
		inW := (h.Width + ss) >> ss
		outW := (h.UpscaledWidth + ss) >> ss
		ph := (h.Height + ss) >> ss
		step := ((inW << 14) + outW/2) / outW
		err := outW*step - inW<<14
		x0 := ((-((outW-inW)<<13)+outW/2)/outW + 128 - err/2) & 0x3fff
		maxX := (f.miCols*4)>>ss - 1
		sp, dp := &src.planes[p], &dst.planes[p]
		maxV := int32(1)<<src.bitDepth - 1
		for y := 0; y < ph; y++ {
			for x := 0; x < outW; x++ {
				srcX := -(1 << 14) + x0 + x*step
				srcP := srcX >> 14
				fl := &upscaleFilter[(srcX&0x3fff)>>8]
				sum := int32(0)
				for k := 0; k < 8; k++ {
					sum += sp.get(clip3(0, maxX, srcP+k-3), y) * fl[k]
				}
				dp.set(x, y, min(max((sum+64)>>7, 0), maxV))
			}
		}
	}
	return dst
}

// lrState holds the inputs of loop restoration for one frame.
type lrState struct {
	f             *frameState
	cdef, deblock *frame // UpscaledCdefFrame, UpscaledCurrFrame
	stripeStart   int
	stripeEnd     int
	planeEndX     int
	planeEndY     int
}

func (s *lrState) sample(plane, x, y int) int32 {
	x = clip3(0, s.planeEndX, x)
	y = clip3(0, s.planeEndY, y)
	if y < s.stripeStart {
		return s.deblock.planes[plane].get(x, max(s.stripeStart-2, y))
	}
	if y > s.stripeEnd {
		return s.deblock.planes[plane].get(x, min(s.stripeEnd+2, y))
	}
	return s.cdef.planes[plane].get(x, y)
}

// restore applies loop restoration, returning the restored frame.
func (d *Decoder) restore(f *frameState, cdefF, deblockF *frame) *frame {
	h := f.hdr
	out := cloneFrame(cdefF)
	s := &lrState{f: f, cdef: cdefF, deblock: deblockF}
	for p := 0; p < cdefF.numPlanes; p++ {
		if h.lrType[p] == restoreNone {
			continue
		}
		ss := min(p, 1)
		lp := &f.lr[p]
		unit := h.lrUnitSize[p]
		s.planeEndX = (h.UpscaledWidth+ss)>>ss - 1
		s.planeEndY = (h.Height+ss)>>ss - 1
		for y := 0; y < h.Height; y += 4 {
			row := y >> 2
			stripe := (y + 8) / 64
			s.stripeStart = (-8 + stripe*64) >> ss
			s.stripeEnd = s.stripeStart + 64>>ss - 1
			unitRow := min(lp.rows-1, ((row*4+8)>>ss)/unit)
			for x := 0; x < h.UpscaledWidth; x += 4 {
				col := x >> 2
				unitCol := min(lp.cols-1, ((col*4)>>ss)/unit)
				px, py := (col*4)>>ss, (row*4)>>ss
				w := min(4>>ss, s.planeEndX-px+1)
				hh := min(4>>ss, s.planeEndY-py+1)
				if ss == 1 {
					// Chroma in 4x4 blocks (two luma units each way):
					// stripe and unit boundaries fall on multiples of 4.
					if row&1 != 0 || col&1 != 0 {
						continue
					}
					w, hh = min(4, s.planeEndX-px+1), min(4, s.planeEndY-py+1)
				}
				u := &lp.units[unitRow*lp.cols+unitCol]
				switch u.typ {
				case restoreWiener:
					s.wiener(out, p, u, px, py, w, hh)
				case restoreSgrproj:
					s.selfGuided(out, p, u, px, py, w, hh)
				}
			}
		}
	}
	return out
}

func (s *lrState) wiener(out *frame, plane int, u *lrUnit, x, y, w, h int) {
	bd := out.bitDepth
	r0, r1 := 3, 11
	if bd == 12 {
		r0, r1 = 5, 9
	}
	var vf, hf [7]int32
	mk := func(c [3]int8, fl *[7]int32) {
		fl[3] = 128
		for i := 0; i < 3; i++ {
			fl[i] = int32(c[i])
			fl[6-i] = int32(c[i])
			fl[3] -= 2 * int32(c[i])
		}
	}
	mk(u.wiener[0], &vf)
	mk(u.wiener[1], &hf)
	offset := int32(1) << (bd + 7 - r0 - 1)
	limit := int32(1)<<(bd+1+7-r0) - 1
	var inter [(4 + 6) * 4]int32
	for r := 0; r < h+6; r++ {
		for c := 0; c < w; c++ {
			sum := int32(0)
			for t := 0; t < 7; t++ {
				sum += hf[t] * s.sample(plane, x+c+t-3, y+r-3)
			}
			v := (sum + 1<<(r0-1)) >> r0
			inter[r*4+c] = min(max(v, -offset), limit-offset)
		}
	}
	pb := &out.planes[plane]
	maxV := int32(1)<<bd - 1
	for r := 0; r < h; r++ {
		for c := 0; c < w; c++ {
			sum := int32(0)
			for t := 0; t < 7; t++ {
				sum += vf[t] * inter[(r+t)*4+c]
			}
			v := (sum + 1<<(r1-1)) >> r1
			pb.set(x+c, y+r, min(max(v, 0), maxV))
		}
	}
}

func (s *lrState) boxFilter(plane, x, y, w, h, set, pass int, flt *[4 * 4]int32) {
	bd := s.cdef.bitDepth
	r := sgrParams[set][pass*2]
	sc := sgrParams[set][pass*2+1]
	n := (2*r + 1) * (2*r + 1)
	var A, B [6][6]int32
	oneOverN := int64(((1 << 12) + n/2) / n)
	for i := -1; i < h+1; i++ {
		for j := -1; j < w+1; j++ {
			var a, b int64
			for dy := -r; dy <= r; dy++ {
				for dx := -r; dx <= r; dx++ {
					c := int64(s.sample(plane, x+j+dx, y+i+dy))
					a += c * c
					b += c
				}
			}
			sh := uint(bd - 8)
			if sh > 0 {
				a = (a + 1<<(2*sh-1)) >> (2 * sh)
			}
			dd := b
			if sh > 0 {
				dd = (b + 1<<(sh-1)) >> sh
			}
			p := max(0, a*int64(n)-dd*dd)
			z := (p*int64(sc) + 1<<19) >> 20
			var a2 int64
			switch {
			case z >= 255:
				a2 = 256
			case z == 0:
				a2 = 1
			default:
				a2 = ((z << 8) + z/2) / (z + 1)
			}
			b2 := ((1 << 8) - a2) * b * oneOverN
			A[i+1][j+1] = int32(a2)
			B[i+1][j+1] = int32((b2 + 1<<11) >> 12)
		}
	}
	for i := 0; i < h; i++ {
		shift := 5
		if pass == 0 && (y+i)&1 != 0 {
			shift = 4
		}
		for j := 0; j < w; j++ {
			var a, b int32
			for dy := -1; dy <= 1; dy++ {
				for dx := -1; dx <= 1; dx++ {
					var wt int32
					if pass == 0 {
						if (y+i+dy)&1 != 0 {
							wt = 5
							if dx == 0 {
								wt = 6
							}
						}
					} else {
						wt = 3
						if dx == 0 || dy == 0 {
							wt = 4
						}
					}
					a += wt * A[i+dy+1][j+dx+1]
					b += wt * B[i+dy+1][j+dx+1]
				}
			}
			v := int64(a)*int64(s.cdef.planes[plane].get(x+j, y+i)) + int64(b)
			sh := uint(8 + shift - 4)
			flt[i*4+j] = int32((v + 1<<(sh-1)) >> sh)
		}
	}
}

func (s *lrState) selfGuided(out *frame, plane int, u *lrUnit, x, y, w, h int) {
	set := int(u.sgrSet)
	var flt0, flt1 [4 * 4]int32
	r0, r1 := sgrParams[set][0], sgrParams[set][2]
	if r0 != 0 {
		s.boxFilter(plane, x, y, w, h, set, 0, &flt0)
	}
	if r1 != 0 {
		s.boxFilter(plane, x, y, w, h, set, 1, &flt1)
	}
	w0, w1 := int64(u.xqd[0]), int64(u.xqd[1])
	w2 := (1 << 7) - w0 - w1
	pb := &out.planes[plane]
	maxV := int64(1)<<out.bitDepth - 1
	for i := 0; i < h; i++ {
		for j := 0; j < w; j++ {
			uu := int64(s.cdef.planes[plane].get(x+j, y+i)) << 4
			v := w1 * uu
			if r0 != 0 {
				v += w0 * int64(flt0[i*4+j])
			} else {
				v += w0 * uu
			}
			if r1 != 0 {
				v += w2 * int64(flt1[i*4+j])
			} else {
				v += w2 * uu
			}
			sv := (v + 1<<10) >> 11
			pb.set(x+j, y+i, int32(min(max(sv, 0), maxV)))
		}
	}
}
