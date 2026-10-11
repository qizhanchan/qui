package h264

// Macroblock reconstruction: intra prediction + dequantized residual.

func lumaBlockAvail(bx, by, size int, nb availability) availability {
	av := availability{
		left:     bx > 0 || nb.left,
		top:      by > 0 || nb.top,
		topRight: bx+size < 16,
	}
	switch {
	case bx > 0 && by > 0:
		av.topLeft = true
	case by > 0:
		av.topLeft = nb.left
	case bx > 0:
		av.topLeft = nb.top
	default:
		av.topLeft = nb.topLeft
	}
	if by == 0 {
		if bx+size < 16 {
			av.topRight = nb.top
		} else {
			av.topRight = nb.topRight
		}
	}
	return av
}

func (s *picState) reconstructMB(f *Frame, mbIdx int) error {
	mb := &s.mbs[mbIdx]
	if mb.mbType == 25 { // I_PCM already written by decodeIPCM
		return nil
	}
	mbX, mbY := mbIdx%s.mbWidth, mbIdx/s.mbWidth
	nb := s.mbNeighbors(mbIdx)

	switch {
	case mb.mbType >= 1 && mb.mbType <= 24:
		s.reconstructI16x16(f, mbX, mbY, mb, nb)
	case mb.mbType == 0:
		if mb.transform8x8 {
			s.reconstructI8x8(f, mbX, mbY, mb, nb)
		} else {
			s.reconstructI4x4(f, mbX, mbY, mb, nb)
		}
	}
	if s.chromaArrayType != 0 {
		s.reconstructChroma(f, mbX, mbY, mb, nb)
	}
	return nil
}

func (s *picState) reconstructI16x16(f *Frame, mbX, mbY int, mb *mbData, nb availability) {
	top, left, topLeft, hasTop, hasLeft := getLuma16x16Neighbors(f, mbX, mbY, nb)
	var topS, leftS []byte
	if hasTop {
		topS = top[:]
	}
	if hasLeft {
		leftS = left[:]
	}
	pred := predict16x16(mb.intraPredMode16, topS, leftS, topLeft)

	var dcMatrix [16]int32
	for k := 0; k < 16; k++ {
		dcMatrix[zigzag4x4[k]] = mb.intra16x16DC[k]
	}
	dcScaledRaster := dequantDC4x4(hadamard4x4(dcMatrix), mb.qpY)
	var dcScaled [16]int32
	for i := 0; i < 16; i++ {
		dcScaled[i] = dcScaledRaster[zScanToRaster[i]]
	}

	for i := 0; i < 16; i++ {
		bx := inverseRasterX4x4[i]
		by := inverseRasterY4x4[i]
		var block [16]int32
		block[0] = dcScaled[i]
		for j := 0; j < 15; j++ {
			block[zigzag4x4AC[j]] = mb.intra16x16AC[i][j]
		}
		deq := dequant4x4(block, mb.qpY)
		deq[0] = dcScaled[i]
		res := idct4x4(deq)
		for y := 0; y < 4; y++ {
			for x := 0; x < 4; x++ {
				f.Y.Set(mbX*16+bx+x, mbY*16+by+y, clip8(int32(pred[by+y][bx+x])+res[y*4+x]))
			}
		}
	}
}

func (s *picState) reconstructI4x4(f *Frame, mbX, mbY int, mb *mbData, nb availability) {
	x0, y0 := mbX*16, mbY*16
	for i := 0; i < 16; i++ {
		bx := inverseRasterX4x4[i]
		by := inverseRasterY4x4[i]
		av := lumaBlockAvail(bx, by, 4, nb)
		if topRightNotAvail4x4[i] {
			av.topRight = false
		}
		ref := getLuma4x4Neighbors(f, x0+bx, y0+by, av)
		pred := predict4x4(mb.intra4x4PredMode[i], ref, av.left, av.top)
		var coeffs [16]int32
		for j := 0; j < 16; j++ {
			coeffs[zigzag4x4[j]] = mb.lumaLevel4x4[i][j]
		}
		res := idct4x4(dequant4x4(coeffs, mb.qpY))
		for y := 0; y < 4; y++ {
			for x := 0; x < 4; x++ {
				f.Y.Set(x0+bx+x, y0+by+y, clip8(int32(pred[y][x])+res[y*4+x]))
			}
		}
	}
}

