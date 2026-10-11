package aac

import (
	"errors"
	"math"
)

// AAC-LC raw_data_block decoding (ISO/IEC 14496-3 4.4.2, 4.5, 4.6):
// syntax, Huffman spectral data, inverse quantisation, M/S and intensity
// stereo, perceptual noise substitution, TNS and the IMDCT filterbank.

// Window sequences.
const (
	onlyLong   = 0
	longStart  = 1
	eightShort = 2
	longStop   = 3
)

// Section codebooks with special meaning.
const (
	zeroHCB       = 0
	escHCB        = 11
	noiseHCB      = 13
	intensityHCB2 = 14
	intensityHCB  = 15
)

// Syntactic element ids.
const (
	idSCE = 0
	idCPE = 1
	idCCE = 2
	idLFE = 3
	idDSE = 4
	idPCE = 5
	idFIL = 6
	idEND = 7
)

var (
	ErrUnsupported = errors.New("aac: unsupported stream feature")
	errCorrupt     = errors.New("aac: corrupt access unit")
)

type icsInfo struct {
	windowSequence int
	windowShape    int
	maxSfb         int
	numWindows     int
	numGroups      int
	groupLen       [8]int
	swb            []int // band offsets for one window
	numSwb         int
}

type tnsFilter struct {
	length, order int
	direction     bool
	lpc           [21]float64
}

type ics struct {
	info       icsInfo
	globalGain int
	sfbCb      [8][64]uint8
	sf         [8][64]int // scalefactor / intensity position / noise energy
	tnsPresent bool
	nFilt      [8]int
	tns        [8][4]tnsFilter
	pulse      bool
	numPulse   int
	pulseStart int
	pulseOff   [4]int
	pulseAmp   [4]int
	quant      [1024]int32
	spec       [1024]float64 // window-ordered spectral coefficients
}

type chanState struct {
	overlap   [1024]float64
	prevShape int
}

// Decoder decodes AAC-LC access units (raw_data_block, as stored in MP4).
type Decoder struct {
	Config      *Config
	sfIdx       int
	outChannels int

	b     bits
	ics   [2]ics
	ch    [8]chanState
	long  *fft
	short *fft
	rng   uint32

	time [2048]float64
	tmp  [256]float64
	pcm  [2][1024]float64
}

// NewDecoder returns a decoder for the AudioSpecificConfig asc.
func NewDecoder(cfg *Config) (*Decoder, error) {
	if cfg.ObjectType != ObjectLC && cfg.ObjectType != ObjectSBR && cfg.ObjectType != ObjectPS {
		return nil, ErrUnsupported
	}
	if cfg.SampleRateIndex < 0 || cfg.SampleRateIndex > 12 || cfg.FrameLength == 960 {
		return nil, ErrUnsupported
	}
	d := &Decoder{Config: cfg, sfIdx: cfg.SampleRateIndex, long: newDCT4(1024), short: newDCT4(128), rng: 0x1f2e3d4c}
	d.outChannels = cfg.Channels
	if d.outChannels <= 0 || d.outChannels > 2 {
		d.outChannels = 2
	}
	return d, nil
}

// Channels is the number of output channels (1 or 2).
func (d *Decoder) Channels() int { return d.outChannels }

// SampleRate is the output sample rate (the AAC-LC core rate).
func (d *Decoder) SampleRate() int { return sampleRates[d.sfIdx] }

// Decode decodes one access unit and appends 1024 interleaved float32
// frames (range about ±1) to out.
func (d *Decoder) Decode(au []byte, out []float32) ([]float32, error) {
	err := d.decodeRaw(au)
	n := d.outChannels
	for i := 0; i < 1024; i++ {
		for c := 0; c < n; c++ {
			out = append(out, float32(d.pcm[c][i]/32768))
		}
	}
	return out, err
}

