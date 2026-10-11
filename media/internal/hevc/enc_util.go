package hevc

// Encoder helpers: state snapshots for trial decisions, distortion
// measures and transform scratch space.

// encTQ is scratch space for transform and quantisation.
type encTQ struct {
	resid [32 * 32]int32
	coef  [32 * 32]int32
	tmp   [2 * 32 * 32]int32
	frac  [32 * 32]int32
	pred  [64 * 64]byte
	refs  intraRefs
}

// encSnap saves the reconstruction, 4x4 coding state and motion of a
// square region plus the bit estimator, so a trial decision can be undone.
type encSnap struct {
	x, y, n int
	py      [64 * 64]byte
	pcb     [32 * 32]byte
	pcr     [32 * 32]byte
	blk     [16 * 16]blkInfo
	mv      [2][256]int32
	refIdx  [2][256]int8
	refPOC  [2][256]int32
	refLT   [2][256]bool
	intra   [256]bool
	est     cabacEst
}

// save records the n x n luma region at (x, y) (clipped to the picture).
func (e *Encoder) save(sn *encSnap, x, y, n int) {
	s := &e.d.sd
	sp := s.sps
	pic := s.pic
	sn.x, sn.y, sn.n = x, y, n
	w, h := min(n, sp.Width-x), min(n, sp.Height-y)
	for r := 0; r < h; r++ {
		copy(sn.py[r*n:r*n+w], pic.y[(y+r)*pic.ys+x:])
	}
	for r := 0; r < h/2; r++ {
		o := (y/2+r)*pic.cs + x/2
		copy(sn.pcb[r*n/2:r*n/2+w/2], pic.cb[o:])
		copy(sn.pcr[r*n/2:r*n/2+w/2], pic.cr[o:])
	}
	fc := e.d.fc
	n4, w4, h4 := n/4, (w+3)/4, (h+3)/4
	for r := 0; r < h4; r++ {
		i := (y/4+r)*fc.w4 + x/4
		copy(sn.blk[r*n4:r*n4+w4], fc.blk[i:i+w4])
		j := (y/4+r)*pic.w4 + x/4
		for l := 0; l < 2; l++ {
			copy(sn.mv[l][r*n4:r*n4+w4], pic.mv[l][j:j+w4])
			copy(sn.refIdx[l][r*n4:r*n4+w4], pic.refIdx[l][j:j+w4])
			copy(sn.refPOC[l][r*n4:r*n4+w4], pic.refPOC[l][j:j+w4])
			copy(sn.refLT[l][r*n4:r*n4+w4], pic.refLT[l][j:j+w4])
		}
		copy(sn.intra[r*n4:r*n4+w4], pic.intra[j:j+w4])
	}
	sn.est = e.est
}

// load restores a snapshot.
func (e *Encoder) load(sn *encSnap) {
	s := &e.d.sd
	sp := s.sps
	pic := s.pic
	x, y, n := sn.x, sn.y, sn.n
	w, h := min(n, sp.Width-x), min(n, sp.Height-y)
	for r := 0; r < h; r++ {
		copy(pic.y[(y+r)*pic.ys+x:(y+r)*pic.ys+x+w], sn.py[r*n:])
	}
	for r := 0; r < h/2; r++ {
		o := (y/2+r)*pic.cs + x/2
		copy(pic.cb[o:o+w/2], sn.pcb[r*n/2:])
		copy(pic.cr[o:o+w/2], sn.pcr[r*n/2:])
	}
	fc := e.d.fc
	n4, w4, h4 := n/4, (w+3)/4, (h+3)/4
	for r := 0; r < h4; r++ {
		i := (y/4+r)*fc.w4 + x/4
		copy(fc.blk[i:i+w4], sn.blk[r*n4:])
		j := (y/4+r)*pic.w4 + x/4
		for l := 0; l < 2; l++ {
			copy(pic.mv[l][j:j+w4], sn.mv[l][r*n4:])
			copy(pic.refIdx[l][j:j+w4], sn.refIdx[l][r*n4:])
			copy(pic.refPOC[l][j:j+w4], sn.refPOC[l][r*n4:])
			copy(pic.refLT[l][j:j+w4], sn.refLT[l][r*n4:])
		}
		copy(pic.intra[j:j+w4], sn.intra[r*n4:])
	}
	e.est = sn.est
}

// ssd is the sum of squared differences of two w x h blocks.
func ssd(a []byte, as int, b []byte, bs int, w, h int) int64 {
	var s int64
	for y := 0; y < h; y++ {
		ra, rb := a[y*as:y*as+w], b[y*bs:y*bs+w]
		for x, v := range ra {
			d := int64(v) - int64(rb[x])
			s += d * d
		}
	}
	return s
}

// satd is the sum of absolute 4x4-Hadamard-transformed differences of
// two n x n blocks, halved.
func satd(a []byte, as int, b []byte, bs int, n int) int { return satdWH(a, as, b, bs, n, n) }

// satdWH is satd for a w x h block (multiples of 4).
func satdWH(a []byte, as int, b []byte, bs int, w, h int) int {
	raw := 0
	for x := 0; x < w; {
		sw := 16
		switch {
		case w-x >= 16:
		case w-x >= 8:
			sw = 8
		default:
			sw = 4
		}
		if haveNEON {
			var out int
			neonSATD(&a[x], as, &b[x], bs, sw, h, &out)
			raw += out
		} else {
			for y := 0; y < h; y += 4 {
				for xx := x; xx < x+sw; xx += 4 {
					raw += satd4Raw(a[y*as+xx:], as, b[y*bs+xx:], bs)
				}
			}
		}
		x += sw
	}
	return (raw + 1) >> 1
}

func satd4Raw(a []byte, as int, b []byte, bs int) int {
	var d [16]int
	for y := 0; y < 4; y++ {
		for x := 0; x < 4; x++ {
			d[y*4+x] = int(a[y*as+x]) - int(b[y*bs+x])
		}
	}
	for y := 0; y < 4; y++ {
		r := d[y*4 : y*4+4]
		s0, s1, d0, d1 := r[0]+r[1], r[2]+r[3], r[0]-r[1], r[2]-r[3]
		r[0], r[1], r[2], r[3] = s0+s1, s0-s1, d0+d1, d0-d1
	}
	sum := 0
	for x := 0; x < 4; x++ {
		s0, s1 := d[x]+d[4+x], d[8+x]+d[12+x]
		d0, d1 := d[x]-d[4+x], d[8+x]-d[12+x]
		sum += iabs(s0+s1) + iabs(s0-s1) + iabs(d0+d1) + iabs(d0-d1)
	}
	return sum
}

// sad is the sum of absolute differences of two w x h blocks (w a
// multiple of 4).
func sad(a []byte, as int, b []byte, bs int, w, h int) int {
	s := 0
	if haveNEON && w&7 == 0 {
		for y := 0; y < h; y += 16 {
			hh := min(16, h-y)
			for x := 0; x < w; {
				sw := 16
				if w-x < 16 {
					sw = 8
				}
				var out int
				neonSAD(&a[y*as+x], as, &b[y*bs+x], bs, sw, hh, &out)
				s += out
				x += sw
			}
		}
		return s
	}
	for y := 0; y < h; y++ {
		ra, rb := a[y*as:y*as+w], b[y*bs:y*bs+w]
		for x, v := range ra {
			s += iabs(int(v) - int(rb[x]))
		}
	}
	return s
}
