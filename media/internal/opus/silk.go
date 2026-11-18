package opus

import "errors"

// SILK decoder (RFC 6716 section 4.2), a port of the fixed-point decoder in
// silk/ (dec_API.c, decode_frame.c, decode_indices.c, decode_pulses.c,
// decode_parameters.c, decode_core.c, NLSF*.c, stereo_*.c, PLC.c, CNG.c),
// with the RFC 8251 fixes. SILK is integer-only, so the output matches the
// reference bit for bit.

const (
	silkMaxLPCOrder    = 16
	silkMinLPCOrder    = 10
	silkMaxNbSubfr     = 4
	silkMaxFrameLength = 320
	silkMaxSubfrLength = 80
	silkLTPOrder       = 5

	typeNoVoiceActivity = 0
	typeUnvoiced        = 1
	typeVoiced          = 2

	codeIndependently           = 0
	codeIndependentlyNoLTPScale = 1
	codeConditionally           = 2

	flagDecodeNormal = 0
	flagPacketLost   = 1
	flagDecodeLBRR   = 2

	nlsfQuantMaxAmplitude   = 4
	quantLevelAdjustQ10     = 80
	bweAfterLossQ16         = 63570
	maxLPCStabilizeIters    = 16
	shellCodecFrameLength   = 16
	maxPulses               = 16
	nRateLevels             = 10
	stereoInterpLenMs       = 8
	nLevelsQGain            = 64
	minDeltaGainQuant       = -4
	maxDeltaGainQuant       = 36
	minQGainDB              = 2
	maxQGainDB              = 88
	cngBufMaskMax           = 255
	cngGainSmthQ16          = 4634
	cngNLSFSmthQ16          = 16348
	cngGainSmthThresholdQ16 = 46396
)

var (
	silkUniform3ICDF = [3]uint8{171, 85, 0}
	silkUniform5ICDF = [5]uint8{205, 154, 102, 51, 0}
	errSILK          = errors.New("opus: corrupt SILK frame")
)

type nlsfCodebook struct {
	nVectors           int
	order              int
	quantStepSizeQ16   int32
	invQuantStepSizeQ6 int32
	cb1NLSFQ8          []uint8
	cb1ICDF            []uint8
	predQ8             []uint8
	ecSel              []uint8
	ecICDF             []uint8
	deltaMinQ15        []int16
	cb1WghtQ9          []int16
}

var (
	nlsfCBNBMB = &nlsfCodebook{32, 10, 11796, 356, silkNLSFCB1NBMBQ8[:], silkNLSFCB1ICDFNBMB[:],
		silkNLSFPredNBMBQ8[:], silkNLSFCB2SelectNBMB[:], silkNLSFCB2ICDFNBMB[:], silkNLSFDeltaMinNBMBQ15[:], silkNLSFCB1NBMBWghtQ9[:]}
	nlsfCBWB = &nlsfCodebook{32, 16, 9830, 427, silkNLSFCB1WBQ8[:], silkNLSFCB1ICDFWB[:],
		silkNLSFPredWBQ8[:], silkNLSFCB2SelectWB[:], silkNLSFCB2ICDFWB[:], silkNLSFDeltaMinWBQ15[:], silkNLSFCB1WBWghtQ9[:]}
)

type silkDecControl struct {
	nChannelsAPI       int
	nChannelsInternal  int
	apiSampleRate      int
	internalSampleRate int
	payloadSizeMs      int
	prevPitchLag       int
}

type sideInfo struct {
	gainsIndices     [silkMaxNbSubfr]int
	ltpIndex         [silkMaxNbSubfr]int
	nlsfIndices      [silkMaxLPCOrder + 1]int
	lagIndex         int
	contourIndex     int
	signalType       int
	quantOffsetType  int
	nlsfInterpCoefQ2 int
	perIndex         int
	ltpScaleIndex    int
	seed             int
}

type silkDecCtrl struct {
	pitchL      [silkMaxNbSubfr]int
	gainsQ16    [silkMaxNbSubfr]int32
	predCoefQ12 [2][silkMaxLPCOrder]int16
	ltpCoefQ14  [silkLTPOrder * silkMaxNbSubfr]int16
	ltpScaleQ14 int32
}

type plcState struct {
	pitchLQ8        int32
	ltpCoefQ14      [silkLTPOrder]int16
	prevLPCQ12      [silkMaxLPCOrder]int16
	lastFrameLost   bool
	randSeed        int32
	randScaleQ14    int16
	concEnergy      int32
	concEnergyShift int
	prevLTPScaleQ14 int16
	prevGainQ16     [2]int32
	fsKHz           int
	nbSubfr         int
	subfrLength     int
}

type cngState struct {
	excBufQ14   [silkMaxFrameLength]int32
	smthNLSFQ15 [silkMaxLPCOrder]int16
	synthState  [silkMaxLPCOrder]int32
	smthGainQ16 int32
	randSeed    int32
	fsKHz       int
}

type silkChannel struct {
	prevGainQ16          int32
	excQ14               [silkMaxFrameLength]int32
	sLPCQ14Buf           [silkMaxLPCOrder]int32
	outBuf               [silkMaxFrameLength + 2*silkMaxSubfrLength]int16
	lagPrev              int
	lastGainIndex        int
	fsKHz                int
	fsAPIHz              int
	nbSubfr              int
	frameLength          int
	subfrLength          int
	ltpMemLength         int
	lpcOrder             int
	prevNLSFQ15          [silkMaxLPCOrder]int16
	firstFrameAfterReset bool
	pitchLagLowBitsICDF  []uint8
	pitchContourICDF     []uint8
	nFramesDecoded       int
	nFramesPerPacket     int
	ecPrevSignalType     int
	ecPrevLagIndex       int
	vadFlags             [3]int
	lbrrFlag             int
	lbrrFlags            [3]int
	resampler            silkResampler
	nlsfCB               *nlsfCodebook
	indices              sideInfo
	cng                  cngState
	lossCnt              int
	prevSignalType       int
	plc                  plcState
}

type stereoDecState struct {
	predPrevQ13 [2]int32
	sMid        [2]int16
	sSide       [2]int16
}

type silkDecoder struct {
	ch                   [2]silkChannel
	stereo               stereoDecState
	nChannelsAPI         int
	nChannelsInternal    int
	prevDecodeOnlyMiddle int

	pulses [silkMaxFrameLength + shellCodecFrameLength]int
	out1   [2][silkMaxFrameLength + 2]int16
	out2   [48 * 20]int16
}

func newSILKDecoder() *silkDecoder {
	s := &silkDecoder{}
	s.reset()
	return s
}

// reset is silk_InitDecoder (with the RFC 8251 stereo state reset).
func (s *silkDecoder) reset() {
	for n := range s.ch {
		s.ch[n].init()
	}
	s.stereo = stereoDecState{}
	s.prevDecodeOnlyMiddle = 0
	s.nChannelsAPI = 0
	s.nChannelsInternal = 0
}

func (c *silkChannel) init() {
	*c = silkChannel{}
	c.firstFrameAfterReset = true
	c.prevGainQ16 = 65536
	c.cngReset()
	c.plcReset()
}

func (c *silkChannel) setFs(fsKHz, fsAPIHz int) error {
	c.subfrLength = 5 * fsKHz
	frameLength := c.nbSubfr * c.subfrLength
	if c.fsKHz != fsKHz || c.fsAPIHz != fsAPIHz {
		if err := c.resampler.init(fsKHz*1000, fsAPIHz); err != nil {
			return err
		}
		c.fsAPIHz = fsAPIHz
	}
	if c.fsKHz != fsKHz || frameLength != c.frameLength {
		if fsKHz == 8 {
			if c.nbSubfr == silkMaxNbSubfr {
				c.pitchContourICDF = silkPitchContourNBICDF[:]
			} else {
				c.pitchContourICDF = silkPitchContour10msNBICDF[:]
			}
		} else {
			if c.nbSubfr == silkMaxNbSubfr {
				c.pitchContourICDF = silkPitchContourICDF[:]
			} else {
				c.pitchContourICDF = silkPitchContour10msICDF[:]
			}
		}
		if c.fsKHz != fsKHz {
			c.ltpMemLength = 20 * fsKHz
			if fsKHz == 8 || fsKHz == 12 {
				c.lpcOrder = silkMinLPCOrder
				c.nlsfCB = nlsfCBNBMB
			} else {
				c.lpcOrder = silkMaxLPCOrder
				c.nlsfCB = nlsfCBWB
			}
			switch fsKHz {
			case 16:
				c.pitchLagLowBitsICDF = silkUniform8ICDF[:]
			case 12:
				c.pitchLagLowBitsICDF = silkUniform6ICDF[:]
			default:
				c.pitchLagLowBitsICDF = silkUniform4ICDF[:]
			}
			c.firstFrameAfterReset = true
			c.lagPrev = 100
			c.lastGainIndex = 10
			c.prevSignalType = typeNoVoiceActivity
			c.outBuf = [len(c.outBuf)]int16{}
			c.sLPCQ14Buf = [silkMaxLPCOrder]int32{}
		}
		c.fsKHz = fsKHz
		c.frameLength = frameLength
	}
	return nil
}

