package opus

// SILK output resampler (silk/resampler*.c) with the RFC 8251 buffer fix:
// 2x all-pass upsampling + 12-phase FIR interpolation for 8/12/16 kHz to
// 24/48 kHz, an exact 2x up-sampler, and AR2 + FIR down-sampling.

const (
	resamplerCopy    = 0
	resamplerUp2HQ   = 1
	resamplerIIRFIR  = 2
	resamplerDownFIR = 3

	resamplerOrderFIR12 = 8
	resamplerMaxBatchIn = 10 * 48
)

var (
	resamplerUp2HQ0 = [3]int32{1746, 14986, 39083 - 65536}
	resamplerUp2HQ1 = [3]int32{6854, 25769, 55542 - 65536}

	resamplerDelayDec = [3][5]int{
		{4, 0, 2, 0, 0},
		{0, 9, 4, 7, 4},
		{0, 3, 12, 7, 7},
	}
)

type silkResampler struct {
	sIIR        [6]int32
	sFIR32      [36]int32
	sFIR16      [resamplerOrderFIR12]int16
	delayBuf    [48]int16
	fn          int
	batchSize   int
	invRatioQ16 int32
	firOrder    int
	firFracs    int
	fsInKHz     int
	fsOutKHz    int
	inputDelay  int
	coefs       []int16
	buf16       [2*resamplerMaxBatchIn + resamplerOrderFIR12]int16
	buf32       [resamplerMaxBatchIn + 36]int32
}

func rateID(r int) int {
	switch r {
	case 8000:
		return 0
	case 12000:
		return 1
	case 16000:
		return 2
	case 24000:
		return 3
	}
	return 4
}

func (s *silkResampler) init(fsIn, fsOut int) error {
	*s = silkResampler{}
	if (fsIn != 8000 && fsIn != 12000 && fsIn != 16000) ||
		(fsOut != 8000 && fsOut != 12000 && fsOut != 16000 && fsOut != 24000 && fsOut != 48000) {
		return errSILK
	}
	s.inputDelay = resamplerDelayDec[rateID(fsIn)][rateID(fsOut)]
	s.fsInKHz = fsIn / 1000
	s.fsOutKHz = fsOut / 1000
	s.batchSize = s.fsInKHz * 10
	up2x := 0
	switch {
	case fsOut > fsIn:
		if fsOut == 2*fsIn {
			s.fn = resamplerUp2HQ
		} else {
			s.fn = resamplerIIRFIR
			up2x = 1
		}
	case fsOut < fsIn:
		s.fn = resamplerDownFIR
		switch {
		case fsOut*4 == fsIn*3:
			s.firFracs, s.firOrder, s.coefs = 3, 18, silkResampler3_4Coefs[:]
		case fsOut*3 == fsIn*2:
			s.firFracs, s.firOrder, s.coefs = 2, 18, silkResampler2_3Coefs[:]
		case fsOut*2 == fsIn:
			s.firFracs, s.firOrder, s.coefs = 1, 24, silkResampler1_2Coefs[:]
		case fsOut*3 == fsIn:
			s.firFracs, s.firOrder, s.coefs = 1, 36, silkResampler1_3Coefs[:]
		case fsOut*4 == fsIn:
			s.firFracs, s.firOrder, s.coefs = 1, 36, silkResampler1_4Coefs[:]
		case fsOut*6 == fsIn:
			s.firFracs, s.firOrder, s.coefs = 1, 36, silkResampler1_6Coefs[:]
		default:
			return errSILK
		}
	default:
		s.fn = resamplerCopy
	}
	s.invRatioQ16 = ((int32(fsIn) << uint(14+up2x)) / int32(fsOut)) << 2
	for smulww(s.invRatioQ16, int32(fsOut)) < int32(fsIn)<<uint(up2x) {
		s.invRatioQ16++
	}
	return nil
}

// process resamples in (at least 1 ms) into out.
func (s *silkResampler) process(out, in []int16) {
	inLen := len(in)
	nSamples := s.fsInKHz - s.inputDelay
	copy(s.delayBuf[s.inputDelay:s.fsInKHz], in[:nSamples])
	switch s.fn {
	case resamplerUp2HQ:
		up2HQ(&s.sIIR, out, s.delayBuf[:s.fsInKHz])
		up2HQ(&s.sIIR, out[s.fsOutKHz:], in[nSamples:nSamples+inLen-s.fsInKHz])
	case resamplerIIRFIR:
		s.iirFIR(out, s.delayBuf[:s.fsInKHz])
		s.iirFIR(out[s.fsOutKHz:], in[nSamples:nSamples+inLen-s.fsInKHz])
	case resamplerDownFIR:
		s.downFIR(out, s.delayBuf[:s.fsInKHz])
		s.downFIR(out[s.fsOutKHz:], in[nSamples:nSamples+inLen-s.fsInKHz])
	default:
		copy(out, s.delayBuf[:s.fsInKHz])
		copy(out[s.fsOutKHz:], in[nSamples:nSamples+inLen-s.fsInKHz])
	}
	copy(s.delayBuf[:s.inputDelay], in[inLen-s.inputDelay:inLen])
}