func (s *picState) reconstructChroma(f *Frame, mbX, mbY int, mb *mbData, nb availability) {
	qpc := chromaQP(mb.qpY + s.chromaQpIndexOffset)
	for c := 0; c < 2; c++ {
		top, left, topLeft, hasTop, hasLeft := getChromaNeighbors(f, c, mbX, mbY, nb)
		var topS, leftS []byte
		if hasTop {
			topS = top[:]
		}
		if hasLeft {
			leftS = left[:]
		}
		pred := predictChroma(mb.intraChromaMode, topS, leftS, topLeft)

		dcScaled := dequantChromaDC2x2(hadamard2x2(mb.chromaDC[c]), qpc)
		plane := f.Cb
		if c == 1 {
			plane = f.Cr
		}
		for blk := 0; blk < 4; blk++ {
			bx := (blk % 2) * 4
			by := (blk / 2) * 4
			var block [16]int32
			block[0] = dcScaled[blk]
			if mb.cbpChroma > 1 {
				for j := 0; j < 15; j++ {
					block[zigzag4x4AC[j]] = mb.chromaAC[c][blk][j]
				}
			}
			deq := dequant4x4(block, qpc)
			deq[0] = dcScaled[blk]
			res := idct4x4(deq)
			for y := 0; y < 4; y++ {
				for x := 0; x < 4; x++ {
					plane.Set(mbX*8+bx+x, mbY*8+by+y, clip8(int32(pred[by+y][bx+x])+res[y*4+x]))
				}
			}
		}
	}
}

func getLuma16x16Neighbors(f *Frame, mbX, mbY int, nb availability) (top, left [16]byte, topLeft byte, hasTop, hasLeft bool) {
	x0, y0 := mbX*16, mbY*16
	if nb.top {
		hasTop = true
		for x := 0; x < 16; x++ {
			top[x] = f.Y.At(x0+x, y0-1)
		}
	}
	if nb.left {
		hasLeft = true
		for y := 0; y < 16; y++ {
			left[y] = f.Y.At(x0-1, y0+y)
		}
	}
	if nb.topLeft {
		topLeft = f.Y.At(x0-1, y0-1)
	}
	return
}

func getLuma4x4Neighbors(f *Frame, x0, y0 int, av availability) [13]byte {
	var ref [13]byte
	for i := 0; i < 4; i++ {
		if av.left {
			ref[i] = f.Y.At(x0-1, y0+3-i)
		} else {
			ref[i] = 128
		}
	}
	if av.topLeft {
		ref[4] = f.Y.At(x0-1, y0-1)
	} else {
		ref[4] = 128
	}
	for i := 0; i < 4; i++ {
		if av.top {
			ref[5+i] = f.Y.At(x0+i, y0-1)
		} else {
			ref[5+i] = 128
		}
	}
	if av.top && av.topRight {
		for i := 4; i < 8; i++ {
			ref[5+i] = f.Y.At(x0+i, y0-1)
		}
	} else {
		for i := 4; i < 8; i++ {
			ref[5+i] = ref[8]
		}
	}
	return ref
}

func getChromaNeighbors(f *Frame, comp int, mbX, mbY int, nb availability) (top, left [8]byte, topLeft byte, hasTop, hasLeft bool) {
	plane := f.Cb
	if comp == 1 {
		plane = f.Cr
	}
	x0, y0 := mbX*8, mbY*8
	if nb.top {
		hasTop = true
		for x := 0; x < 8; x++ {
			top[x] = plane.At(x0+x, y0-1)
		}
	}
	if nb.left {
		hasLeft = true
		for y := 0; y < 8; y++ {
			left[y] = plane.At(x0-1, y0+y)
		}
	}
	if nb.topLeft {
		topLeft = plane.At(x0-1, y0-1)
	}
	return
}