// DecodeInt16 is Decode producing interleaved signed 16-bit samples.
func (d *Decoder) DecodeInt16(au []byte, out []int16) ([]int16, error) {
	err := d.decodeRaw(au)
	n := d.outChannels
	for i := 0; i < 1024; i++ {
		for c := 0; c < n; c++ {
			v := math.Round(d.pcm[c][i])
			if v > 32767 {
				v = 32767
			} else if v < -32768 {
				v = -32768
			}
			out = append(out, int16(v))
		}
	}
	return out, err
}

func (d *Decoder) decodeRaw(au []byte) (err error) {
	defer func() {
		// A malformed access unit must never take the host process down.
		if r := recover(); r != nil {
			d.silence()
			err = errCorrupt
		}
	}()
	b := &d.b
	b.init(au)
	chIdx := 0
	gotAudio := false
	for {
		if b.left() < 3 {
			break
		}
		id := b.read(3)
		if id == idEND {
			break
		}
		switch id {
		case idSCE, idLFE:
			b.read(4)
			if err := d.decodeICS(&d.ics[0], false); err != nil {
				d.silence()
				return err
			}
			if chIdx < 2 {
				d.finishChannel(&d.ics[0], chIdx)
			}
			chIdx++
			gotAudio = true
		case idCPE:
			b.read(4)
			if err := d.decodeCPE(chIdx); err != nil {
				d.silence()
				return err
			}
			chIdx += 2
			gotAudio = true
		case idCCE:
			d.silence()
			return ErrUnsupported
		case idDSE:
			b.read(4)
			align := b.bit()
			count := b.read(8)
			if count == 255 {
				count += b.read(8)
			}
			if align {
				b.align()
			}
			b.skip(8 * count)
		case idPCE:
			if err := skipPCE(b); err != nil {
				d.silence()
				return err
			}
		case idFIL:
			count := b.read(4)
			if count == 15 {
				count += b.read(8) - 1
			}
			b.skip(8 * count)
		}
		if b.over {
			d.silence()
			return errCorrupt
		}
	}
	if !gotAudio {
		d.silence()
		return nil
	}
	// A mono stream played as stereo (channel config 0 without a PCE).
	if d.outChannels == 2 && chIdx == 1 {
		d.pcm[1] = d.pcm[0]
	}
	return nil
}

func (d *Decoder) silence() {
	d.pcm[0] = [1024]float64{}
	d.pcm[1] = [1024]float64{}
}

func skipPCE(b *bits) error {
	b.read(4) // element_instance_tag
	b.read(2) // object_type
	b.read(4) // sampling_frequency_index
	front := b.read(4)
	side := b.read(4)
	back := b.read(4)
	lfe := b.read(2)
	assoc := b.read(3)
	cc := b.read(4)
	if b.bit() {
		b.read(4)
	}
	if b.bit() {
		b.read(4)
	}
	if b.bit() {
		b.read(3)
	}
	b.skip(5 * (front + side + back))
	b.skip(4*lfe + 4*assoc + 5*cc)
	b.align()
	n := b.read(8)
	b.skip(8 * n)
	if b.over {
		return errCorrupt
	}
	return nil
}

func (d *Decoder) decodeICSInfo(info *icsInfo) error {
	b := &d.b
	b.read(1) // ics_reserved_bit
	info.windowSequence = b.read(2)
	info.windowShape = b.read(1)
	if info.windowSequence == eightShort {
		info.maxSfb = b.read(4)
		grouping := b.read(7)
		info.numWindows = 8
		info.numGroups = 1
		info.groupLen = [8]int{1}
		for i := 0; i < 7; i++ {
			if grouping&(1<<uint(6-i)) != 0 {
				info.groupLen[info.numGroups-1]++
			} else {
				info.numGroups++
				info.groupLen[info.numGroups-1] = 1
			}
		}
		info.swb = swbShort[d.sfIdx]
	} else {
		info.maxSfb = b.read(6)
		info.numWindows = 1
		info.numGroups = 1
		info.groupLen = [8]int{1}
		if b.bit() { // predictor_data_present (AAC Main only)
			return ErrUnsupported
		}
		info.swb = swbLong[d.sfIdx]
	}
	info.numSwb = len(info.swb) - 1
	if info.maxSfb > info.numSwb {
		return errCorrupt
	}
	return nil
}