// decode is silk_Decode: it decodes one 10 or 20 ms SILK frame (per
// channel) from the packet and writes the resampled, interleaved output.
func (s *silkDecoder) decode(ctl *silkDecControl, lostFlag int, newPacket bool, d *rangeDecoder, out []int16) (int, error) {
	ch := &s.ch
	decodeOnlyMiddle := 0
	var msPredQ13 [2]int32
	if newPacket {
		for n := 0; n < ctl.nChannelsInternal; n++ {
			ch[n].nFramesDecoded = 0
		}
	}
	if ctl.nChannelsInternal > s.nChannelsInternal {
		ch[1].init()
	}
	stereoToMono := ctl.nChannelsInternal == 1 && s.nChannelsInternal == 2 &&
		ctl.internalSampleRate == 1000*ch[0].fsKHz
	if ch[0].nFramesDecoded == 0 {
		for n := 0; n < ctl.nChannelsInternal; n++ {
			switch ctl.payloadSizeMs {
			case 0, 10:
				ch[n].nFramesPerPacket = 1
				ch[n].nbSubfr = 2
			case 20:
				ch[n].nFramesPerPacket = 1
				ch[n].nbSubfr = 4
			case 40:
				ch[n].nFramesPerPacket = 2
				ch[n].nbSubfr = 4
			case 60:
				ch[n].nFramesPerPacket = 3
				ch[n].nbSubfr = 4
			default:
				return 0, errSILK
			}
			fsKHzDec := (ctl.internalSampleRate >> 10) + 1
			if fsKHzDec != 8 && fsKHzDec != 12 && fsKHzDec != 16 {
				return 0, errSILK
			}
			if err := ch[n].setFs(fsKHzDec, ctl.apiSampleRate); err != nil {
				return 0, err
			}
		}
	}
	if ctl.nChannelsAPI == 2 && ctl.nChannelsInternal == 2 && (s.nChannelsAPI == 1 || s.nChannelsInternal == 1) {
		s.stereo.predPrevQ13 = [2]int32{}
		s.stereo.sSide = [2]int16{}
		ch[1].resampler = ch[0].resampler
	}
	s.nChannelsAPI = ctl.nChannelsAPI
	s.nChannelsInternal = ctl.nChannelsInternal

	if lostFlag != flagPacketLost && ch[0].nFramesDecoded == 0 {
		for n := 0; n < ctl.nChannelsInternal; n++ {
			for i := 0; i < ch[n].nFramesPerPacket; i++ {
				ch[n].vadFlags[i] = d.bitLogp(1)
			}
			ch[n].lbrrFlag = d.bitLogp(1)
		}
		for n := 0; n < ctl.nChannelsInternal; n++ {
			ch[n].lbrrFlags = [3]int{}
			if ch[n].lbrrFlag != 0 {
				if ch[n].nFramesPerPacket == 1 {
					ch[n].lbrrFlags[0] = 1
				} else {
					var sym int
					if ch[n].nFramesPerPacket == 2 {
						sym = d.icdf(silkLBRRFlags2ICDF[:], 8) + 1
					} else {
						sym = d.icdf(silkLBRRFlags3ICDF[:], 8) + 1
					}
					for i := 0; i < ch[n].nFramesPerPacket; i++ {
						ch[n].lbrrFlags[i] = (sym >> uint(i)) & 1
					}
				}
			}
		}
		if lostFlag == flagDecodeNormal {
			// Skip all LBRR (in-band FEC) data.
			for i := 0; i < ch[0].nFramesPerPacket; i++ {
				for n := 0; n < ctl.nChannelsInternal; n++ {
					if ch[n].lbrrFlags[i] != 0 {
						if ctl.nChannelsInternal == 2 && n == 0 {
							stereoDecodePred(d, &msPredQ13)
							if ch[1].lbrrFlags[i] == 0 {
								decodeOnlyMiddle = d.icdf(silkStereoOnlyCodeMidICDF[:], 8)
							}
						}
						cond := codeIndependently
						if i > 0 && ch[n].lbrrFlags[i-1] != 0 {
							cond = codeConditionally
						}
						ch[n].decodeIndices(d, i, true, cond)
						silkDecodePulses(d, s.pulses[:], ch[n].indices.signalType, ch[n].indices.quantOffsetType, ch[n].frameLength)
					}
				}
			}
		}
	}

	if ctl.nChannelsInternal == 2 {
		if lostFlag == flagDecodeNormal ||
			(lostFlag == flagDecodeLBRR && ch[0].lbrrFlags[ch[0].nFramesDecoded] == 1) {
			stereoDecodePred(d, &msPredQ13)
			if (lostFlag == flagDecodeNormal && ch[1].vadFlags[ch[0].nFramesDecoded] == 0) ||
				(lostFlag == flagDecodeLBRR && ch[1].lbrrFlags[ch[0].nFramesDecoded] == 0) {
				decodeOnlyMiddle = d.icdf(silkStereoOnlyCodeMidICDF[:], 8)
			} else {
				decodeOnlyMiddle = 0
			}
		} else {
			msPredQ13 = s.stereo.predPrevQ13
		}
	}
	if ctl.nChannelsInternal == 2 && decodeOnlyMiddle == 0 && s.prevDecodeOnlyMiddle == 1 {
		c1 := &ch[1]
		c1.outBuf = [len(c1.outBuf)]int16{}
		c1.sLPCQ14Buf = [silkMaxLPCOrder]int32{}
		c1.lagPrev = 100
		c1.lastGainIndex = 10
		c1.prevSignalType = typeNoVoiceActivity
		c1.firstFrameAfterReset = true
	}
	hasSide := false
	if lostFlag == flagDecodeNormal {
		hasSide = decodeOnlyMiddle == 0
	} else {
		hasSide = s.prevDecodeOnlyMiddle == 0 ||
			(ctl.nChannelsInternal == 2 && lostFlag == flagDecodeLBRR && ch[1].lbrrFlags[ch[1].nFramesDecoded] == 1)
	}
	nSamplesOutDec := 0
	for n := 0; n < ctl.nChannelsInternal; n++ {
		if n == 0 || hasSide {
			frameIndex := ch[0].nFramesDecoded - n
			cond := codeConditionally
			if frameIndex <= 0 {
				cond = codeIndependently
			} else if lostFlag == flagDecodeLBRR {
				if ch[n].lbrrFlags[frameIndex-1] != 0 {
					cond = codeConditionally
				} else {
					cond = codeIndependently
				}
			} else if n > 0 && s.prevDecodeOnlyMiddle != 0 {
				cond = codeIndependentlyNoLTPScale
			}
			nSamplesOutDec = ch[n].decodeFrame(d, s.out1[n][2:], lostFlag, cond, s.pulses[:])
		} else {
			clear(s.out1[n][2 : 2+nSamplesOutDec])
		}
		ch[n].nFramesDecoded++
	}
	if ctl.nChannelsAPI == 2 && ctl.nChannelsInternal == 2 {
		stereoMSToLR(&s.stereo, s.out1[0][:], s.out1[1][:], msPredQ13, ch[0].fsKHz, nSamplesOutDec)
	} else {
		copy(s.out1[0][:2], s.stereo.sMid[:])
		copy(s.stereo.sMid[:], s.out1[0][nSamplesOutDec:nSamplesOutDec+2])
	}
	nSamplesOut := nSamplesOutDec * ctl.apiSampleRate / (ch[0].fsKHz * 1000)
	resOut := s.out2[:]
	if ctl.nChannelsAPI != 2 {
		resOut = out
	}
	for n := 0; n < min(ctl.nChannelsAPI, ctl.nChannelsInternal); n++ {
		ch[n].resampler.process(resOut, s.out1[n][1:1+nSamplesOutDec])
		if ctl.nChannelsAPI == 2 {
			for i := 0; i < nSamplesOut; i++ {
				out[n+2*i] = resOut[i]
			}
		}
	}
	if ctl.nChannelsAPI == 2 && ctl.nChannelsInternal == 1 {
		if stereoToMono {
			ch[1].resampler.process(resOut, s.out1[0][1:1+nSamplesOutDec])
			for i := 0; i < nSamplesOut; i++ {
				out[1+2*i] = resOut[i]
			}
		} else {
			for i := 0; i < nSamplesOut; i++ {
				out[1+2*i] = out[2*i]
			}
		}
	}
	if ch[0].prevSignalType == typeVoiced {
		mult := [3]int{6, 4, 3}
		ctl.prevPitchLag = ch[0].lagPrev * mult[(ch[0].fsKHz-8)>>2]
	} else {
		ctl.prevPitchLag = 0
	}
	if lostFlag == flagPacketLost {
		for i := 0; i < s.nChannelsInternal; i++ {
			s.ch[i].lastGainIndex = 10
		}
	} else {
		s.prevDecodeOnlyMiddle = decodeOnlyMiddle
	}
	return nSamplesOut, nil
}

