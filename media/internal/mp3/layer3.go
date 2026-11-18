package mp3

// Layer III decoding (ISO/IEC 11172-3 2.4.3.4, ISO/IEC 13818-3 2.4.3.4).
// The structure deliberately follows FFmpeg's mpegaudiodec so that edge
// cases (region boundaries, count1 overrun, intensity stereo limits, mixed
// blocks) resolve the same way and the float output matches mp3float.

const (
	modeExtIS = 1
	modeExtMS = 2
)

type granule struct {
	part23      int
	bigValues   int
	globalGain  int
	sfCompress  int
	blockType   int
	switchPoint bool
	tableSelect [3]int
	subGain     [3]int
	regionSize  [3]int // in pairs of lines
	preflag     int
	sfScale     int
	count1Table int
	scfsi       int
	longEnd     int
	shortStart  int
	sf          [40]int
	xr          [576]float64
}

// layer3 decodes one Layer III frame into d.pcm. skipped is true when the
// bit reservoir did not hold the frame's main data (start of stream or
// after Reset); the decoder state is still advanced.
func (d *Decoder) layer3(h Header, frame []byte) (skipped bool, err error) {
	nch := h.Channels()
	ngr := 2
	if h.LSF() {
		ngr = 1
	}
	off := 4
	if h.CRC {
		off += 2
	}
	sis := h.sideInfoSize()
	size := h.FrameSize()
	if off+sis > size {
		return false, errCorrupt
	}
	modeExt := 0
	if h.Mode == ModeJointStereo {
		modeExt = h.ModeExt
	}

	var br bitReader
	br.init(frame[off : off+sis])
	var mdb int
	if h.LSF() {
		mdb = br.read(8)
		br.read(nch) // private bits
	} else {
		mdb = br.read(9)
		if nch == 1 {
			br.read(5)
		} else {
			br.read(3)
		}
		for ch := 0; ch < nch; ch++ {
			d.gr[ch][0].scfsi = 0
			d.gr[ch][1].scfsi = br.read(4)
		}
	}
	for gr := 0; gr < ngr; gr++ {
		for ch := 0; ch < nch; ch++ {
			g := &d.gr[ch][gr]
			if err := readGranuleInfo(&br, g, h, modeExt); err != nil {
				return false, err
			}
		}
	}

	mainData := frame[off+sis : size]
	if mdb > len(d.res) {
		// Missing reservoir: keep the frame's bytes for the next frame and
		// advance the filterbank on silence so its state stays coherent.
		d.pushReservoir(mainData)
		for gr := 0; gr < ngr; gr++ {
			for ch := 0; ch < nch; ch++ {
				g := &d.gr[ch][gr]
				g.xr = [576]float64{}
				g.blockType, g.switchPoint = 0, false
				d.imdctSynth(g, ch, gr)
			}
		}
		return true, nil
	}
	buf := append(d.scratch[:0], d.res[len(d.res)-mdb:]...)
	buf = append(buf, mainData...)
	d.scratch = buf
	d.pushReservoir(mainData)

	br.init(buf)
	for gr := 0; gr < ngr; gr++ {
		for ch := 0; ch < nch; ch++ {
			g := &d.gr[ch][gr]
			start := br.pos
			end := start + g.part23
			if end > br.n {
				err = errCorrupt
				g.xr = [576]float64{}
				br.pos = end
				continue
			}
			if h.LSF() {
				d.lsfScaleFactors(&br, g, ch, modeExt)
			} else {
				d.scaleFactors(&br, g, ch, gr)
			}
			var exps [576]int
			d.exponents(g, &exps, h.srIndex)
			if e := d.huffman(&br, g, &exps, end); e != nil {
				err = e
			}
			br.pos = end
		}
		if nch == 2 {
			d.stereo(&d.gr[0][gr], &d.gr[1][gr], h, modeExt)
		}
		for ch := 0; ch < nch; ch++ {
			g := &d.gr[ch][gr]
			reorder(g, h.srIndex)
			antialias(g)
			d.imdctSynth(g, ch, gr)
		}
	}
	return false, err
}