func (d *Decoder) decodeCPE(chIdx int) error {
	b := &d.b
	l, r := &d.ics[0], &d.ics[1]
	common := b.bit()
	var msMask int
	var msUsed [8][64]bool
	if common {
		if err := d.decodeICSInfo(&l.info); err != nil {
			return err
		}
		r.info = l.info
		msMask = b.read(2)
		if msMask == 2 { // all bands use M/S
			for g := range msUsed {
				for sfb := range msUsed[g] {
					msUsed[g][sfb] = true
				}
			}
		}
		if msMask == 1 {
			for g := 0; g < l.info.numGroups; g++ {
				for sfb := 0; sfb < l.info.maxSfb; sfb++ {
					msUsed[g][sfb] = b.bit()
				}
			}
		}
	}
	if err := d.decodeICS(l, common); err != nil {
		return err
	}
	if err := d.decodeICS(r, common); err != nil {
		return err
	}
	if common {
		d.stereo(l, r, msMask, &msUsed)
	}
	if chIdx < 2 {
		d.finishChannel(l, chIdx)
	}
	if chIdx+1 < 2 {
		d.finishChannel(r, chIdx+1)
	}
	return nil
}

// decodeICS parses individual_channel_stream and dequantises the spectrum
// into s.spec (window order).
func (d *Decoder) decodeICS(s *ics, common bool) error {
	b := &d.b
	s.globalGain = b.read(8)
	if !common {
		if err := d.decodeICSInfo(&s.info); err != nil {
			return err
		}
	}
	info := &s.info
	if err := d.sectionData(s); err != nil {
		return err
	}
	if err := d.scaleFactors(s); err != nil {
		return err
	}
	s.pulse = b.bit()
	if s.pulse {
		if info.windowSequence == eightShort {
			return errCorrupt
		}
		s.numPulse = b.read(2) + 1
		s.pulseStart = b.read(6)
		for i := 0; i < s.numPulse; i++ {
			s.pulseOff[i] = b.read(5)
			s.pulseAmp[i] = b.read(4)
		}
	}
	s.tnsPresent = b.bit()
	if s.tnsPresent {
		d.tnsData(s)
	}
	if b.bit() { // gain_control_data_present (AAC SSR only)
		return ErrUnsupported
	}
	if err := d.spectralData(s); err != nil {
		return err
	}
	d.dequant(s)
	return nil
}

func (d *Decoder) sectionData(s *ics) error {
	b := &d.b
	info := &s.info
	bitsLen, esc := 5, 31
	if info.windowSequence == eightShort {
		bitsLen, esc = 3, 7
	}
	for g := 0; g < info.numGroups; g++ {
		k := 0
		for k < info.maxSfb {
			cb := b.read(4)
			if cb == 12 {
				return errCorrupt
			}
			n := 0
			for {
				inc := b.read(bitsLen)
				n += inc
				if inc != esc {
					break
				}
				if b.over {
					return errCorrupt
				}
			}
			if k+n > info.maxSfb || b.over {
				return errCorrupt
			}
			for i := 0; i < n; i++ {
				s.sfbCb[g][k+i] = uint8(cb)
			}
			k += n
		}
		for k := info.maxSfb; k < 64; k++ {
			s.sfbCb[g][k] = zeroHCB
		}
	}
	return nil
}

