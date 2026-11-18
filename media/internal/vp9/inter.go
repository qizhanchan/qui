package vp9

// Inter prediction (libvpx vp9_decodeframe.c dec_build_inter_predictors
// and vpx_dsp/vpx_convolve.c), including scaled references.

type scaleFactors struct {
	xScale, yScale int // 1<<14 = unscaled
	xStep, yStep   int // q4
}

func newScale(otherW, otherH, thisW, thisH int) scaleFactors {
	s := scaleFactors{
		xScale: (otherW << 14) / thisW,
		yScale: (otherH << 14) / thisH,
	}
	s.xStep = s.scaleX(16)
	s.yStep = s.scaleY(16)
	return s
}

func (s *scaleFactors) scaled() bool     { return s.xScale != 1<<14 || s.yScale != 1<<14 }
func (s *scaleFactors) scaleX(v int) int { return int((int64(v) * int64(s.xScale)) >> 14) }
func (s *scaleFactors) scaleY(v int) int { return int((int64(v) * int64(s.yScale)) >> 14) }

var filterKernels = [4]*[16][8]int16{&kernelRegular, &kernelSmooth, &kernelSharp, &kernelBilinear}

func (t *tileState) predictInter() {
	d := t.d
	b := t.bi
	nrefs := 1
	if b.hasSecond() {
		nrefs = 2
	}
	kernel := filterKernels[b.filter&3]
	miX, miY := t.miCol*8, t.miRow*8
	for r := 0; r < nrefs; r++ {
		ri := int(b.ref[r]) - 1
		ref := d.refFrames[ri]
		sf := &d.scale[ri]
		for p := 0; p < 3; p++ {
			ss := 0
			if p > 0 {
				ss = 1
			}
			n4w := max((1<<t.bwl)>>ss, 1)
			n4h := max((1<<t.bhl)>>ss, 1)
			bw, bh := 4*n4w, 4*n4h
			if b.bsize < block8x8 {
				i := 0
				for y := 0; y < n4h; y++ {
					for x := 0; x < n4w; x++ {
						m := t.splitMV(p, r, i)
						i++
						t.buildPred(p, ref, sf, kernel, bw, bh, 4*x, 4*y, 4, 4, miX, miY, m, r)
					}
				}
			} else {
				t.buildPred(p, ref, sf, kernel, bw, bh, 0, 0, bw, bh, miX, miY, b.mv[r], r)
			}
		}
	}
}

func roundQ4(v int) int {
	if v < 0 {
		return (v - 2) / 4
	}
	return (v + 2) / 4
}

func roundQ2(v int) int {
	if v < 0 {
		return (v - 1) / 2
	}
	return (v + 1) / 2
}

// splitMV is libvpx average_split_mvs.
func (t *tileState) splitMV(p, r, block int) mv {
	b := t.bi
	if p == 0 {
		return b.bmv[block][r]
	}
	// 4:2:0: the average of all four.
	var rs, cs int
	for i := 0; i < 4; i++ {
		rs += int(b.bmv[i][r].row)
		cs += int(b.bmv[i][r].col)
	}
	return mv{int16(roundQ4(rs)), int16(roundQ4(cs))}
}

// buildPred predicts the w×h block at (x,y) inside the plane block of
// size bw×bh.
func (t *tileState) buildPred(p int, ref *frame, sf *scaleFactors, kernel *[16][8]int16, bw, bh, x, y, w, h, miX, miY int, m mv, which int) {
	d := t.d
	cur := d.cur
	ss := 0
	if p > 0 {
		ss = 1
	}
	dstStride := cur.stride[p]
	xStart := (-t.toLeft >> (3 + ss))
	yStart := (-t.toTop >> (3 + ss))
	dst := cur.planes[p][(yStart+y)*dstStride+xStart+x:]
	fw, fh := ref.pw[p], ref.ph[p]

	// clamp_mv_to_umv_border_sb
	mul := 2 >> ss // 1 << (1 - ss)
	spelLeft := (4 + bw) << 4
	spelRight := spelLeft - 16
	spelTop := (4 + bh) << 4
	spelBottom := spelTop - 16
	mr, mc := int(m.row)*mul, int(m.col)*mul
	mc = min(max(mc, t.toLeft*mul-spelLeft), t.toRight*mul+spelRight)
	mr = min(max(mr, t.toTop*mul-spelTop), t.toBottom*mul+spelBottom)

	var x0, y0, smvC, smvR, xs, ys, x0_16, y0_16 int
	if sf.scaled() {
		x0_16 = sf.scaleX((xStart + x) << 4)
		y0_16 = sf.scaleY((yStart + y) << 4)
		x0 = sf.scaleX(xStart + x)
		y0 = sf.scaleY(yStart + y)
		smvR = sf.scaleY(mr) + (sf.scaleY((miY+y)<<4) & 15)
		smvC = sf.scaleX(mc) + (sf.scaleX((miX+x)<<4) & 15)
		xs, ys = sf.xStep, sf.yStep
	} else {
		x0, y0 = xStart+x, yStart+y
		x0_16, y0_16 = x0<<4, y0<<4
		smvR, smvC = mr, mc
		xs, ys = 16, 16
	}
	subX, subY := smvC&15, smvR&15
	x0 += smvC >> 4
	y0 += smvR >> 4
	x0_16 += smvC
	y0_16 += smvR
	_ = x0_16
	_ = y0_16

	src := ref.planes[p]
	srcStride := ref.stride[p]
	srcOff := 0
	// Reference pixels needed: rows y0-3 .. y0+((subY+(h-1)*ys)>>4)+4.
	lastX := x0 + ((subX + (w-1)*xs) >> 4)
	lastY := y0 + ((subY + (h-1)*ys) >> 4)
	if x0-3 < 0 || lastX+4 > fw-1 || y0-3 < 0 || lastY+4 > fh-1 {
		// Emulate the edge: clamp every coordinate into the frame.
		bwE := lastX + 4 - (x0 - 3) + 1
		bhE := lastY + 4 - (y0 - 3) + 1
		buf := t.edgeBuf[:bwE*bhE]
		for r := 0; r < bhE; r++ {
			sy := min(max(y0-3+r, 0), fh-1)
			row := src[sy*srcStride:]
			o := buf[r*bwE : r*bwE+bwE]
			for c := range o {
				sx := min(max(x0-3+c, 0), fw-1)
				o[c] = row[sx]
			}
		}
		src = buf
		srcOff = 3*bwE + 3
		srcStride = bwE
	} else {
		srcOff = y0*srcStride + x0
	}
	convolve(src, srcOff, srcStride, dst, dstStride, kernel, subX, xs, subY, ys, w, h, which == 1, &t.mcTmp)
}