func (d *Decoder) pushReservoir(b []byte) {
	const keep = 2048
	d.res = append(d.res, b...)
	if len(d.res) > keep {
		n := copy(d.res, d.res[len(d.res)-keep:])
		d.res = d.res[:n]
	}
}

func readGranuleInfo(br *bitReader, g *granule, h Header, modeExt int) error {
	g.part23 = br.read(12)
	g.bigValues = br.read(9)
	if g.bigValues > 288 {
		return errCorrupt
	}
	g.globalGain = br.read(8)
	// MS-only stereo folds the 1/sqrt(2) into the global gain.
	if modeExt&(modeExtMS|modeExtIS) == modeExtMS {
		g.globalGain -= 2
	}
	if h.LSF() {
		g.sfCompress = br.read(9)
	} else {
		g.sfCompress = br.read(4)
	}
	if br.read(1) == 1 { // window switching
		g.blockType = br.read(2)
		if g.blockType == 0 {
			return errCorrupt
		}
		g.switchPoint = br.read(1) == 1
		g.tableSelect[0] = br.read(5)
		g.tableSelect[1] = br.read(5)
		g.tableSelect[2] = 0
		for i := 0; i < 3; i++ {
			g.subGain[i] = br.read(3)
		}
		if g.blockType == 2 {
			if h.srIndex != 8 {
				g.regionSize[0] = 36 / 2
			} else {
				g.regionSize[0] = 72 / 2
			}
		} else {
			switch {
			case h.srIndex <= 2:
				g.regionSize[0] = 36 / 2
			case h.srIndex != 8:
				g.regionSize[0] = 54 / 2
			default:
				g.regionSize[0] = 108 / 2
			}
		}
		g.regionSize[1] = 576 / 2
	} else {
		g.blockType = 0
		g.switchPoint = false
		for i := 0; i < 3; i++ {
			g.tableSelect[i] = br.read(5)
		}
		g.subGain = [3]int{}
		ra1 := br.read(4)
		ra2 := br.read(3)
		g.regionSize[0] = bandIndexLong[h.srIndex][ra1+1] / 2
		l := ra1 + ra2 + 2
		if l > 22 {
			l = 22
		}
		g.regionSize[1] = bandIndexLong[h.srIndex][l] / 2
	}
	g.regionSize[2] = 576 / 2
	j := 0
	for i := 0; i < 3; i++ {
		k := g.regionSize[i]
		if k > g.bigValues {
			k = g.bigValues
		}
		g.regionSize[i] = k - j
		if g.regionSize[i] < 0 {
			g.regionSize[i] = 0
			k = j
		}
		j = k
	}
	if h.LSF() {
		g.preflag = 0
	} else {
		g.preflag = br.read(1)
	}
	g.sfScale = br.read(1)
	g.count1Table = br.read(1)

	if g.blockType == 2 {
		if g.switchPoint {
			if h.srIndex <= 2 {
				g.longEnd = 8
			} else {
				g.longEnd = 6
			}
			g.shortStart = 3
		} else {
			g.longEnd = 0
			g.shortStart = 0
		}
	} else {
		g.shortStart = 13
		g.longEnd = 22
	}
	return nil
}

func (d *Decoder) scaleFactors(br *bitReader, g *granule, ch, gr int) {
	slen1 := slenTable[0][g.sfCompress]
	slen2 := slenTable[1][g.sfCompress]
	j := 0
	if g.blockType == 2 {
		n := 18
		if g.switchPoint {
			n = 17
		}
		for i := 0; i < n; i++ {
			g.sf[j] = br.read(slen1)
			j++
		}
		for i := 0; i < 18; i++ {
			g.sf[j] = br.read(slen2)
			j++
		}
		for i := 0; i < 3; i++ {
			g.sf[j] = 0
			j++
		}
	} else {
		prev := &d.gr[ch][0].sf
		for k := 0; k < 4; k++ {
			n := 5
			if k == 0 {
				n = 6
			}
			if gr == 1 && g.scfsi&(8>>uint(k)) != 0 {
				for i := 0; i < n; i++ {
					g.sf[j] = prev[j]
					j++
				}
				continue
			}
			slen := slen2
			if k < 2 {
				slen = slen1
			}
			for i := 0; i < n; i++ {
				g.sf[j] = br.read(slen)
				j++
			}
		}
		g.sf[j] = 0
		j++
	}
	for ; j < 40; j++ {
		g.sf[j] = 0
	}
}