func (d *Decoder) scaleFactors(s *ics) error {
	b := &d.b
	info := &s.info
	sf := s.globalGain
	isPos := 0
	noise := s.globalGain - 90
	noiseFirst := true
	for g := 0; g < info.numGroups; g++ {
		for sfb := 0; sfb < info.maxSfb; sfb++ {
			switch s.sfbCb[g][sfb] {
			case zeroHCB:
				s.sf[g][sfb] = 0
			case intensityHCB, intensityHCB2:
				v := sfLUT.decode(b)
				if v < 0 {
					return errCorrupt
				}
				isPos += v - 60
				s.sf[g][sfb] = isPos
			case noiseHCB:
				if noiseFirst {
					noise += b.read(9) - 256
					noiseFirst = false
				} else {
					v := sfLUT.decode(b)
					if v < 0 {
						return errCorrupt
					}
					noise += v - 60
				}
				s.sf[g][sfb] = noise
			default:
				v := sfLUT.decode(b)
				if v < 0 {
					return errCorrupt
				}
				sf += v - 60
				if sf < 0 || sf > 255 {
					return errCorrupt
				}
				s.sf[g][sfb] = sf
			}
		}
	}
	return nil
}

func (d *Decoder) tnsData(s *ics) {
	b := &d.b
	short := s.info.windowSequence == eightShort
	nBits, lBits, oBits, maxOrder := 2, 6, 5, 12
	if short {
		nBits, lBits, oBits, maxOrder = 1, 4, 3, 7
	}
	for w := 0; w < s.info.numWindows; w++ {
		s.nFilt[w] = b.read(nBits)
		if s.nFilt[w] == 0 {
			continue
		}
		coefRes := b.read(1)
		for f := 0; f < s.nFilt[w]; f++ {
			t := &s.tns[w][f]
			t.length = b.read(lBits)
			t.order = b.read(oBits)
			if t.order == 0 {
				continue
			}
			t.direction = b.bit()
			compress := b.read(1)
			resBits := coefRes + 3
			coefBits := resBits - compress
			iqfac := (float64(int(1)<<uint(resBits-1)) - 0.5) / (math.Pi / 2)
			iqfacM := (float64(int(1)<<uint(resBits-1)) + 0.5) / (math.Pi / 2)
			var par [32]float64
			for i := 0; i < t.order; i++ {
				c := b.read(coefBits)
				if c&(1<<uint(coefBits-1)) != 0 {
					c -= 1 << uint(coefBits)
				}
				if c >= 0 {
					par[i] = math.Sin(float64(c) / iqfac)
				} else {
					par[i] = math.Sin(float64(c) / iqfacM)
				}
			}
			if t.order > maxOrder {
				t.order = maxOrder
			}
			// Parcor to LPC (step-up recursion).
			var a, tmp [21]float64
			a[0] = 1
			for m := 1; m <= t.order; m++ {
				for i := 1; i < m; i++ {
					tmp[i] = a[i] + par[m-1]*a[m-i]
				}
				for i := 1; i < m; i++ {
					a[i] = tmp[i]
				}
				a[m] = par[m-1]
			}
			t.lpc = a
		}
	}
}

func (d *Decoder) spectralData(s *ics) error {
	b := &d.b
	info := &s.info
	q := &s.quant
	*q = [1024]int32{}
	// Coefficients are coded group by group, band by band, interleaving the
	// windows of a group; they are stored here window-ordered.
	winBase := 0
	for g := 0; g < info.numGroups; g++ {
		gl := info.groupLen[g]
		for sfb := 0; sfb < info.maxSfb; sfb++ {
			cb := int(s.sfbCb[g][sfb])
			start, end := info.swb[sfb], info.swb[sfb+1]
			if cb == zeroHCB || cb >= noiseHCB {
				continue
			}
			lut := &spectralLUT[cb]
			vals := spectralVals[cb]
			step := 2
			if quadCB[cb] {
				step = 4
			}
			for w := 0; w < gl; w++ {
				base := (winBase + w) * 128
				if info.windowSequence != eightShort {
					base = 0
				}
				for k := start; k < end; k += step {
					idx := lut.decode(b)
					if idx < 0 || idx >= len(vals) {
						return errCorrupt
					}
					v := vals[idx]
					for j := 0; j < step; j++ {
						x := int32(v[j])
						if unsignedCB[cb] && x != 0 && b.bit() {
							x = -x
						}
						q[base+k+j] = x
					}
					if cb == escHCB {
						for j := 0; j < 2; j++ {
							x := q[base+k+j]
							if x == 16 || x == -16 {
								n := 0
								for b.bit() {
									n++
									if n > 8 {
										return errCorrupt
									}
								}
								e := int32(1<<uint(n+4)) + int32(b.read(n+4))
								if x < 0 {
									e = -e
								}
								q[base+k+j] = e
							}
						}
					}
				}
				if b.over {
					return errCorrupt
				}
			}
		}
		winBase += gl
	}
	if s.pulse {
		n := s.numPulse
		start := s.pulseStart
		if start >= info.numSwb {
			return errCorrupt
		}
		k := info.swb[start]
		for i := 0; i < n; i++ {
			k += s.pulseOff[i]
			if k >= 1024 {
				return errCorrupt
			}
			if q[k] > 0 {
				q[k] += int32(s.pulseAmp[i])
			} else {
				q[k] -= int32(s.pulseAmp[i])
			}
		}
	}
	return nil
}