func stereoDecodePred(d *rangeDecoder, predQ13 *[2]int32) {
	var ix [2][3]int
	n := d.icdf(silkStereoPredJointICDF[:], 8)
	ix[0][2] = n / 5
	ix[1][2] = n - 5*ix[0][2]
	for n := 0; n < 2; n++ {
		ix[n][0] = d.icdf(silkUniform3ICDF[:], 8)
		ix[n][1] = d.icdf(silkUniform5ICDF[:], 8)
	}
	for n := 0; n < 2; n++ {
		ix[n][0] += 3 * ix[n][2]
		lowQ13 := int32(silkStereoPredQuantQ13[ix[n][0]])
		stepQ13 := smulwb(int32(silkStereoPredQuantQ13[ix[n][0]+1])-lowQ13, 6554)
		predQ13[n] = smlabb(lowQ13, stepQ13, int32(2*ix[n][1]+1))
	}
	predQ13[0] -= predQ13[1]
}

func stereoMSToLR(st *stereoDecState, x1, x2 []int16, predQ13 [2]int32, fsKHz, frameLength int) {
	copy(x1[:2], st.sMid[:])
	copy(x2[:2], st.sSide[:])
	copy(st.sMid[:], x1[frameLength:frameLength+2])
	copy(st.sSide[:], x2[frameLength:frameLength+2])
	pred0 := st.predPrevQ13[0]
	pred1 := st.predPrevQ13[1]
	denomQ16 := int32((1 << 16) / (stereoInterpLenMs * fsKHz))
	delta0 := rshiftRound(smulbb(predQ13[0]-st.predPrevQ13[0], denomQ16), 16)
	delta1 := rshiftRound(smulbb(predQ13[1]-st.predPrevQ13[1], denomQ16), 16)
	n := 0
	for ; n < stereoInterpLenMs*fsKHz && n < frameLength; n++ {
		pred0 += delta0
		pred1 += delta1
		sum := (int32(x1[n]) + int32(x1[n+2]) + int32(x1[n+1])<<1) << 9
		sum = smlawb(int32(x2[n+1])<<8, sum, pred0)
		sum = smlawb(sum, int32(x1[n+1])<<11, pred1)
		x2[n+1] = int16(sat16(rshiftRound(sum, 8)))
	}
	pred0 = predQ13[0]
	pred1 = predQ13[1]
	for ; n < frameLength; n++ {
		sum := (int32(x1[n]) + int32(x1[n+2]) + int32(x1[n+1])<<1) << 9
		sum = smlawb(int32(x2[n+1])<<8, sum, pred0)
		sum = smlawb(sum, int32(x1[n+1])<<11, pred1)
		x2[n+1] = int16(sat16(rshiftRound(sum, 8)))
	}
	st.predPrevQ13 = predQ13
	for n := 0; n < frameLength; n++ {
		sum := int32(x1[n+1]) + int32(x2[n+1])
		diff := int32(x1[n+1]) - int32(x2[n+1])
		x1[n+1] = int16(sat16(sum))
		x2[n+1] = int16(sat16(diff))
	}
}

// decodeFrame is silk_decode_frame; it returns the frame length.
func (c *silkChannel) decodeFrame(d *rangeDecoder, out []int16, lostFlag, cond int, pulses []int) int {
	var ctrl silkDecCtrl
	L := c.frameLength
	if lostFlag == flagDecodeNormal || (lostFlag == flagDecodeLBRR && c.lbrrFlags[c.nFramesDecoded] == 1) {
		c.decodeIndices(d, c.nFramesDecoded, lostFlag != 0, cond)
		silkDecodePulses(d, pulses, c.indices.signalType, c.indices.quantOffsetType, c.frameLength)
		c.decodeParameters(&ctrl, cond)
		L = c.frameLength
		c.decodeCore(&ctrl, out, pulses)
		c.plcUpdate(&ctrl)
		c.lossCnt = 0
		c.prevSignalType = c.indices.signalType
		c.firstFrameAfterReset = false
	} else {
		if c.fsKHz != c.plc.fsKHz {
			c.plcReset()
			c.plc.fsKHz = c.fsKHz
		}
		c.plcConceal(&ctrl, out)
		c.lossCnt++
	}
	mvLen := c.ltpMemLength - c.frameLength
	copy(c.outBuf[:mvLen], c.outBuf[c.frameLength:c.frameLength+mvLen])
	copy(c.outBuf[mvLen:], out[:c.frameLength])
	c.cngApply(&ctrl, out, L)
	c.plcGlueFrames(out, L)
	c.lagPrev = ctrl.pitchL[c.nbSubfr-1]
	return L
}

func (c *silkChannel) decodeIndices(d *rangeDecoder, frameIndex int, decodeLBRR bool, cond int) {
	ix := 0
	if decodeLBRR || c.vadFlags[frameIndex] != 0 {
		ix = d.icdf(silkTypeOffsetVADICDF[:], 8) + 2
	} else {
		ix = d.icdf(silkTypeOffsetNoVADICDF[:], 8)
	}
	ind := &c.indices
	ind.signalType = ix >> 1
	ind.quantOffsetType = ix & 1
	if cond == codeConditionally {
		ind.gainsIndices[0] = d.icdf(silkDeltaGainICDF[:], 8)
	} else {
		ind.gainsIndices[0] = d.icdf(silkGainICDF[ind.signalType*8:ind.signalType*8+8], 8) << 3
		ind.gainsIndices[0] += d.icdf(silkUniform8ICDF[:], 8)
	}
	for i := 1; i < c.nbSubfr; i++ {
		ind.gainsIndices[i] = d.icdf(silkDeltaGainICDF[:], 8)
	}
	cb := c.nlsfCB
	ind.nlsfIndices[0] = d.icdf(cb.cb1ICDF[(ind.signalType>>1)*cb.nVectors:], 8)
	var ecIx [silkMaxLPCOrder]int
	var predQ8 [silkMaxLPCOrder]uint8
	nlsfUnpack(ecIx[:], predQ8[:], cb, ind.nlsfIndices[0])
	for i := 0; i < cb.order; i++ {
		v := d.icdf(cb.ecICDF[ecIx[i]:], 8)
		if v == 0 {
			v -= d.icdf(silkNLSFExtICDF[:], 8)
		} else if v == 2*nlsfQuantMaxAmplitude {
			v += d.icdf(silkNLSFExtICDF[:], 8)
		}
		ind.nlsfIndices[i+1] = v - nlsfQuantMaxAmplitude
	}
	if c.nbSubfr == silkMaxNbSubfr {
		ind.nlsfInterpCoefQ2 = d.icdf(silkNLSFInterpolationFactorICDF[:], 8)
	} else {
		ind.nlsfInterpCoefQ2 = 4
	}
	if ind.signalType == typeVoiced {
		absolute := true
		if cond == codeConditionally && c.ecPrevSignalType == typeVoiced {
			delta := d.icdf(silkPitchDeltaICDF[:], 8)
			if delta > 0 {
				delta -= 9
				ind.lagIndex = int(int16(c.ecPrevLagIndex + delta))
				absolute = false
			}
		}
		if absolute {
			ind.lagIndex = d.icdf(silkPitchLagICDF[:], 8) * (c.fsKHz >> 1)
			ind.lagIndex += d.icdf(c.pitchLagLowBitsICDF, 8)
		}
		c.ecPrevLagIndex = ind.lagIndex
		ind.contourIndex = d.icdf(c.pitchContourICDF, 8)
		ind.perIndex = d.icdf(silkLTPPerIndexICDF[:], 8)
		for k := 0; k < c.nbSubfr; k++ {
			switch ind.perIndex {
			case 0:
				ind.ltpIndex[k] = d.icdf(silkLTPGainICDF0[:], 8)
			case 1:
				ind.ltpIndex[k] = d.icdf(silkLTPGainICDF1[:], 8)
			default:
				ind.ltpIndex[k] = d.icdf(silkLTPGainICDF2[:], 8)
			}
		}
		if cond == codeIndependently {
			ind.ltpScaleIndex = d.icdf(silkLTPscaleICDF[:], 8)
		} else {
			ind.ltpScaleIndex = 0
		}
	}
	c.ecPrevSignalType = ind.signalType
	ind.seed = d.icdf(silkUniform4ICDF[:], 8)
}