func (d *Decoder) lsfScaleFactors(br *bitReader, g *granule, ch, modeExt int) {
	var slen [4]int
	var tindex int
	expand := func(sf, n1, n2, n3 int) {
		if n3 != 0 {
			slen[3] = sf % n3
			sf /= n3
		} else {
			slen[3] = 0
		}
		if n2 != 0 {
			slen[2] = sf % n2
			sf /= n2
		} else {
			slen[2] = 0
		}
		slen[1] = sf % n1
		slen[0] = sf / n1
	}
	sf := g.sfCompress
	g.preflag = 0
	if modeExt&modeExtIS != 0 && ch == 1 {
		sf >>= 1
		switch {
		case sf < 180:
			expand(sf, 6, 6, 0)
			tindex = 3
		case sf < 244:
			expand(sf-180, 4, 4, 0)
			tindex = 4
		default:
			expand(sf-244, 3, 0, 0)
			tindex = 5
		}
	} else {
		switch {
		case sf < 400:
			expand(sf, 5, 4, 4)
			tindex = 0
		case sf < 500:
			expand(sf-400, 5, 4, 0)
			tindex = 1
		default:
			expand(sf-500, 3, 0, 0)
			tindex = 2
			g.preflag = 1
		}
	}
	kind := 0
	if g.blockType == 2 {
		kind = 1
		if g.switchPoint {
			kind = 2
		}
	}
	j := 0
	for k := 0; k < 4; k++ {
		n := lsfNsfTable[tindex][kind][k]
		for i := 0; i < n && j < 40; i++ {
			g.sf[j] = br.read(slen[k])
			j++
		}
	}
	for ; j < 40; j++ {
		g.sf[j] = 0
	}
}

// exponents computes, per spectral line, the quarter-power-of-two gain
// offset by 400 (index into expTable).
func (d *Decoder) exponents(g *granule, exps *[576]int, sr int) {
	gain := g.globalGain - 210
	shift := uint(g.sfScale + 1)
	p := 0
	for i := 0; i < g.longEnd; i++ {
		pt := 0
		if g.preflag != 0 {
			pt = pretab[i]
		}
		v := gain - (g.sf[i]+pt)<<shift + 400
		for j := bandSizeLong[sr][i]; j > 0 && p < 576; j-- {
			exps[p] = v
			p++
		}
	}
	if g.shortStart < 13 {
		var gains [3]int
		for l := 0; l < 3; l++ {
			gains[l] = gain - g.subGain[l]<<3
		}
		k := g.longEnd
		for i := g.shortStart; i < 13; i++ {
			n := bandSizeShort[sr][i]
			for l := 0; l < 3; l++ {
				v := gains[l] - g.sf[k]<<shift + 400
				k++
				for j := n; j > 0 && p < 576; j-- {
					exps[p] = v
					p++
				}
			}
		}
	}
	for i := 0; i < 576; i++ {
		if exps[i] < 0 {
			exps[i] = 0
		} else if exps[i] > 511 {
			exps[i] = 511
		}
	}
}

