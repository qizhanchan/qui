package h264

// Fractional-sample motion compensation (spec 8.4.2.2). Reference samples
// outside the picture are replicated from the nearest edge sample by copying
// the needed window into an emulation buffer first.

func clip255(v int32) byte {
	if uint32(v) > 255 {
		if v < 0 {
			return 0
		}
		return 255
	}
	return byte(v)
}

// emulateEdge copies the bw x bh window at (x, y) of a pw x ph plane into
// buf (stride bw), clamping coordinates to the plane.
func emulateEdge(buf []byte, src []byte, ss, pw, ph, x, y, bw, bh int) {
	for j := 0; j < bh; j++ {
		sy := clip3(0, ph-1, y+j)
		row := src[sy*ss:]
		d := buf[j*bw : j*bw+bw]
		for i := range d {
			d[i] = row[clip3(0, pw-1, x+i)]
		}
	}
}

// mcLuma predicts a w x h luma block whose top-left integer position in the
// reference is (x, y) with quarter-sample fraction (fx, fy).
func (s *sliceCtx) mcLuma(dst []byte, ds int, src []byte, ss, pw, ph, x, y, fx, fy, w, h int) {
	o := y*ss + x
	if x < 2 || y < 2 || x+w+3 > pw || y+h+3 > ph {
		bw := w + 5
		emulateEdge(s.emu[:], src, ss, pw, ph, x-2, y-2, bw, h+5)
		src, ss, o = s.emu[:], bw, 2*bw+2
	}
	switch {
	case fx == 0 && fy == 0:
		for j := 0; j < h; j++ {
			copy(dst[j*ds:j*ds+w], src[o+j*ss:o+j*ss+w])
		}
	case fy == 0:
		lumaH(dst, ds, src, o, ss, w, h)
		if fx != 2 {
			avgInto(dst, ds, src, o+(fx>>1), ss, w, h)
		}
	case fx == 0:
		lumaV(dst, ds, src, o, ss, w, h)
		if fy != 2 {
			avgInto(dst, ds, src, o+(fy>>1)*ss, ss, w, h)
		}
	case fx == 2 && fy == 2:
		s.lumaHV(dst, ds, src, o, ss, w, h)
	case fx == 2:
		s.lumaHV(dst, ds, src, o, ss, w, h)
		var t [256]byte
		lumaH(t[:], 16, src, o+(fy>>1)*ss, ss, w, h)
		avgInto(dst, ds, t[:], 0, 16, w, h)
	case fy == 2:
		s.lumaHV(dst, ds, src, o, ss, w, h)
		var t [256]byte
		lumaV(t[:], 16, src, o+(fx>>1), ss, w, h)
		avgInto(dst, ds, t[:], 0, 16, w, h)
	default:
		lumaH(dst, ds, src, o+(fy>>1)*ss, ss, w, h)
		var t [256]byte
		lumaV(t[:], 16, src, o+(fx>>1), ss, w, h)
		avgInto(dst, ds, t[:], 0, 16, w, h)
	}
}

// lumaH, lumaV and lumaHV run the NEON kernels for 16- and 8-wide blocks
// when every byte the kernel touches is inside the slices (the kernels load
// whole vectors, so a block at a buffer's very end takes the Go path).

func lumaH(dst []byte, ds int, src []byte, o, ss, w, h int) {
	if haveNEON && (w == 16 || w == 8) {
		start := o - 2
		if start >= 0 && start+(h-1)*ss+2*w <= len(src) && (h-1)*ds+w <= len(dst) {
			neonLumaH(&dst[0], ds, &src[start], ss, w, h)
			return
		}
	}
	lumaHGo(dst, ds, src, o, ss, w, h)
}

func lumaV(dst []byte, ds int, src []byte, o, ss, w, h int) {
	if haveNEON && (w == 16 || w == 8) {
		start := o - 2*ss
		if start >= 0 && o+(h+2)*ss+w <= len(src) && (h-1)*ds+w <= len(dst) {
			neonLumaV(&dst[0], ds, &src[start], ss, w, h)
			return
		}
	}
	lumaVGo(dst, ds, src, o, ss, w, h)
}

func (s *sliceCtx) lumaHV(dst []byte, ds int, src []byte, o, ss, w, h int) {
	if haveNEON && (w == 16 || w == 8) && h <= 16 {
		start := o - 2*ss - 2
		if start >= 0 && o+(h+2)*ss-2+24 <= len(src) && (h-1)*ds+w <= len(dst) {
			neonLumaHV(&dst[0], ds, &src[start], ss, w, h, &s.hvTmp[0])
			return
		}
	}
	s.lumaHVGo(dst, ds, src, o, ss, w, h)
}