var shellTables = [4][]uint8{silkShellCodeTable0[:], silkShellCodeTable1[:], silkShellCodeTable2[:], silkShellCodeTable3[:]}

func decodeSplit(d *rangeDecoder, p int, table []uint8) (int, int) {
	if p > 0 {
		c1 := d.icdf(table[silkShellCodeTableOffsets[p]:], 8)
		return c1, p - c1
	}
	return 0, 0
}

func shellDecoder(out []int, d *rangeDecoder, pulses4 int) {
	var p3 [2]int
	var p2 [4]int
	var p1 [8]int
	t0, t1, t2, t3 := shellTables[0], shellTables[1], shellTables[2], shellTables[3]
	p3[0], p3[1] = decodeSplit(d, pulses4, t3)
	p2[0], p2[1] = decodeSplit(d, p3[0], t2)
	p1[0], p1[1] = decodeSplit(d, p2[0], t1)
	out[0], out[1] = decodeSplit(d, p1[0], t0)
	out[2], out[3] = decodeSplit(d, p1[1], t0)
	p1[2], p1[3] = decodeSplit(d, p2[1], t1)
	out[4], out[5] = decodeSplit(d, p1[2], t0)
	out[6], out[7] = decodeSplit(d, p1[3], t0)
	p2[2], p2[3] = decodeSplit(d, p3[1], t2)
	p1[4], p1[5] = decodeSplit(d, p2[2], t1)
	out[8], out[9] = decodeSplit(d, p1[4], t0)
	out[10], out[11] = decodeSplit(d, p1[5], t0)
	p1[6], p1[7] = decodeSplit(d, p2[3], t1)
	out[12], out[13] = decodeSplit(d, p1[6], t0)
	out[14], out[15] = decodeSplit(d, p1[7], t0)
}

func silkDecodePulses(d *rangeDecoder, pulses []int, signalType, quantOffsetType, frameLength int) {
	var sumPulses, nLshifts [silkMaxFrameLength/shellCodecFrameLength + 1]int
	rateLevel := d.icdf(silkRateLevelsICDF[(signalType>>1)*9:(signalType>>1)*9+9], 8)
	iter := frameLength >> 4
	if iter*shellCodecFrameLength < frameLength {
		iter++
	}
	cdf := silkPulsesPerBlockICDF[rateLevel*18 : rateLevel*18+18]
	for i := 0; i < iter; i++ {
		nLshifts[i] = 0
		sumPulses[i] = d.icdf(cdf, 8)
		for sumPulses[i] == maxPulses+1 {
			nLshifts[i]++
			off := 0
			if nLshifts[i] == 10 {
				off = 1
			}
			sumPulses[i] = d.icdf(silkPulsesPerBlockICDF[(nRateLevels-1)*18+off:(nRateLevels-1)*18+18], 8)
			if nLshifts[i] > 10 {
				break
			}
		}
	}
	for i := 0; i < iter; i++ {
		p := pulses[i*16 : i*16+16]
		if sumPulses[i] > 0 {
			shellDecoder(p, d, sumPulses[i])
		} else {
			clear(p)
		}
	}
	for i := 0; i < iter; i++ {
		if nLS := nLshifts[i]; nLS > 0 {
			p := pulses[i*16 : i*16+16]
			for k := 0; k < 16; k++ {
				absQ := p[k]
				for j := 0; j < nLS; j++ {
					absQ = absQ<<1 + d.icdf(silkLsbICDF[:], 8)
				}
				p[k] = absQ
			}
			sumPulses[i] |= nLS << 5
		}
	}
	// silk_decode_signs
	var icdf [2]uint8
	base := 7 * (quantOffsetType + signalType<<1)
	length := (frameLength + shellCodecFrameLength/2) >> 4
	for i := 0; i < length; i++ {
		p := sumPulses[i]
		if p > 0 {
			icdf[0] = silkSignICDF[base+min(p&0x1F, 6)]
			q := pulses[i*16 : i*16+16]
			for j := 0; j < 16; j++ {
				if q[j] > 0 {
					q[j] *= d.icdf(icdf[:], 8)<<1 - 1
				}
			}
		}
	}
}

func nlsfUnpack(ecIx []int, predQ8 []uint8, cb *nlsfCodebook, cb1Index int) {
	sel := cb.ecSel[cb1Index*cb.order/2:]
	for i := 0; i < cb.order; i += 2 {
		entry := sel[i/2]
		ecIx[i] = int((entry>>1)&7) * (2*nlsfQuantMaxAmplitude + 1)
		predQ8[i] = cb.predQ8[i+int(entry&1)*(cb.order-1)]
		ecIx[i+1] = int((entry>>5)&7) * (2*nlsfQuantMaxAmplitude + 1)
		predQ8[i+1] = cb.predQ8[i+int((entry>>4)&1)*(cb.order-1)+1]
	}
}

func nlsfDecode(nlsfQ15 []int16, indices []int, cb *nlsfCodebook) {
	order := cb.order
	elem := cb.cb1NLSFQ8[indices[0]*order:]
	for i := 0; i < order; i++ {
		nlsfQ15[i] = int16(int32(elem[i]) << 7)
	}
	var ecIx [silkMaxLPCOrder]int
	var predQ8 [silkMaxLPCOrder]uint8
	nlsfUnpack(ecIx[:], predQ8[:], cb, indices[0])
	// silk_NLSF_residual_dequant
	var resQ10 [silkMaxLPCOrder]int16
	outQ10 := int32(0)
	for i := order - 1; i >= 0; i-- {
		predQ10 := smulbb(outQ10, int32(int16(predQ8[i]))) >> 8
		outQ10 = int32(indices[i+1]) << 10
		if outQ10 > 0 {
			outQ10 = int32(int16(outQ10 - 102))
		} else if outQ10 < 0 {
			outQ10 = int32(int16(outQ10 + 102))
		}
		outQ10 = smlawb(predQ10, outQ10, cb.quantStepSizeQ16)
		resQ10[i] = int16(outQ10)
	}
	// libopus >= 1.2 uses precomputed codebook weights rather than
	// computing the Laroia weights at run time.
	wght := cb.cb1WghtQ9[indices[0]*order:]
	for i := 0; i < order; i++ {
		v := (int32(resQ10[i])<<14)/int32(wght[i]) + int32(int16(elem[i]))<<7
		nlsfQ15[i] = int16(limit32(v, 0, 32767))
	}
	nlsfStabilize(nlsfQ15, cb.deltaMinQ15, order)
}

func nlsfStabilize(nlsf []int16, deltaMin []int16, L int) {
	const maxLoops = 20
	loops := 0
	for ; loops < maxLoops; loops++ {
		minDiff := int32(nlsf[0]) - int32(deltaMin[0])
		I := 0
		for i := 1; i <= L-1; i++ {
			diff := int32(nlsf[i]) - (int32(nlsf[i-1]) + int32(deltaMin[i]))
			if diff < minDiff {
				minDiff = diff
				I = i
			}
		}
		diff := int32(1<<15) - (int32(nlsf[L-1]) + int32(deltaMin[L]))
		if diff < minDiff {
			minDiff = diff
			I = L
		}
		if minDiff >= 0 {
			return
		}
		if I == 0 {
			nlsf[0] = deltaMin[0]
		} else if I == L {
			nlsf[L-1] = int16((1 << 15) - int32(deltaMin[L]))
		} else {
			minCenter := int32(0)
			for k := 0; k < I; k++ {
				minCenter += int32(deltaMin[k])
			}
			minCenter += int32(deltaMin[I]) >> 1
			maxCenter := int32(1 << 15)
			for k := L; k > I; k-- {
				maxCenter -= int32(deltaMin[k])
			}
			maxCenter -= int32(deltaMin[I]) >> 1
			center := int16(limit32(rshiftRound(int32(nlsf[I-1])+int32(nlsf[I]), 1), minCenter, maxCenter))
			nlsf[I-1] = center - int16(deltaMin[I]>>1)
			nlsf[I] = nlsf[I-1] + deltaMin[I]
		}
	}
	// Fall back: sort and enforce the minimum distances.
	for i := 1; i < L; i++ {
		v := nlsf[i]
		j := i - 1
		for ; j >= 0 && v < nlsf[j]; j-- {
			nlsf[j+1] = nlsf[j]
		}
		nlsf[j+1] = v
	}
	nlsf[0] = int16(max(int32(nlsf[0]), int32(deltaMin[0])))
	for i := 1; i < L; i++ {
		nlsf[i] = int16(max(int32(nlsf[i]), addSat16(int32(nlsf[i-1]), int32(deltaMin[i]))))
	}
	nlsf[L-1] = int16(min(int32(nlsf[L-1]), (1<<15)-int32(deltaMin[L])))
	for i := L - 2; i >= 0; i-- {
		nlsf[i] = int16(min(int32(nlsf[i]), int32(nlsf[i+1])-int32(deltaMin[i+1])))
	}
}