func up2HQ(S *[6]int32, out, in []int16) {
	for k := range in {
		in32 := int32(in[k]) << 10
		Y := in32 - S[0]
		X := smulwb(Y, resamplerUp2HQ0[0])
		o1 := S[0] + X
		S[0] = in32 + X
		Y = o1 - S[1]
		X = smulwb(Y, resamplerUp2HQ0[1])
		o2 := S[1] + X
		S[1] = o1 + X
		Y = o2 - S[2]
		X = smlawb(Y, Y, resamplerUp2HQ0[2])
		o1 = S[2] + X
		S[2] = o2 + X
		out[2*k] = int16(sat16(rshiftRound(o1, 10)))

		Y = in32 - S[3]
		X = smulwb(Y, resamplerUp2HQ1[0])
		o1 = S[3] + X
		S[3] = in32 + X
		Y = o1 - S[4]
		X = smulwb(Y, resamplerUp2HQ1[1])
		o2 = S[4] + X
		S[4] = o1 + X
		Y = o2 - S[5]
		X = smlawb(Y, Y, resamplerUp2HQ1[2])
		o1 = S[5] + X
		S[5] = o2 + X
		out[2*k+1] = int16(sat16(rshiftRound(o1, 10)))
	}
}

func (s *silkResampler) iirFIR(out, in []int16) {
	buf := s.buf16[:]
	copy(buf[:resamplerOrderFIR12], s.sFIR16[:])
	inc := s.invRatioQ16
	inLen := len(in)
	o := 0
	var nIn int
	for {
		nIn = min(inLen, s.batchSize)
		up2HQ(&s.sIIR, buf[resamplerOrderFIR12:], in[:nIn])
		maxIdx := int32(nIn) << (16 + 1)
		for idx := int32(0); idx < maxIdx; idx += inc {
			ti := smulwb(idx&0xFFFF, 12)
			b := buf[idx>>16:]
			f := silkResamplerFracFIR12[ti*4:]
			g := silkResamplerFracFIR12[(11-ti)*4:]
			r := smulbb(int32(b[0]), int32(f[0]))
			r = smlabb(r, int32(b[1]), int32(f[1]))
			r = smlabb(r, int32(b[2]), int32(f[2]))
			r = smlabb(r, int32(b[3]), int32(f[3]))
			r = smlabb(r, int32(b[4]), int32(g[3]))
			r = smlabb(r, int32(b[5]), int32(g[2]))
			r = smlabb(r, int32(b[6]), int32(g[1]))
			r = smlabb(r, int32(b[7]), int32(g[0]))
			out[o] = int16(sat16(rshiftRound(r, 15)))
			o++
		}
		in = in[nIn:]
		inLen -= nIn
		if inLen > 0 {
			copy(buf[:resamplerOrderFIR12], buf[nIn<<1:nIn<<1+resamplerOrderFIR12])
		} else {
			break
		}
	}
	copy(s.sFIR16[:], buf[nIn<<1:nIn<<1+resamplerOrderFIR12])
}

func (s *silkResampler) downFIR(out, in []int16) {
	buf := s.buf32[:]
	copy(buf[:s.firOrder], s.sFIR32[:s.firOrder])
	fir := s.coefs[2:]
	inc := s.invRatioQ16
	inLen := len(in)
	o := 0
	var nIn int
	for {
		nIn = min(inLen, s.batchSize)
		// silk_resampler_private_AR2
		for k := 0; k < nIn; k++ {
			out32 := s.sIIR[0] + int32(in[k])<<8
			buf[s.firOrder+k] = out32
			out32 <<= 2
			s.sIIR[0] = smlawb(s.sIIR[1], out32, int32(s.coefs[0]))
			s.sIIR[1] = smulwb(out32, int32(s.coefs[1]))
		}
		maxIdx := int32(nIn) << 16
		for idx := int32(0); idx < maxIdx; idx += inc {
			b := buf[idx>>16:]
			var r int32
			switch s.firOrder {
			case 18:
				ii := int(smulwb(idx&0xFFFF, int32(s.firFracs)))
				p := fir[9*ii:]
				r = smulwb(b[0], int32(p[0]))
				for j := 1; j < 9; j++ {
					r = smlawb(r, b[j], int32(p[j]))
				}
				p = fir[9*(s.firFracs-1-ii):]
				for j := 0; j < 9; j++ {
					r = smlawb(r, b[17-j], int32(p[j]))
				}
			default:
				half := s.firOrder / 2
				r = smulwb(b[0]+b[s.firOrder-1], int32(fir[0]))
				for j := 1; j < half; j++ {
					r = smlawb(r, b[j]+b[s.firOrder-1-j], int32(fir[j]))
				}
			}
			out[o] = int16(sat16(rshiftRound(r, 6)))
			o++
		}
		in = in[nIn:]
		inLen -= nIn
		if inLen > 1 {
			copy(buf[:s.firOrder], buf[nIn:nIn+s.firOrder])
		} else {
			break
		}
	}
	copy(s.sFIR32[:s.firOrder], buf[nIn:nIn+s.firOrder])
}
