package mp3

// Layer I and Layer II decoding (ISO/IEC 11172-3 2.4.3.2, 2.4.3.3 and the
// LSF extension of ISO/IEC 13818-3).

type allocBand struct {
	nbal   int
	quants []int8 // quantisation class per allocation value 1..2^nbal-1
}

type allocTable struct {
	sblimit int
	bands   []allocBand
}

var quantSteps = [17]int{3, 5, 7, 9, 15, 31, 63, 127, 255, 511, 1023, 2047, 4095, 8191, 16383, 32767, 65535}

// quantBits: codeword bits per sample; negative = grouped codeword of
// three samples with that many bits.
var quantBits = [17]int{-5, -7, 3, -10, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16}

var layer2Tables [5]allocTable

func init() {
	var (
		a4 = []int8{0, 2, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16}
		b4 = []int8{0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 16}
		c3 = []int8{0, 1, 2, 3, 4, 5, 16}
		d2 = []int8{0, 1, 16}
		e4 = []int8{0, 1, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15}
		f3 = []int8{0, 1, 3, 4, 5, 6, 7}
		g4 = []int8{0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14}
		h2 = []int8{0, 1, 3}
	)
	build := func(sblimit int, f func(sb int) allocBand) allocTable {
		t := allocTable{sblimit: sblimit}
		for sb := 0; sb < sblimit; sb++ {
			t.bands = append(t.bands, f(sb))
		}
		return t
	}
	high := func(sb int) allocBand { // Tables B.2a / B.2b
		switch {
		case sb < 3:
			return allocBand{4, a4}
		case sb < 11:
			return allocBand{4, b4}
		case sb < 23:
			return allocBand{3, c3}
		}
		return allocBand{2, d2}
	}
	low := func(sb int) allocBand { // Tables B.2c / B.2d
		if sb < 2 {
			return allocBand{4, e4}
		}
		return allocBand{3, f3}
	}
	layer2Tables[0] = build(27, high)
	layer2Tables[1] = build(30, high)
	layer2Tables[2] = build(8, low)
	layer2Tables[3] = build(12, low)
	layer2Tables[4] = build(30, func(sb int) allocBand { // ISO/IEC 13818-3 Table B.1
		switch {
		case sb < 4:
			return allocBand{4, g4}
		case sb < 11:
			return allocBand{3, f3}
		}
		return allocBand{2, h2}
	})
}

func layer2Table(h Header) int {
	if h.LSF() {
		return 4
	}
	chBitrate := h.Bitrate / 1000 / h.Channels()
	switch {
	case (h.SampleRate == 48000 && chBitrate >= 56) || (chBitrate >= 56 && chBitrate <= 80):
		return 0
	case h.SampleRate != 48000 && chBitrate >= 96:
		return 1
	case h.SampleRate != 32000 && chBitrate <= 48:
		return 2
	}
	return 3
}

// dequant maps a sample code of a quantiser with the given number of steps
// to [-1, 1): s”' = (2c - (steps-1)) / steps.
func dequant(c, steps int) float64 {
	return float64(2*c-(steps-1)) / float64(steps)
}

func (d *Decoder) layer1(h Header, frame []byte) error {
	nch := h.Channels()
	off := 4
	if h.CRC {
		off += 2
	}
	var br bitReader
	br.init(frame[off:h.FrameSize()])
	bound := 32
	if h.Mode == ModeJointStereo {
		bound = (h.ModeExt + 1) * 4
	}
	var alloc [2][32]int
	var scale [2][32]float64
	for sb := 0; sb < 32; sb++ {
		if sb < bound {
			for ch := 0; ch < nch; ch++ {
				alloc[ch][sb] = br.read(4)
			}
		} else {
			a := br.read(4)
			alloc[0][sb], alloc[1][sb] = a, a
		}
		if alloc[0][sb] == 15 || alloc[1][sb] == 15 {
			return errCorrupt
		}
	}
	for sb := 0; sb < 32; sb++ {
		for ch := 0; ch < nch; ch++ {
			if alloc[ch][sb] != 0 {
				scale[ch][sb] = l12Scale[br.read(6)]
			}
		}
	}
	var sbs [2][32]float64
	for s := 0; s < 12; s++ {
		for sb := 0; sb < 32; sb++ {
			if sb < bound {
				for ch := 0; ch < nch; ch++ {
					sbs[ch][sb] = 0
					if a := alloc[ch][sb]; a != 0 {
						nb := a + 1
						sbs[ch][sb] = dequant(br.read(nb), 1<<uint(nb)-1) * scale[ch][sb]
					}
				}
			} else {
				sbs[0][sb], sbs[1][sb] = 0, 0
				if a := alloc[0][sb]; a != 0 {
					nb := a + 1
					v := dequant(br.read(nb), 1<<uint(nb)-1)
					sbs[0][sb] = v * scale[0][sb]
					sbs[1][sb] = v * scale[1][sb]
				}
			}
		}
		for ch := 0; ch < nch; ch++ {
			d.synth[ch].run(&sbs[ch], d.pcm[ch][s*32:s*32+32])
		}
	}
	if br.overrun() {
		return errCorrupt
	}
	return nil
}