// pow43 holds |x|^(4/3) for the quantised range.
var pow43 [8192]float64

func init() {
	for i := range pow43 {
		pow43[i] = math.Pow(float64(i), 4.0/3.0)
	}
}

func iq(x int32) float64 {
	if x < 0 {
		if -x < 8192 {
			return -pow43[-x]
		}
		return -math.Pow(float64(-x), 4.0/3.0)
	}
	if x < 8192 {
		return pow43[x]
	}
	return math.Pow(float64(x), 4.0/3.0)
}

// dequant applies inverse quantisation and scalefactors, and fills noise
// bands (PNS); intensity bands stay zero until stereo processing.
func (d *Decoder) dequant(s *ics) {
	info := &s.info
	spec := &s.spec
	*spec = [1024]float64{}
	winBase := 0
	for g := 0; g < info.numGroups; g++ {
		for w := 0; w < info.groupLen[g]; w++ {
			base := (winBase + w) * 128
			if info.windowSequence != eightShort {
				base = 0
			}
			for sfb := 0; sfb < info.maxSfb; sfb++ {
				cb := s.sfbCb[g][sfb]
				start, end := base+info.swb[sfb], base+info.swb[sfb+1]
				switch {
				case cb == zeroHCB || cb == intensityHCB || cb == intensityHCB2:
				case cb == noiseHCB:
					energy := 0.0
					for k := start; k < end; k++ {
						d.rng = d.rng*1664525 + 1013904223
						v := float64(int32(d.rng))
						spec[k] = v
						energy += v * v
					}
					if energy > 0 {
						scale := math.Pow(2, 0.25*float64(s.sf[g][sfb])) / math.Sqrt(energy)
						for k := start; k < end; k++ {
							spec[k] *= scale
						}
					}
				default:
					gain := math.Pow(2, 0.25*float64(s.sf[g][sfb]-100))
					for k := start; k < end; k++ {
						if x := s.quant[k]; x != 0 {
							spec[k] = iq(x) * gain
						}
					}
				}
			}
		}
		winBase += info.groupLen[g]
	}
}

// stereo applies M/S and intensity stereo to a common-window CPE.
func (d *Decoder) stereo(l, r *ics, msMask int, msUsed *[8][64]bool) {
	info := &l.info
	winBase := 0
	for g := 0; g < info.numGroups; g++ {
		for w := 0; w < info.groupLen[g]; w++ {
			base := (winBase + w) * 128
			if info.windowSequence != eightShort {
				base = 0
			}
			for sfb := 0; sfb < info.maxSfb; sfb++ {
				start, end := base+info.swb[sfb], base+info.swb[sfb+1]
				rcb := r.sfbCb[g][sfb]
				ms := msMask != 0 && msUsed[g][sfb]
				if rcb == intensityHCB || rcb == intensityHCB2 {
					sign := 1.0
					if rcb == intensityHCB2 {
						sign = -1
					}
					if ms {
						sign = -sign
					}
					scale := sign * math.Pow(0.5, 0.25*float64(r.sf[g][sfb]))
					for k := start; k < end; k++ {
						r.spec[k] = l.spec[k] * scale
					}
					continue
				}
				if ms && l.sfbCb[g][sfb] != noiseHCB && rcb != noiseHCB {
					for k := start; k < end; k++ {
						m, s := l.spec[k], r.spec[k]
						l.spec[k], r.spec[k] = m+s, m-s
					}
				}
			}
		}
		winBase += info.groupLen[g]
	}
}