var (
	nlsf2aOrdering16 = [16]int{0, 15, 8, 7, 4, 11, 12, 3, 2, 13, 10, 5, 6, 9, 14, 1}
	nlsf2aOrdering10 = [10]int{0, 9, 6, 3, 4, 5, 8, 1, 2, 7}
)

func nlsf2aFindPoly(out []int32, cLSF []int32, dd int) {
	const qa = 16
	out[0] = 1 << qa
	out[1] = -cLSF[0]
	for k := 1; k < dd; k++ {
		ftmp := cLSF[2*k]
		out[k+1] = out[k-1]<<1 - int32(rshiftRound64(smull(ftmp, out[k]), qa))
		for n := k; n > 1; n-- {
			out[n] += out[n-2] - int32(rshiftRound64(smull(ftmp, out[n-1]), qa))
		}
		out[1] -= ftmp
	}
}

func nlsf2a(aQ12 []int16, nlsf []int16, d int) {
	const qa = 16
	var ordering []int
	if d == 16 {
		ordering = nlsf2aOrdering16[:]
	} else {
		ordering = nlsf2aOrdering10[:]
	}
	var cosLSF [silkMaxLPCOrder]int32
	for k := 0; k < d; k++ {
		fInt := int32(nlsf[k]) >> (15 - 7)
		fFrac := int32(nlsf[k]) - fInt<<(15-7)
		cosVal := int32(silkLSFCosTabFIXQ12[fInt])
		delta := int32(silkLSFCosTabFIXQ12[fInt+1]) - cosVal
		cosLSF[ordering[k]] = rshiftRound(cosVal<<8+delta*fFrac, 20-qa)
	}
	dd := d >> 1
	var P, Q [silkMaxLPCOrder/2 + 1]int32
	nlsf2aFindPoly(P[:], cosLSF[0:], dd)
	nlsf2aFindPoly(Q[:], cosLSF[1:], dd)
	var a32 [silkMaxLPCOrder]int32
	for k := 0; k < dd; k++ {
		pt := P[k+1] + P[k]
		qt := Q[k+1] - Q[k]
		a32[k] = -qt - pt
		a32[d-k-1] = qt - pt
	}
	// silk_LPC_fit
	i := 0
	for ; i < 10; i++ {
		maxabs := int32(0)
		idx := 0
		for k := 0; k < d; k++ {
			if a := abs32(a32[k]); a > maxabs {
				maxabs = a
				idx = k
			}
		}
		maxabs = rshiftRound(maxabs, qa+1-12)
		if maxabs > 32767 {
			maxabs = min(maxabs, 163838)
			scQ16 := int32(65470) - ((maxabs-32767)<<14)/((maxabs*int32(idx+1))>>2)
			bwexpander32(a32[:d], scQ16)
		} else {
			break
		}
	}
	if i == 10 {
		for k := 0; k < d; k++ {
			aQ12[k] = int16(sat16(rshiftRound(a32[k], qa+1-12)))
			a32[k] = int32(aQ12[k]) << (qa + 1 - 12)
		}
	} else {
		for k := 0; k < d; k++ {
			aQ12[k] = int16(rshiftRound(a32[k], qa+1-12))
		}
	}
	for i := 0; i < maxLPCStabilizeIters; i++ {
		if lpcInversePredGain(aQ12[:d]) < 107374 { // 1/1e4 in Q30
			bwexpander32(a32[:d], 65536-int32(2)<<uint(i))
			for k := 0; k < d; k++ {
				aQ12[k] = int16(rshiftRound(a32[k], qa+1-12))
			}
		} else {
			break
		}
	}
}

func bwexpander32(ar []int32, chirpQ16 int32) {
	d := len(ar)
	cm1 := chirpQ16 - 65536
	for i := 0; i < d-1; i++ {
		ar[i] = smulww(chirpQ16, ar[i])
		chirpQ16 += rshiftRound(chirpQ16*cm1, 16)
	}
	ar[d-1] = smulww(chirpQ16, ar[d-1])
}

func bwexpander(ar []int16, chirpQ16 int32) {
	d := len(ar)
	cm1 := chirpQ16 - 65536
	for i := 0; i < d-1; i++ {
		ar[i] = int16(rshiftRound(chirpQ16*int32(ar[i]), 16))
		chirpQ16 += rshiftRound(chirpQ16*cm1, 16)
	}
	ar[d-1] = int16(rshiftRound(chirpQ16*int32(ar[d-1]), 16))
}

// lpcInversePredGain returns the inverse prediction gain in Q30, or 0 for
// an unstable filter (silk/LPC_inv_pred_gain.c with the RFC 8251 fix).
func lpcInversePredGain(aQ12 []int16) int32 {
	const qa = 24
	const aLimit = 16773022 // SILK_FIX_CONST(0.99975, 24)
	order := len(aQ12)
	var A [silkMaxLPCOrder]int32
	dc := int32(0)
	for k := 0; k < order; k++ {
		dc += int32(aQ12[k])
		A[k] = int32(aQ12[k]) << (qa - 12)
	}
	if dc >= 4096 {
		return 0
	}
	invGain := int32(1 << 30)
	for k := order - 1; k > 0; k-- {
		if A[k] > aLimit || A[k] < -aLimit {
			return 0
		}
		rc := -(A[k] << (31 - qa))
		rcMult1 := int32(1<<30) - smmul(rc, rc)
		invGain = smmul(invGain, rcMult1) << 2
		if invGain < 107374 {
			return 0
		}
		mult2Q := 32 - clz32(abs32(rcMult1))
		rcMult2 := inverse32VarQ(rcMult1, int(mult2Q)+30)
		for n := 0; n < (k+1)>>1; n++ {
			t1 := A[n]
			t2 := A[k-n-1]
			v := rshiftRound64(smull(subSat32(t1, int32(rshiftRound64(smull(t2, rc), 31))), rcMult2), uint(mult2Q))
			if v > 0x7FFFFFFF || v < -0x80000000 {
				return 0
			}
			A[n] = int32(v)
			v = rshiftRound64(smull(subSat32(t2, int32(rshiftRound64(smull(t1, rc), 31))), rcMult2), uint(mult2Q))
			if v > 0x7FFFFFFF || v < -0x80000000 {
				return 0
			}
			A[k-n-1] = int32(v)
		}
	}
	if A[0] > aLimit || A[0] < -aLimit {
		return 0
	}
	rc := -(A[0] << (31 - qa))
	rcMult1 := int32(1<<30) - smmul(rc, rc)
	invGain = smmul(invGain, rcMult1) << 2
	if invGain < 107374 {
		return 0
	}
	return invGain
}

func gainsDequant(gainQ16 []int32, ind []int, prevInd *int, conditional bool, nbSubfr int) {
	const offset = (minQGainDB*128)/6 + 16*128
	const invScaleQ16 = (65536 * (((maxQGainDB - minQGainDB) * 128) / 6)) / (nLevelsQGain - 1)
	for k := 0; k < nbSubfr; k++ {
		if k == 0 && !conditional {
			*prevInd = max(ind[k], *prevInd-16)
		} else {
			indTmp := ind[k] + minDeltaGainQuant
			thr := 2*maxDeltaGainQuant - nLevelsQGain + *prevInd
			if indTmp > thr {
				*prevInd += indTmp<<1 - thr
			} else {
				*prevInd += indTmp
			}
		}
		*prevInd = limitInt(*prevInd, 0, nLevelsQGain-1)
		gainQ16[k] = log2lin(min(smulwb(invScaleQ16, int32(*prevInd))+offset, 3967))
	}
}