// avgInto sets dst = (dst + src + 1) >> 1.
func avgInto(dst []byte, ds int, src []byte, o, ss, w, h int) {
	avg2(dst, ds, dst, ds, src[o:], ss, w, h)
}

func lumaHGo(dst []byte, ds int, src []byte, o, ss, w, h int) {
	for j := 0; j < h; j++ {
		r := src[o+j*ss-2 : o+j*ss+w+3]
		d := dst[j*ds : j*ds+w]
		for i := range d {
			v := int32(r[i]) - 5*int32(r[i+1]) + 20*int32(r[i+2]) + 20*int32(r[i+3]) - 5*int32(r[i+4]) + int32(r[i+5])
			d[i] = clip255((v + 16) >> 5)
		}
	}
}

func lumaVGo(dst []byte, ds int, src []byte, o, ss, w, h int) {
	for j := 0; j < h; j++ {
		b := o + j*ss
		r0 := src[b-2*ss : b-2*ss+w]
		r1 := src[b-ss : b-ss+w]
		r2 := src[b : b+w]
		r3 := src[b+ss : b+ss+w]
		r4 := src[b+2*ss : b+2*ss+w]
		r5 := src[b+3*ss : b+3*ss+w]
		d := dst[j*ds : j*ds+w]
		for i := range d {
			v := int32(r0[i]) - 5*int32(r1[i]) + 20*int32(r2[i]) + 20*int32(r3[i]) - 5*int32(r4[i]) + int32(r5[i])
			d[i] = clip255((v + 16) >> 5)
		}
	}
}

// lumaHV computes the centre half-sample position j.
func (s *sliceCtx) lumaHVGo(dst []byte, ds int, src []byte, o, ss, w, h int) {
	tw := w + 5
	t := s.mcTmp[:tw*h]
	for j := 0; j < h; j++ {
		b := o + j*ss - 2
		r0 := src[b-2*ss : b-2*ss+tw]
		r1 := src[b-ss : b-ss+tw]
		r2 := src[b : b+tw]
		r3 := src[b+ss : b+ss+tw]
		r4 := src[b+2*ss : b+2*ss+tw]
		r5 := src[b+3*ss : b+3*ss+tw]
		row := t[j*tw : j*tw+tw]
		for i := range row {
			row[i] = int32(r0[i]) - 5*int32(r1[i]) + 20*int32(r2[i]) + 20*int32(r3[i]) - 5*int32(r4[i]) + int32(r5[i])
		}
	}
	for j := 0; j < h; j++ {
		r := t[j*tw : j*tw+tw]
		d := dst[j*ds : j*ds+w]
		for i := range d {
			v := r[i] - 5*r[i+1] + 20*r[i+2] + 20*r[i+3] - 5*r[i+4] + r[i+5]
			d[i] = clip255((v + 512) >> 10)
		}
	}
}

// mcChroma predicts a w x h chroma block at integer (x, y) with eighth-sample
// fraction (fx, fy).
func (s *sliceCtx) mcChroma(dst []byte, ds int, src []byte, ss, pw, ph, x, y, fx, fy, w, h int) {
	o := y*ss + x
	if x < 0 || y < 0 || x+w+1 > pw || y+h+1 > ph {
		bw := w + 1
		emulateEdge(s.emu[:], src, ss, pw, ph, x, y, bw, h+1)
		src, ss, o = s.emu[:], bw, 0
	}
	if fx == 0 && fy == 0 {
		for j := 0; j < h; j++ {
			copy(dst[j*ds:j*ds+w], src[o+j*ss:o+j*ss+w])
		}
		return
	}
	a := int32((8 - fx) * (8 - fy))
	b := int32(fx * (8 - fy))
	c := int32((8 - fx) * fy)
	dd := int32(fx * fy)
	if haveNEON && (w == 8 || w == 4) && o+h*ss+2*w <= len(src) && (h-1)*ds+w <= len(dst) {
		neonChroma(&dst[0], ds, &src[o], ss, w, h, int(a), int(b), int(c), int(dd))
		return
	}
	for j := 0; j < h; j++ {
		r0 := src[o+j*ss : o+j*ss+w+1]
		r1 := src[o+(j+1)*ss : o+(j+1)*ss+w+1]
		d := dst[j*ds : j*ds+w]
		for i := range d {
			d[i] = byte((a*int32(r0[i]) + b*int32(r0[i+1]) + c*int32(r1[i]) + dd*int32(r1[i+1]) + 32) >> 6)
		}
	}
}