// convolve is vpx_convolve8 (scaled when the steps differ from 16) with
// optional averaging into dst.
func convolve(src []byte, srcOff, srcStride int, dst []byte, dstStride int, kernel *[16][8]int16, x0q4, xStep, y0q4, yStep, w, h int, avg bool, tmpBuf *[(64*2 + 16) * 64]uint8) {
	if x0q4 == 0 && y0q4 == 0 && xStep == 16 && yStep == 16 {
		for r := 0; r < h; r++ {
			s := src[srcOff+r*srcStride : srcOff+r*srcStride+w]
			o := dst[r*dstStride : r*dstStride+w]
			if avg {
				for i := range o {
					o[i] = uint8((int(o[i]) + int(s[i]) + 1) >> 1)
				}
			} else {
				copy(o, s)
			}
		}
		return
	}
	ih := (((h-1)*yStep + y0q4) >> 4) + 8
	tmp := tmpBuf[:ih*64]
	// Horizontal pass over rows starting 3 above.
	base := srcOff - 3*srcStride
	if xStep == 16 && x0q4 == 0 {
		for r := 0; r < ih; r++ {
			copy(tmp[r*64:r*64+w], src[base+r*srcStride:])
		}
	} else {
		for r := 0; r < ih; r++ {
			s := src[base+r*srcStride-3:]
			o := tmp[r*64 : r*64+w]
			xq := x0q4
			if xStep == 16 {
				f := &kernel[xq&15]
				for i := range o {
					sx := s[i : i+8]
					sum := int(sx[0])*int(f[0]) + int(sx[1])*int(f[1]) + int(sx[2])*int(f[2]) + int(sx[3])*int(f[3]) +
						int(sx[4])*int(f[4]) + int(sx[5])*int(f[5]) + int(sx[6])*int(f[6]) + int(sx[7])*int(f[7])
					o[i] = clipPixel((sum + 64) >> 7)
				}
			} else {
				for i := range o {
					sx := s[xq>>4 : xq>>4+8]
					f := &kernel[xq&15]
					sum := int(sx[0])*int(f[0]) + int(sx[1])*int(f[1]) + int(sx[2])*int(f[2]) + int(sx[3])*int(f[3]) +
						int(sx[4])*int(f[4]) + int(sx[5])*int(f[5]) + int(sx[6])*int(f[6]) + int(sx[7])*int(f[7])
					o[i] = clipPixel((sum + 64) >> 7)
					xq += xStep
				}
			}
		}
	}
	// Vertical pass.
	yq := y0q4
	for r := 0; r < h; r++ {
		o := dst[r*dstStride : r*dstStride+w]
		f := &kernel[yq&15]
		t0 := tmp[(yq>>4)*64:]
		if yq&15 == 0 {
			s := t0[3*64:]
			for i := range o {
				v := s[i]
				if avg {
					v = uint8((int(o[i]) + int(v) + 1) >> 1)
				}
				o[i] = v
			}
		} else {
			for i := range o {
				sum := int(t0[i])*int(f[0]) + int(t0[64+i])*int(f[1]) + int(t0[128+i])*int(f[2]) + int(t0[192+i])*int(f[3]) +
					int(t0[256+i])*int(f[4]) + int(t0[320+i])*int(f[5]) + int(t0[384+i])*int(f[6]) + int(t0[448+i])*int(f[7])
				v := clipPixel((sum + 64) >> 7)
				if avg {
					v = uint8((int(o[i]) + int(v) + 1) >> 1)
				}
				o[i] = v
			}
		}
		yq += yStep
	}
}