func (d *Decoder) huffman(br *bitReader, g *granule, exps *[576]int, end int) error {
	xr := &g.xr
	s := 0
	for i := 0; i < 3; i++ {
		j := g.regionSize[i]
		if j == 0 {
			continue
		}
		sel := huffSelect[g.tableSelect[i]]
		if sel.tab == 0 {
			for ; j > 0 && s < 576; j-- {
				xr[s], xr[s+1] = 0, 0
				s += 2
			}
			continue
		}
		t := huffTrees[sel.tab]
		for ; j > 0; j-- {
			if br.pos >= end {
				break
			}
			sym := t.decode(br)
			if sym < 0 {
				clearFrom(xr, s)
				return errCorrupt
			}
			x, y := sym/t.xlen, sym%t.xlen
			e := expTable[exps[s]]
			xr[s] = d.bigValue(br, x, sel.linbits, e)
			xr[s+1] = d.bigValue(br, y, sel.linbits, e)
			s += 2
		}
	}

	t := huffTrees[32+g.count1Table]
	lastPos := 0
	for s <= 572 {
		pos := br.pos
		if pos >= end {
			if pos > end && lastPos != 0 {
				// The last quadruple overran part2_3_length: drop it.
				s -= 4
				br.pos = lastPos
			}
			break
		}
		lastPos = pos
		sym := t.decode(br)
		if sym < 0 {
			clearFrom(xr, s)
			return errCorrupt
		}
		for k := 0; k < 4; k++ {
			if sym&(8>>uint(k)) != 0 {
				v := expTable[exps[s+k]]
				if br.bit() == 1 {
					v = -v
				}
				xr[s+k] = v
			} else {
				xr[s+k] = 0
			}
		}
		s += 4
	}
	clearFrom(xr, s)
	return nil
}

func clearFrom(xr *[576]float64, s int) {
	for i := s; i < 576; i++ {
		xr[i] = 0
	}
}

func (d *Decoder) bigValue(br *bitReader, x, linbits int, e float64) float64 {
	if x == 0 {
		return 0
	}
	if x == 15 && linbits > 0 {
		x += br.read(linbits)
	}
	v := pow43Table[x] * e
	if br.bit() == 1 {
		v = -v
	}
	return v
}

// stereo applies intensity and M/S stereo to one granule pair (FFmpeg's
// compute_stereo; the spectrum is still in bitstream order here).
func (d *Decoder) stereo(g0, g1 *granule, h Header, modeExt int) {
	sr := h.srIndex
	if modeExt&modeExtIS != 0 {
		var isTab *[2][7]float64
		var isLSF *[2][16]float64
		sfMax := 7
		if h.LSF() {
			isLSF = &isTableLSF[g1.sfCompress&1]
			sfMax = 16
		} else {
			isTab = &isTable
		}
		ratio := func(sf int) (float64, float64) {
			if isLSF != nil {
				return isLSF[0][sf], isLSF[1][sf]
			}
			return isTab[0][sf], isTab[1][sf]
		}
		ms := modeExt&modeExtMS != 0
		apply := func(p, n, sf int, nonZero *bool) {
			if !*nonZero {
				for j := 0; j < n; j++ {
					if g1.xr[p+j] != 0 {
						*nonZero = true
						break
					}
				}
			}
			if !*nonZero && sf < sfMax {
				v1, v2 := ratio(sf)
				for j := 0; j < n; j++ {
					t := g0.xr[p+j]
					g0.xr[p+j] = t * v1
					g1.xr[p+j] = t * v2
				}
				return
			}
			if ms {
				for j := 0; j < n; j++ {
					a, b := g0.xr[p+j], g1.xr[p+j]
					g0.xr[p+j] = (a + b) * sqrtHalf
					g1.xr[p+j] = (a - b) * sqrtHalf
				}
			}
		}
		p := 576
		var nzShort [3]bool
		k := (13-g1.shortStart)*3 + g1.longEnd - 3
		for i := 12; i >= g1.shortStart; i-- {
			if i != 11 {
				k -= 3
			}
			n := bandSizeShort[sr][i]
			for l := 2; l >= 0; l-- {
				p -= n
				if p < 0 {
					return
				}
				sf := g1.sf[k+l]
				if nzShort[l] {
					sf = sfMax
				}
				apply(p, n, sf, &nzShort[l])
			}
		}
		nz := nzShort[0] || nzShort[1] || nzShort[2]
		for i := g1.longEnd - 1; i >= 0; i-- {
			n := bandSizeLong[sr][i]
			p -= n
			if p < 0 {
				return
			}
			kk := i
			if i == 21 {
				kk = 20
			}
			apply(p, n, g1.sf[kk], &nz)
		}
	} else if modeExt&modeExtMS != 0 {
		for i := 0; i < 576; i++ {
			a, b := g0.xr[i], g1.xr[i]
			g0.xr[i] = a + b
			g1.xr[i] = a - b
		}
	}
}