func (c *silkChannel) decodeParameters(ctrl *silkDecCtrl, cond int) {
	ind := &c.indices
	gainsDequant(ctrl.gainsQ16[:], ind.gainsIndices[:], &c.lastGainIndex, cond == codeConditionally, c.nbSubfr)
	var nlsf, nlsf0 [silkMaxLPCOrder]int16
	nlsfDecode(nlsf[:], ind.nlsfIndices[:], c.nlsfCB)
	nlsf2a(ctrl.predCoefQ12[1][:], nlsf[:], c.lpcOrder)
	if c.firstFrameAfterReset {
		ind.nlsfInterpCoefQ2 = 4
	}
	if ind.nlsfInterpCoefQ2 < 4 {
		for i := 0; i < c.lpcOrder; i++ {
			nlsf0[i] = c.prevNLSFQ15[i] + int16((int32(ind.nlsfInterpCoefQ2)*(int32(nlsf[i])-int32(c.prevNLSFQ15[i])))>>2)
		}
		nlsf2a(ctrl.predCoefQ12[0][:], nlsf0[:], c.lpcOrder)
	} else {
		copy(ctrl.predCoefQ12[0][:c.lpcOrder], ctrl.predCoefQ12[1][:c.lpcOrder])
	}
	copy(c.prevNLSFQ15[:c.lpcOrder], nlsf[:c.lpcOrder])
	if c.lossCnt != 0 {
		bwexpander(ctrl.predCoefQ12[0][:c.lpcOrder], bweAfterLossQ16)
		bwexpander(ctrl.predCoefQ12[1][:c.lpcOrder], bweAfterLossQ16)
	}
	if ind.signalType == typeVoiced {
		decodePitch(ind.lagIndex, ind.contourIndex, ctrl.pitchL[:], c.fsKHz, c.nbSubfr)
		var cbk []int8
		switch ind.perIndex {
		case 0:
			cbk = silkLTPGainVQ0[:]
		case 1:
			cbk = silkLTPGainVQ1[:]
		default:
			cbk = silkLTPGainVQ2[:]
		}
		for k := 0; k < c.nbSubfr; k++ {
			ix := ind.ltpIndex[k]
			for i := 0; i < silkLTPOrder; i++ {
				ctrl.ltpCoefQ14[k*silkLTPOrder+i] = int16(int32(cbk[ix*silkLTPOrder+i]) << 7)
			}
		}
		ctrl.ltpScaleQ14 = int32(silkLTPScalesTableQ14[ind.ltpScaleIndex])
	} else {
		ctrl.pitchL = [silkMaxNbSubfr]int{}
		ctrl.ltpCoefQ14 = [len(ctrl.ltpCoefQ14)]int16{}
		ind.perIndex = 0
		ctrl.ltpScaleQ14 = 0
	}
}

func decodePitch(lagIndex, contourIndex int, pitchLags []int, fsKHz, nbSubfr int) {
	var cb []int8
	var cbkSize int
	if fsKHz == 8 {
		if nbSubfr == silkMaxNbSubfr {
			cb, cbkSize = silkCBLagsStage2[:], 11
		} else {
			cb, cbkSize = silkCBLagsStage2_10ms[:], 3
		}
	} else {
		if nbSubfr == silkMaxNbSubfr {
			cb, cbkSize = silkCBLagsStage3[:], 34
		} else {
			cb, cbkSize = silkCBLagsStage3_10ms[:], 12
		}
	}
	minLag := 2 * fsKHz
	maxLag := 18 * fsKHz
	lag := minLag + lagIndex
	for k := 0; k < nbSubfr; k++ {
		pitchLags[k] = limitInt(lag+int(cb[k*cbkSize+contourIndex]), minLag, maxLag)
	}
}

func lpcAnalysisFilter(out, in, B []int16, length, d int) {
	for ix := d; ix < length; ix++ {
		p := ix - 1
		acc := smulbb(int32(in[p]), int32(B[0]))
		for j := 1; j < d; j++ {
			acc = int32(uint32(acc) + uint32(smulbb(int32(in[p-j]), int32(B[j]))))
		}
		acc = int32(uint32(int32(in[p+1])<<12) - uint32(acc))
		out[ix] = int16(sat16(rshiftRound(acc, 12)))
	}
	clear(out[:d])
}

func (c *silkChannel) decodeCore(ctrl *silkDecCtrl, xq []int16, pulses []int) {
	ind := &c.indices
	var sLTP [silkMaxFrameLength]int16
	var sLTPQ15 [2 * silkMaxFrameLength]int32
	var resQ14 [silkMaxSubfrLength]int32
	var sLPC [silkMaxSubfrLength + silkMaxLPCOrder]int32
	var aTmp [silkMaxLPCOrder]int16

	offsetQ10 := int32(silkQuantizationOffsetsQ10[(ind.signalType>>1)*2+ind.quantOffsetType])
	nlsfInterp := ind.nlsfInterpCoefQ2 < 4
	randSeed := int32(ind.seed)
	for i := 0; i < c.frameLength; i++ {
		randSeed = silkRand(randSeed)
		e := int32(pulses[i]) << 14
		if e > 0 {
			e -= quantLevelAdjustQ10 << 4
		} else if e < 0 {
			e += quantLevelAdjustQ10 << 4
		}
		e += offsetQ10 << 4
		if randSeed < 0 {
			e = -e
		}
		c.excQ14[i] = e
		randSeed = int32(uint32(randSeed) + uint32(int32(pulses[i])))
	}
	copy(sLPC[:silkMaxLPCOrder], c.sLPCQ14Buf[:])
	pexc := 0
	pxq := 0
	sLTPBufIdx := c.ltpMemLength
	lag := 0
	for k := 0; k < c.nbSubfr; k++ {
		A := ctrl.predCoefQ12[k>>1][:]
		copy(aTmp[:c.lpcOrder], A[:c.lpcOrder])
		B := ctrl.ltpCoefQ14[k*silkLTPOrder : k*silkLTPOrder+silkLTPOrder]
		signalType := ind.signalType
		gainQ10 := ctrl.gainsQ16[k] >> 6
		invGainQ31 := inverse32VarQ(ctrl.gainsQ16[k], 47)
		gainAdjQ16 := int32(1 << 16)
		if ctrl.gainsQ16[k] != c.prevGainQ16 {
			gainAdjQ16 = div32VarQ(c.prevGainQ16, ctrl.gainsQ16[k], 16)
			for i := 0; i < silkMaxLPCOrder; i++ {
				sLPC[i] = smulww(gainAdjQ16, sLPC[i])
			}
		}
		c.prevGainQ16 = ctrl.gainsQ16[k]
		if c.lossCnt != 0 && c.prevSignalType == typeVoiced && ind.signalType != typeVoiced && k < silkMaxNbSubfr/2 {
			for i := range B {
				B[i] = 0
			}
			B[silkLTPOrder/2] = 4096 // 0.25 in Q14
			signalType = typeVoiced
			ctrl.pitchL[k] = c.lagPrev
		}
		var pres []int32
		if signalType == typeVoiced {
			lag = ctrl.pitchL[k]
			if k == 0 || (k == 2 && nlsfInterp) {
				startIdx := c.ltpMemLength - lag - c.lpcOrder - silkLTPOrder/2
				if startIdx < 0 {
					startIdx = 0 // corrupt stream guard
				}
				if k == 2 {
					copy(c.outBuf[c.ltpMemLength:], xq[:2*c.subfrLength])
				}
				lpcAnalysisFilter(sLTP[startIdx:], c.outBuf[startIdx+k*c.subfrLength:], A, c.ltpMemLength-startIdx, c.lpcOrder)
				if k == 0 {
					invGainQ31 = smulwb(invGainQ31, ctrl.ltpScaleQ14) << 2
				}
				for i := 0; i < lag+silkLTPOrder/2; i++ {
					sLTPQ15[sLTPBufIdx-i-1] = smulwb(invGainQ31, int32(sLTP[c.ltpMemLength-i-1]))
				}
			} else if gainAdjQ16 != 1<<16 {
				for i := 0; i < lag+silkLTPOrder/2; i++ {
					sLTPQ15[sLTPBufIdx-i-1] = smulww(gainAdjQ16, sLTPQ15[sLTPBufIdx-i-1])
				}
			}
			pred := sLTPBufIdx - lag + silkLTPOrder/2
			pres = resQ14[:c.subfrLength]
			for i := 0; i < c.subfrLength; i++ {
				ltp := int32(2)
				ltp = smlawb(ltp, sLTPQ15[pred], int32(B[0]))
				ltp = smlawb(ltp, sLTPQ15[pred-1], int32(B[1]))
				ltp = smlawb(ltp, sLTPQ15[pred-2], int32(B[2]))
				ltp = smlawb(ltp, sLTPQ15[pred-3], int32(B[3]))
				ltp = smlawb(ltp, sLTPQ15[pred-4], int32(B[4]))
				pred++
				pres[i] = c.excQ14[pexc+i] + ltp<<1
				sLTPQ15[sLTPBufIdx] = pres[i] << 1
				sLTPBufIdx++
			}
		} else {
			pres = c.excQ14[pexc : pexc+c.subfrLength]
		}
		for i := 0; i < c.subfrLength; i++ {
			lpc := int32(c.lpcOrder >> 1)
			for j := 0; j < c.lpcOrder; j++ {
				lpc = smlawb(lpc, sLPC[silkMaxLPCOrder+i-j-1], int32(aTmp[j]))
			}
			sLPC[silkMaxLPCOrder+i] = addSat32(pres[i], lshiftSat32(lpc, 4))
			xq[pxq+i] = int16(sat16(rshiftRound(smulww(sLPC[silkMaxLPCOrder+i], gainQ10), 8)))
		}
		copy(sLPC[:silkMaxLPCOrder], sLPC[c.subfrLength:c.subfrLength+silkMaxLPCOrder])
		pexc += c.subfrLength
		pxq += c.subfrLength
	}
	copy(c.sLPCQ14Buf[:], sLPC[:silkMaxLPCOrder])
}