// applyTNS runs the TNS all-pole filters over the spectrum.
func (d *Decoder) applyTNS(s *ics) {
	info := &s.info
	short := info.windowSequence == eightShort
	maxBands := tnsMaxBandsLong[d.sfIdx]
	if short {
		maxBands = tnsMaxBandsShort[d.sfIdx]
	}
	for w := 0; w < info.numWindows; w++ {
		base := w * 128
		if !short {
			base = 0
		}
		bottom := info.numSwb
		for f := 0; f < s.nFilt[w]; f++ {
			t := &s.tns[w][f]
			top := bottom
			bottom = max(top-t.length, 0)
			if t.order == 0 {
				continue
			}
			lim := min(maxBands, info.maxSfb)
			start := info.swb[min(bottom, lim)]
			end := info.swb[min(top, lim)]
			size := end - start
			if size <= 0 {
				continue
			}
			inc := 1
			pos := base + start
			if t.direction {
				inc = -1
				pos = base + end - 1
			}
			var state [21]float64
			order := t.order
			for m := 0; m < size; m++ {
				y := s.spec[pos]
				for j := 0; j < order; j++ {
					y -= state[j] * t.lpc[j+1]
				}
				for j := order - 1; j > 0; j-- {
					state[j] = state[j-1]
				}
				state[0] = y
				s.spec[pos] = y
				pos += inc
			}
		}
	}
}

// finishChannel applies TNS and the filterbank, producing 1024 PCM samples
// for output channel ch.
func (d *Decoder) finishChannel(s *ics, ch int) {
	if s.tnsPresent {
		d.applyTNS(s)
	}
	cs := &d.ch[ch]
	info := &s.info
	z := &d.time
	prev, cur := cs.prevShape, info.windowShape
	switch info.windowSequence {
	case eightShort:
		*z = [2048]float64{}
		for w := 0; w < 8; w++ {
			d.short.imdct(s.spec[w*128:w*128+128], d.tmp[:])
			ls := cur
			if w == 0 {
				ls = prev
			}
			off := 448 + 128*w
			for n := 0; n < 128; n++ {
				z[off+n] += d.tmp[n] * winShort[ls][n]
				z[off+128+n] += d.tmp[128+n] * winShort[cur][127-n]
			}
		}
	default:
		d.long.imdct(s.spec[:], z[:])
		switch info.windowSequence {
		case onlyLong:
			for n := 0; n < 1024; n++ {
				z[n] *= winLong[prev][n]
				z[1024+n] *= winLong[cur][1023-n]
			}
		case longStart:
			for n := 0; n < 1024; n++ {
				z[n] *= winLong[prev][n]
			}
			for n := 1472; n < 1600; n++ {
				z[n] *= winShort[cur][127-(n-1472)]
			}
			for n := 1600; n < 2048; n++ {
				z[n] = 0
			}
		case longStop:
			for n := 0; n < 448; n++ {
				z[n] = 0
			}
			for n := 448; n < 576; n++ {
				z[n] *= winShort[prev][n-448]
			}
			for n := 1024; n < 2048; n++ {
				z[n] *= winLong[cur][2047-n]
			}
		}
	}
	out := &d.pcm[ch]
	for n := 0; n < 1024; n++ {
		out[n] = z[n] + cs.overlap[n]
	}
	copy(cs.overlap[:], z[1024:])
	cs.prevShape = cur
}

// Reset clears the filterbank state (after a seek).
func (d *Decoder) Reset() {
	for i := range d.ch {
		d.ch[i] = chanState{}
	}
}