// cropFrame copies the top-left display area out of a coded-size frame.
func cropFrame(src *Frame, w, h int) *Frame {
	if src.Y.W == w && src.Y.H == h {
		return src
	}
	dst := NewFrame(w, h)
	for y := 0; y < h; y++ {
		copy(dst.Y.Pix[y*dst.Y.Stride:], src.Y.Pix[y*src.Y.Stride:y*src.Y.Stride+w])
	}
	cw, ch := chromaSize(w), chromaSize(h)
	for y := 0; y < ch; y++ {
		copy(dst.Cb.Pix[y*dst.Cb.Stride:], src.Cb.Pix[y*src.Cb.Stride:y*src.Cb.Stride+cw])
		copy(dst.Cr.Pix[y*dst.Cr.Stride:], src.Cr.Pix[y*src.Cr.Stride:y*src.Cr.Stride+cw])
	}
	return dst
}

// reconstructI8x8 reconstructs an I_8x8 macroblock (High profile).
func (s *picState) reconstructI8x8(f *Frame, mbX, mbY int, mb *mbData, nb availability) {
	x0, y0 := mbX*16, mbY*16
	for i := 0; i < 4; i++ {
		bx := (i % 2) * 8
		by := (i / 2) * 8
		av := lumaBlockAvail(bx, by, 8, nb)
		ref := getLuma8x8Neighbors(f, x0+bx, y0+by, av)
		pred := predict8x8(mb.intra8x8PredMode[i], ref, av.left, av.top)
		res := idct8x8(dequant8x8(mb.lumaLevel8x8[i], mb.qpY))
		for y := 0; y < 8; y++ {
			for x := 0; x < 8; x++ {
				f.Y.Set(x0+bx+x, y0+by+y, clip8(int32(pred[y][x])+res[y*8+x]))
			}
		}
	}
}

// getLuma8x8Neighbors returns the 25 filtered reference samples for 8x8 intra
// prediction (spec 8.3.2.2.1). Layout: [L7..L0, TL, T0..T7, T8..T15].
func getLuma8x8Neighbors(f *Frame, blkX, blkY int, av availability) [25]byte {
	var p [25]int
	if av.left {
		for i := 0; i < 8; i++ {
			p[i] = int(f.Y.At(blkX-1, blkY+7-i))
		}
	}
	if av.topLeft {
		p[8] = int(f.Y.At(blkX-1, blkY-1))
	}
	if av.top {
		for i := 0; i < 8; i++ {
			p[9+i] = int(f.Y.At(blkX+i, blkY-1))
		}
		for i := 0; i < 8; i++ {
			if av.topRight {
				p[17+i] = int(f.Y.At(blkX+8+i, blkY-1))
			} else {
				p[17+i] = p[16]
			}
		}
	}

	var ref [25]byte
	if av.top {
		if av.topLeft {
			ref[9] = byte((p[8] + 2*p[9] + p[10] + 2) >> 2)
		} else {
			ref[9] = byte((3*p[9] + p[10] + 2) >> 2)
		}
		for i := 10; i < 24; i++ {
			ref[i] = byte((p[i-1] + 2*p[i] + p[i+1] + 2) >> 2)
		}
		ref[24] = byte((p[23] + 3*p[24] + 2) >> 2)
	}
	if av.topLeft {
		switch {
		case av.top && av.left:
			ref[8] = byte((p[9] + 2*p[8] + p[7] + 2) >> 2)
		case av.top:
			ref[8] = byte((3*p[8] + p[9] + 2) >> 2)
		case av.left:
			ref[8] = byte((3*p[8] + p[7] + 2) >> 2)
		default:
			ref[8] = byte(p[8])
		}
	}
	if av.left {
		if av.topLeft {
			ref[7] = byte((p[8] + 2*p[7] + p[6] + 2) >> 2)
		} else {
			ref[7] = byte((3*p[7] + p[6] + 2) >> 2)
		}
		for i := 1; i < 7; i++ {
			ref[i] = byte((p[i+1] + 2*p[i] + p[i-1] + 2) >> 2)
		}
		ref[0] = byte((p[1] + 3*p[0] + 2) >> 2)
	}
	return ref
}