// --- PLC (silk/PLC.c) ---

var (
	harmAttQ15            = [2]int32{32440, 31130}
	plcRandAttenuateVQ15  = [2]int32{31130, 26214}
	plcRandAttenuateUVQ15 = [2]int32{32440, 29491}
)

func (c *silkChannel) plcReset() {
	c.plc.pitchLQ8 = int32(c.frameLength) << (8 - 1)
	c.plc.prevGainQ16 = [2]int32{65536, 65536}
	c.plc.subfrLength = 20
	c.plc.nbSubfr = 2
}

func (c *silkChannel) plcUpdate(ctrl *silkDecCtrl) {
	if c.fsKHz != c.plc.fsKHz {
		c.plcReset()
		c.plc.fsKHz = c.fsKHz
	}
	p := &c.plc
	c.prevSignalType = c.indices.signalType
	ltpGain := int32(0)
	if c.indices.signalType == typeVoiced {
		for j := 0; j*c.subfrLength < ctrl.pitchL[c.nbSubfr-1]; j++ {
			if j == c.nbSubfr {
				break
			}
			tmp := int32(0)
			for i := 0; i < silkLTPOrder; i++ {
				tmp += int32(ctrl.ltpCoefQ14[(c.nbSubfr-1-j)*silkLTPOrder+i])
			}
			if tmp > ltpGain {
				ltpGain = tmp
				copy(p.ltpCoefQ14[:], ctrl.ltpCoefQ14[(c.nbSubfr-1-j)*silkLTPOrder:])
				p.pitchLQ8 = int32(ctrl.pitchL[c.nbSubfr-1-j]) << 8
			}
		}
		p.ltpCoefQ14 = [silkLTPOrder]int16{}
		p.ltpCoefQ14[silkLTPOrder/2] = int16(ltpGain)
		if ltpGain < 11469 {
			scale := (int32(11469) << 10) / max(ltpGain, 1)
			for i := range p.ltpCoefQ14 {
				p.ltpCoefQ14[i] = int16(smulbb(int32(p.ltpCoefQ14[i]), scale) >> 10)
			}
		} else if ltpGain > 15565 {
			scale := (int32(15565) << 14) / max(ltpGain, 1)
			for i := range p.ltpCoefQ14 {
				p.ltpCoefQ14[i] = int16(smulbb(int32(p.ltpCoefQ14[i]), scale) >> 14)
			}
		}
	} else {
		p.pitchLQ8 = int32(c.fsKHz*18) << 8
		p.ltpCoefQ14 = [silkLTPOrder]int16{}
	}
	copy(p.prevLPCQ12[:c.lpcOrder], ctrl.predCoefQ12[1][:c.lpcOrder])
	p.prevLTPScaleQ14 = int16(ctrl.ltpScaleQ14)
	copy(p.prevGainQ16[:], ctrl.gainsQ16[c.nbSubfr-2:c.nbSubfr])
	p.subfrLength = c.subfrLength
	p.nbSubfr = c.nbSubfr
}

func sumSqrShift(x []int16) (int32, int) {
	length := len(x)
	shft := 31 - int(clz32(int32(length)))
	nrg := int32(length)
	i := 0
	for ; i < length-1; i += 2 {
		t := uint32(smulbb(int32(x[i]), int32(x[i]))) + uint32(smulbb(int32(x[i+1]), int32(x[i+1])))
		nrg = int32(uint32(nrg) + t>>uint(shft))
	}
	if i < length {
		t := uint32(smulbb(int32(x[i]), int32(x[i])))
		nrg = int32(uint32(nrg) + t>>uint(shft))
	}
	shft = max(0, shft+3-int(clz32(nrg)))
	nrg = 0
	i = 0
	for ; i < length-1; i += 2 {
		t := uint32(smulbb(int32(x[i]), int32(x[i]))) + uint32(smulbb(int32(x[i+1]), int32(x[i+1])))
		nrg = int32(uint32(nrg) + t>>uint(shft))
	}
	if i < length {
		t := uint32(smulbb(int32(x[i]), int32(x[i])))
		nrg = int32(uint32(nrg) + t>>uint(shft))
	}
	return nrg, shft
}