func (d *Decoder) layer2(h Header, frame []byte) error {
	nch := h.Channels()
	off := 4
	if h.CRC {
		off += 2
	}
	var br bitReader
	br.init(frame[off:h.FrameSize()])
	tab := &layer2Tables[layer2Table(h)]
	sblimit := tab.sblimit
	bound := sblimit
	if h.Mode == ModeJointStereo {
		bound = (h.ModeExt + 1) * 4
		if bound > sblimit {
			bound = sblimit
		}
	}
	var alloc [2][32]int // quant class + 1, 0 = none
	for sb := 0; sb < sblimit; sb++ {
		band := &tab.bands[sb]
		n := 1
		if sb < bound {
			n = nch
		}
		for ch := 0; ch < n; ch++ {
			a := br.read(band.nbal)
			if a != 0 {
				alloc[ch][sb] = int(band.quants[a-1]) + 1
			}
		}
		if sb >= bound {
			alloc[1][sb] = alloc[0][sb]
		}
	}
	var scfsi [2][32]int
	for sb := 0; sb < sblimit; sb++ {
		for ch := 0; ch < nch; ch++ {
			if alloc[ch][sb] != 0 {
				scfsi[ch][sb] = br.read(2)
			}
		}
	}
	var scale [2][32][3]float64
	for sb := 0; sb < sblimit; sb++ {
		for ch := 0; ch < nch; ch++ {
			if alloc[ch][sb] == 0 {
				continue
			}
			sc := &scale[ch][sb]
			switch scfsi[ch][sb] {
			case 0:
				sc[0] = l12Scale[br.read(6)]
				sc[1] = l12Scale[br.read(6)]
				sc[2] = l12Scale[br.read(6)]
			case 1:
				sc[0] = l12Scale[br.read(6)]
				sc[1] = sc[0]
				sc[2] = l12Scale[br.read(6)]
			case 2:
				sc[0] = l12Scale[br.read(6)]
				sc[1], sc[2] = sc[0], sc[0]
			case 3:
				sc[0] = l12Scale[br.read(6)]
				sc[1] = l12Scale[br.read(6)]
				sc[2] = sc[1]
			}
		}
	}
	var sbs [2][3][32]float64
	readTriple := func(q int, out *[3]float64) {
		steps := quantSteps[q]
		bits := quantBits[q]
		if bits < 0 {
			c := br.read(-bits)
			for i := 0; i < 3; i++ {
				out[i] = dequant(c%steps, steps)
				c /= steps
			}
			return
		}
		for i := 0; i < 3; i++ {
			out[i] = dequant(br.read(bits), steps)
		}
	}
	for gr := 0; gr < 12; gr++ {
		part := gr >> 2
		for sb := 0; sb < 32; sb++ {
			for ch := 0; ch < 2; ch++ {
				for i := 0; i < 3; i++ {
					sbs[ch][i][sb] = 0
				}
			}
			if sb >= sblimit {
				continue
			}
			if sb < bound {
				for ch := 0; ch < nch; ch++ {
					if a := alloc[ch][sb]; a != 0 {
						var v [3]float64
						readTriple(a-1, &v)
						for i := 0; i < 3; i++ {
							sbs[ch][i][sb] = v[i] * scale[ch][sb][part]
						}
					}
				}
			} else if a := alloc[0][sb]; a != 0 {
				var v [3]float64
				readTriple(a-1, &v)
				for ch := 0; ch < nch; ch++ {
					for i := 0; i < 3; i++ {
						sbs[ch][i][sb] = v[i] * scale[ch][sb][part]
					}
				}
			}
		}
		for ch := 0; ch < nch; ch++ {
			for i := 0; i < 3; i++ {
				p := (gr*3 + i) * 32
				d.synth[ch].run(&sbs[ch][i], d.pcm[ch][p:p+32])
			}
		}
	}
	if br.overrun() {
		return errCorrupt
	}
	return nil
}