const sqrtHalf = 0.70710678118654752440

// reorder interleaves short-block lines so that each frequency line holds
// its three windows consecutively.
func reorder(g *granule, sr int) {
	if g.blockType != 2 {
		return
	}
	p := 0
	if g.switchPoint {
		if sr != 8 {
			p = 36
		} else {
			p = 72
		}
	}
	var tmp [576]float64
	for i := g.shortStart; i < 13; i++ {
		n := bandSizeShort[sr][i]
		if p+3*n > 576 {
			return
		}
		t := 0
		for j := 0; j < n; j++ {
			tmp[t] = g.xr[p+j]
			tmp[t+1] = g.xr[p+n+j]
			tmp[t+2] = g.xr[p+2*n+j]
			t += 3
		}
		copy(g.xr[p:p+3*n], tmp[:3*n])
		p += 3 * n
	}
}

func antialias(g *granule) {
	n := 31
	if g.blockType == 2 {
		if !g.switchPoint {
			return
		}
		n = 1
	}
	for sb := 1; sb <= n; sb++ {
		p := sb * 18
		for i := 0; i < 8; i++ {
			a, b := g.xr[p-1-i], g.xr[p+i]
			g.xr[p-1-i] = a*csTable[i] - b*caTable[i]
			g.xr[p+i] = a*caTable[i] + b*csTable[i]
		}
	}
}

// imdctSynth runs the hybrid filterbank IMDCT with overlap-add and then
// the polyphase synthesis for one granule of one channel.
func (d *Decoder) imdctSynth(g *granule, ch, gr int) {
	longEnd := 32
	if g.blockType == 2 {
		longEnd = 0
		if g.switchPoint {
			longEnd = 2
		}
	}
	var sb [18][32]float64
	for s := 0; s < 32; s++ {
		in := g.xr[s*18 : s*18+18]
		prev := &d.overlap[ch][s]
		zero := true
		for _, v := range in {
			if v != 0 {
				zero = false
				break
			}
		}
		var z [36]float64
		if !zero {
			if s < longEnd {
				bt := g.blockType
				if g.switchPoint && s < 2 {
					bt = 0
				}
				win := &imdctWin[bt]
				for k := 0; k < 18; k++ {
					x := in[k]
					if x == 0 {
						continue
					}
					c := &cos36[k]
					for i := 0; i < 36; i++ {
						z[i] += x * c[i]
					}
				}
				for i := 0; i < 36; i++ {
					z[i] *= win[i]
				}
			} else {
				win := &imdctWin[2]
				for w := 0; w < 3; w++ {
					var y [12]float64
					for k := 0; k < 6; k++ {
						x := in[3*k+w]
						if x == 0 {
							continue
						}
						c := &cos12[k]
						for i := 0; i < 12; i++ {
							y[i] += x * c[i]
						}
					}
					for i := 0; i < 12; i++ {
						z[6+6*w+i] += y[i] * win[i]
					}
				}
			}
		}
		for i := 0; i < 18; i++ {
			v := z[i] + prev[i]
			if s&1 == 1 && i&1 == 1 {
				v = -v
			}
			sb[i][s] = v
			prev[i] = z[18+i]
		}
	}
	base := gr * 576
	for i := 0; i < 18; i++ {
		d.synth[ch].run(&sb[i], d.pcm[ch][base+i*32:base+i*32+32])
	}
}