func (c *silkChannel) plcConceal(ctrl *silkDecCtrl, frame []int16) {
	p := &c.plc
	var excBuf [2 * silkMaxSubfrLength]int16
	var aQ12 [silkMaxLPCOrder]int16
	var sLTP [silkMaxFrameLength]int16
	var sLTPQ14 [2 * silkMaxFrameLength]int32
	prevGainQ10 := [2]int32{p.prevGainQ16[0] >> 6, p.prevGainQ16[1] >> 6}
	if c.firstFrameAfterReset {
		p.prevLPCQ12 = [silkMaxLPCOrder]int16{}
	}
	// libopus >= 1.3 (silk_PLC_energy) measures the energies with the
	// decoder's current subframe layout, not the one saved in the PLC state.
	sl, nb := c.subfrLength, c.nbSubfr
	for k := 0; k < 2; k++ {
		for i := 0; i < sl; i++ {
			excBuf[k*sl+i] = int16(sat16(smulww(c.excQ14[i+(k+nb-2)*sl], prevGainQ10[k]) >> 8))
		}
	}
	e1, s1 := sumSqrShift(excBuf[:sl])
	e2, s2 := sumSqrShift(excBuf[sl : 2*sl])
	var randOff int
	if e1>>uint(s2) < e2>>uint(s1) {
		randOff = max(0, (p.nbSubfr-1)*p.subfrLength-128)
	} else {
		randOff = max(0, p.nbSubfr*p.subfrLength-128)
	}
	randBuf := c.excQ14[randOff:]
	B := p.ltpCoefQ14[:]
	randScale := int32(p.randScaleQ14)
	lc := min(1, c.lossCnt)
	harmGain := harmAttQ15[lc]
	randGain := plcRandAttenuateUVQ15[lc]
	if c.prevSignalType == typeVoiced {
		randGain = plcRandAttenuateVQ15[lc]
	}
	bwexpander(p.prevLPCQ12[:c.lpcOrder], 64881) // BWE_COEF 0.99
	copy(aQ12[:c.lpcOrder], p.prevLPCQ12[:c.lpcOrder])
	if c.lossCnt == 0 {
		randScale = 1 << 14
		if c.prevSignalType == typeVoiced {
			for i := 0; i < silkLTPOrder; i++ {
				randScale -= int32(B[i])
			}
			randScale = max(3277, randScale)
			randScale = int32(int16(smulbb(randScale, int32(p.prevLTPScaleQ14)) >> 14))
		} else {
			invGain := lpcInversePredGain(p.prevLPCQ12[:c.lpcOrder])
			down := min(int32(1<<30)>>3, invGain)
			down = max(int32(1<<30)>>8, down)
			down <<= 3
			randGain = smulwb(down, randGain) >> 14
		}
	}
	randSeed := p.randSeed
	lag := int(rshiftRound(p.pitchLQ8, 8))
	sLTPBufIdx := c.ltpMemLength
	idx := c.ltpMemLength - lag - c.lpcOrder - silkLTPOrder/2
	if idx < 0 {
		idx = 0
	}
	lpcAnalysisFilter(sLTP[idx:], c.outBuf[idx:], aQ12[:], c.ltpMemLength-idx, c.lpcOrder)
	invGainQ30 := inverse32VarQ(p.prevGainQ16[1], 46)
	invGainQ30 = min(invGainQ30, 0x7FFFFFFF>>1)
	for i := idx + c.lpcOrder; i < c.ltpMemLength; i++ {
		sLTPQ14[i] = smulwb(invGainQ30, int32(sLTP[i]))
	}
	for k := 0; k < c.nbSubfr; k++ {
		pred := sLTPBufIdx - lag + silkLTPOrder/2
		for i := 0; i < c.subfrLength; i++ {
			ltp := int32(2)
			ltp = smlawb(ltp, sLTPQ14[pred], int32(B[0]))
			ltp = smlawb(ltp, sLTPQ14[pred-1], int32(B[1]))
			ltp = smlawb(ltp, sLTPQ14[pred-2], int32(B[2]))
			ltp = smlawb(ltp, sLTPQ14[pred-3], int32(B[3]))
			ltp = smlawb(ltp, sLTPQ14[pred-4], int32(B[4]))
			pred++
			randSeed = silkRand(randSeed)
			ri := (randSeed >> 25) & 127
			sLTPQ14[sLTPBufIdx] = smlawb(ltp, randBuf[ri], randScale) << 2
			sLTPBufIdx++
		}
		for j := 0; j < silkLTPOrder; j++ {
			B[j] = int16(smulbb(harmGain, int32(B[j])) >> 15)
		}
		randScale = smulbb(randScale, randGain) >> 15
		p.pitchLQ8 = smlawb(p.pitchLQ8, p.pitchLQ8, 655)
		p.pitchLQ8 = min(p.pitchLQ8, int32(18*c.fsKHz)<<8)
		lag = int(rshiftRound(p.pitchLQ8, 8))
	}
	sLPC := sLTPQ14[c.ltpMemLength-silkMaxLPCOrder:]
	copy(sLPC[:silkMaxLPCOrder], c.sLPCQ14Buf[:])
	for i := 0; i < c.frameLength; i++ {
		lpc := int32(c.lpcOrder >> 1)
		for j := 0; j < c.lpcOrder; j++ {
			lpc = smlawb(lpc, sLPC[silkMaxLPCOrder+i-j-1], int32(aQ12[j]))
		}
		sLPC[silkMaxLPCOrder+i] = addSat32(sLPC[silkMaxLPCOrder+i], lshiftSat32(lpc, 4))
		frame[i] = int16(sat16(rshiftRound(smulww(sLPC[silkMaxLPCOrder+i], prevGainQ10[1]), 8)))
	}
	copy(c.sLPCQ14Buf[:], sLPC[c.frameLength:c.frameLength+silkMaxLPCOrder])
	p.randSeed = randSeed
	p.randScaleQ14 = int16(randScale)
	for i := 0; i < silkMaxNbSubfr; i++ {
		ctrl.pitchL[i] = lag
	}
}

func (c *silkChannel) plcGlueFrames(frame []int16, length int) {
	p := &c.plc
	if c.lossCnt != 0 {
		p.concEnergy, p.concEnergyShift = sumSqrShift(frame[:length])
		p.lastFrameLost = true
		return
	}
	if p.lastFrameLost {
		energy, shift := sumSqrShift(frame[:length])
		if shift > p.concEnergyShift {
			p.concEnergy >>= uint(shift - p.concEnergyShift)
		} else if shift < p.concEnergyShift {
			energy >>= uint(p.concEnergyShift - shift)
		}
		if energy > p.concEnergy {
			lz := clz32(p.concEnergy) - 1
			p.concEnergy <<= uint(lz)
			energy >>= uint(max(24-lz, 0))
			fracQ24 := p.concEnergy / max(energy, 1)
			gainQ16 := sqrtApprox(fracQ24) << 4
			slopeQ16 := ((1 << 16) - gainQ16) / int32(length)
			slopeQ16 <<= 2
			for i := 0; i < length; i++ {
				frame[i] = int16(smulwb(gainQ16, int32(frame[i])))
				gainQ16 += slopeQ16
				if gainQ16 > 1<<16 {
					break
				}
			}
		}
	}
	p.lastFrameLost = false
}

// --- CNG (silk/CNG.c) ---

func (c *silkChannel) cngReset() {
	step := int32(32767) / int32(c.lpcOrder+1)
	acc := int32(0)
	for i := 0; i < c.lpcOrder; i++ {
		acc += step
		c.cng.smthNLSFQ15[i] = int16(acc)
	}
	c.cng.smthGainQ16 = 0
	c.cng.randSeed = 3176576
}

func (c *silkChannel) cngApply(ctrl *silkDecCtrl, frame []int16, length int) {
	cng := &c.cng
	if c.fsKHz != cng.fsKHz {
		c.cngReset()
		cng.fsKHz = c.fsKHz
	}
	if c.lossCnt == 0 && c.prevSignalType == typeNoVoiceActivity {
		for i := 0; i < c.lpcOrder; i++ {
			cng.smthNLSFQ15[i] += int16(smulwb(int32(c.prevNLSFQ15[i])-int32(cng.smthNLSFQ15[i]), cngNLSFSmthQ16))
		}
		maxGain := int32(0)
		subfr := 0
		for i := 0; i < c.nbSubfr; i++ {
			if ctrl.gainsQ16[i] > maxGain {
				maxGain = ctrl.gainsQ16[i]
				subfr = i
			}
		}
		copy(cng.excBufQ14[c.subfrLength:], cng.excBufQ14[:(c.nbSubfr-1)*c.subfrLength])
		copy(cng.excBufQ14[:c.subfrLength], c.excQ14[subfr*c.subfrLength:])
		for i := 0; i < c.nbSubfr; i++ {
			cng.smthGainQ16 += smulwb(ctrl.gainsQ16[i]-cng.smthGainQ16, cngGainSmthQ16)
			if smulww(cng.smthGainQ16, cngGainSmthThresholdQ16) > ctrl.gainsQ16[i] {
				cng.smthGainQ16 = ctrl.gainsQ16[i]
			}
		}
	}
	if c.lossCnt != 0 {
		var sig [silkMaxFrameLength + silkMaxLPCOrder]int32
		var aQ12 [silkMaxLPCOrder]int16
		gainQ16 := smulww(int32(c.plc.randScaleQ14), c.plc.prevGainQ16[1])
		if gainQ16 >= 1<<21 || cng.smthGainQ16 > 1<<23 {
			gainQ16 = smultt(gainQ16, gainQ16)
			gainQ16 = smultt(cng.smthGainQ16, cng.smthGainQ16) - gainQ16<<5
			gainQ16 = sqrtApprox(gainQ16) << 16
		} else {
			gainQ16 = smulww(gainQ16, gainQ16)
			gainQ16 = smulww(cng.smthGainQ16, cng.smthGainQ16) - gainQ16<<5
			gainQ16 = sqrtApprox(gainQ16) << 8
		}
		gainQ10 := gainQ16 >> 6
		mask := cngBufMaskMax
		for mask > length {
			mask >>= 1
		}
		seed := cng.randSeed
		for i := 0; i < length; i++ {
			seed = silkRand(seed)
			idx := int((seed >> 24) & int32(mask))
			sig[silkMaxLPCOrder+i] = cng.excBufQ14[idx]
		}
		cng.randSeed = seed
		nlsf2a(aQ12[:], cng.smthNLSFQ15[:], c.lpcOrder)
		copy(sig[:silkMaxLPCOrder], cng.synthState[:])
		for i := 0; i < length; i++ {
			sum := int32(c.lpcOrder >> 1)
			for j := 0; j < c.lpcOrder; j++ {
				sum = smlawb(sum, sig[silkMaxLPCOrder+i-j-1], int32(aQ12[j]))
			}
			sig[silkMaxLPCOrder+i] = addSat32(sig[silkMaxLPCOrder+i], lshiftSat32(sum, 4))
			frame[i] = int16(addSat16(int32(frame[i]), sat16(rshiftRound(smulww(sig[silkMaxLPCOrder+i], gainQ10), 8))))
		}
		copy(cng.synthState[:], sig[length:length+silkMaxLPCOrder])
	} else {
		cng.synthState = [silkMaxLPCOrder]int32{}
	}
}
