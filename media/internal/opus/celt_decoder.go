package opus

import (
	"errors"
	"math"
)

// CELT decoder (RFC 6716 section 4.3), a float port of the decoder half of
// celt/celt.c.

const (
	decodeBufferSize = 2048
	maxPeriod        = 1024
	combMinPeriod    = 15
	lpcOrder         = 24
)

var (
	trimICDF   = []uint8{126, 124, 119, 109, 87, 41, 19, 9, 4, 2, 0}
	spreadICDF = []uint8{25, 23, 2, 0}
	tapsetICDF = []uint8{2, 1, 0}

	predCoef = [4]float32{29440 / 32768., 26112 / 32768., 21248 / 32768., 16384 / 32768.}
	betaCoef = [4]float32{30147 / 32768., 22282 / 32768., 12124 / 32768., 6554 / 32768.}

	smallEnergyICDF = []uint8{2, 1, 0}

	combGains = [3][3]float32{
		{0.3066406250, 0.2170410156, 0.1296386719},
		{0.4638671875, 0.2680664062, 0},
		{0.7998046875, 0.1000976562, 0},
	}
)

const (
	betaIntra   = float32(4915 / 32768.)
	preemphCoef = float32(0.85000610)
)

var errCorrupt = errors.New("opus: corrupt packet")

type celtDecoder struct {
	channels       int // output channels
	streamChannels int
	downsample     int
	start, end     int

	rng                 uint32
	lastPitchIndex      int
	lossDuration        int
	plcDuration         int
	lastFrameType       int
	skipPLC             bool
	prefilterAndFold    bool
	postfilterPeriod    int
	postfilterPeriodOld int
	postfilterGain      float32
	postfilterGainOld   float32
	postfilterTapset    int
	postfilterTapsetOld int
	preemphMem          [2]float32
	decodeMem           [2][decodeBufferSize + celtOverlap]float32
	lpc                 [2][lpcOrder]float32
	oldBandE            [2 * nbEBands]float32
	oldLogE             [2 * nbEBands]float32
	oldLogE2            [2 * nbEBands]float32
	backgroundLogE      [2 * nbEBands]float32
	disableInv          bool
	scratchX, scratchF  [2 * 960]float32
	norm                [2 * 8 * 100]float32
	lowbandScratch      [8 * 22]float32
	bands               bandCtx
	alloc               allocScratch
	imdctTmp            [960 + celtOverlap]float32
	mdctScratch         mdctScratch
}

func newCELTDecoder(sampleRate, channels int) *celtDecoder {
	st := &celtDecoder{channels: channels, streamChannels: channels}
	switch sampleRate {
	case 48000:
		st.downsample = 1
	case 24000:
		st.downsample = 2
	case 16000:
		st.downsample = 3
	case 12000:
		st.downsample = 4
	case 8000:
		st.downsample = 6
	}
	st.start = 0
	st.end = nbEBands
	st.reset()
	return st
}

func (st *celtDecoder) reset() {
	st.rng = 0
	st.lastPitchIndex = 0
	st.lossDuration = 0
	st.plcDuration = 0
	st.lastFrameType = frameNone
	st.skipPLC = true
	st.prefilterAndFold = false
	st.postfilterPeriod, st.postfilterPeriodOld = 0, 0
	st.postfilterGain, st.postfilterGainOld = 0, 0
	st.postfilterTapset, st.postfilterTapsetOld = 0, 0
	st.preemphMem = [2]float32{}
	st.decodeMem = [2][decodeBufferSize + celtOverlap]float32{}
	st.lpc = [2][lpcOrder]float32{}
	st.oldBandE = [2 * nbEBands]float32{}
	st.backgroundLogE = [2 * nbEBands]float32{}
	for i := range st.oldLogE {
		st.oldLogE[i] = -28
		st.oldLogE2[i] = -28
	}
}

// combFilter applies the pitch post-filter (celt.c comb_filter) to
// x[xOff:xOff+N] into y[yOff:], crossfading the first overlap samples from
// (T0, g0, tapset0) to (T1, g1, tapset1). x needs T+2 samples of history.
// y and x may be the same buffer (the filter is then recursive).
func combFilter(y []float32, yOff int, x []float32, xOff int, T0, T1, N int, g0, g1 float32, tapset0, tapset1 int, window []float32, overlap int) {
	if g0 == 0 && g1 == 0 {
		copy(y[yOff:yOff+N], x[xOff:xOff+N])
		return
	}
	T0 = imax(T0, combMinPeriod)
	T1 = imax(T1, combMinPeriod)
	g00 := g0 * combGains[tapset0][0]
	g01 := g0 * combGains[tapset0][1]
	g02 := g0 * combGains[tapset0][2]
	g10 := g1 * combGains[tapset1][0]
	g11 := g1 * combGains[tapset1][1]
	g12 := g1 * combGains[tapset1][2]
	if g0 == g1 && T0 == T1 && tapset0 == tapset1 {
		overlap = 0
	}
	if window == nil {
		overlap = 0
	}
	i := 0
	for ; i < overlap; i++ {
		f := window[i] * window[i]
		xi := xOff + i
		y[yOff+i] = x[xi] +
			(1-f)*g00*x[xi-T0] +
			(1-f)*g01*(x[xi-T0+1]+x[xi-T0-1]) +
			(1-f)*g02*(x[xi-T0+2]+x[xi-T0-2]) +
			f*g10*x[xi-T1] +
			f*g11*(x[xi-T1+1]+x[xi-T1-1]) +
			f*g12*(x[xi-T1+2]+x[xi-T1-2])
	}
	if g1 == 0 {
		copy(y[yOff+i:yOff+N], x[xOff+i:xOff+N])
		return
	}
	for ; i < N; i++ {
		xi := xOff + i
		y[yOff+i] = x[xi] +
			g10*x[xi-T1] +
			g11*(x[xi-T1+1]+x[xi-T1-1]) +
			g12*(x[xi-T1+2]+x[xi-T1-2])
	}
}

// computeInvMDCTs runs the IMDCT(s) for channel data freq (N per channel)
// into decodeMem at offset decodeBufferSize-N, overlap-adding the saved
// windowed tail, and stores the new tail at decodeBufferSize.
func (st *celtDecoder) computeInvMDCTs(freq []float32, shortBlocks, C, LM int) {
	N := shortMdctSize << uint(LM)
	x := st.imdctTmp[:N+celtOverlap]
	for c := 0; c < C; c++ {
		for j := 0; j < celtOverlap; j++ {
			x[j] = 0
		}
		B := 1
		N2 := N
		shift := maxLM - LM
		if shortBlocks != 0 {
			N2 = shortMdctSize
			B = shortBlocks
			shift = maxLM
		}
		m := getMDCT(shift)
		for b := 0; b < B; b++ {
			m.backward(freq[b+c*N2*B:], B, x, N2*b, window120[:], celtOverlap, &st.mdctScratch)
		}
		mem := st.decodeMem[c][:]
		out := decodeBufferSize - N
		// The previous frame's windowed tail sits at mem[out:out+overlap]
		// (the caller already shifted the buffer by N).
		for j := 0; j < celtOverlap; j++ {
			mem[out+j] = x[j] + mem[out+j]
		}
		for j := celtOverlap; j < N; j++ {
			mem[out+j] = x[j]
		}
		for j := 0; j < celtOverlap; j++ {
			mem[decodeBufferSize+j] = x[N+j]
		}
	}
}

func (st *celtDecoder) deemphasis(pcm []float32, N int, accum bool) {
	C := st.channels
	for c := 0; c < C; c++ {
		m := st.preemphMem[c]
		x := st.decodeMem[c][decodeBufferSize-N : decodeBufferSize]
		count := 0
		y := c
		for j := 0; j < N; j++ {
			tmp := x[j] + m + 1e-30
			m = preemphCoef * tmp
			if count == 0 {
				v := tmp * (1.0 / 32768)
				if accum {
					pcm[y] += v
				} else {
					pcm[y] = v
				}
			}
			count++
			if count == st.downsample {
				y += C
				count = 0
			}
		}
		st.preemphMem[c] = m
	}
}

// decode decodes one CELT frame of frameSize samples (at the output rate)
// into pcm (interleaved, st.channels). data==nil or len(data)<=1 runs the
// packet-loss concealment. dec, when non-nil, continues an existing range
// decoder (hybrid mode). When accum is set the output is added to pcm.
func (st *celtDecoder) decode(data []byte, pcm []float32, frameSize int, dec *rangeDecoder, accum bool) (int, error) {
	CC := st.channels
	C := st.streamChannels
	frameSize *= st.downsample
	LM := 0
	for LM = 0; LM <= maxLM; LM++ {
		if shortMdctSize<<uint(LM) == frameSize {
			break
		}
	}
	if LM > maxLM {
		return 0, errors.New("opus: bad CELT frame size")
	}
	M := 1 << uint(LM)
	if len(data) > 1275 {
		return 0, errCorrupt
	}
	N := M * shortMdctSize
	effEnd := st.end
	if effEnd > nbEBands {
		effEnd = nbEBands
	}
	if data == nil || len(data) <= 1 {
		st.decodeLost(pcm, N, LM, accum)
		return frameSize / st.downsample, nil
	}
	if st.lossDuration == 0 {
		st.skipPLC = false
	}
	var localDec rangeDecoder
	if dec == nil {
		localDec.init(data)
		dec = &localDec
	}
	X := st.scratchX[:C*N]
	for i := range X {
		X[i] = 0
	}
	if C == 1 {
		for i := 0; i < nbEBands; i++ {
			st.oldBandE[i] = max(st.oldBandE[i], st.oldBandE[nbEBands+i])
		}
	}
	totalBits := len(data) * 8
	tell := dec.tell()
	silence := 0
	if tell >= totalBits {
		silence = 1
	} else if tell == 1 {
		silence = dec.bitLogp(15)
	}
	if silence != 0 {
		tell = len(data) * 8
		dec.nbitsTotal += tell - dec.tell()
	}
	var postfilterGain float32
	postfilterPitch := 0
	postfilterTapset := 0
	if st.start == 0 && tell+16 <= totalBits {
		if dec.bitLogp(1) != 0 {
			octave := int(dec.uint(6))
			postfilterPitch = (16 << uint(octave)) + int(dec.bits(uint(4+octave))) - 1
			qg := int(dec.bits(3))
			if dec.tell()+2 <= totalBits {
				postfilterTapset = dec.icdf(tapsetICDF, 2)
			}
			postfilterGain = 0.09375 * float32(qg+1)
		}
		tell = dec.tell()
	}
	isTransient := 0
	if LM > 0 && tell+3 <= totalBits {
		isTransient = dec.bitLogp(3)
		tell = dec.tell()
	}
	shortBlocks := 0
	if isTransient != 0 {
		shortBlocks = M
	}
	intraEner := 0
	if tell+3 <= totalBits {
		intraEner = dec.bitLogp(3)
	}
	if intraEner == 0 && st.lossDuration != 0 {
		// Recovering from loss: make the energy prediction safe.
		missing := imin(10, st.lossDuration>>uint(LM))
		var safety float32
		if LM == 0 {
			safety = 1.5
		} else if LM == 1 {
			safety = 0.5
		}
		for c := 0; c < 2; c++ {
			for i := st.start; i < st.end; i++ {
				k := c*nbEBands + i
				if st.oldBandE[k] < max(st.oldLogE[k], st.oldLogE2[k]) {
					E0, E1, E2 := st.oldBandE[k], st.oldLogE[k], st.oldLogE2[k]
					slope := max(E1-E0, 0.5*(E2-E0))
					slope = min(slope, 2)
					E0 -= max(0, float32(1+missing)*slope)
					st.oldBandE[k] = max(-20, E0)
				} else {
					st.oldBandE[k] = min(min(st.oldBandE[k], st.oldLogE[k]), st.oldLogE2[k])
				}
				st.oldBandE[k] -= safety
			}
		}
	}
	st.unquantCoarseEnergy(intraEner, dec, C, LM)

	var tfRes, pulses, cap, offsets, finePriority, fineQuant [nbEBands]int
	st.tfDecode(isTransient, tfRes[:], LM, dec)

	tell = dec.tell()
	spread := spreadNormal
	if tell+4 <= totalBits {
		spread = dec.icdf(spreadICDF, 5)
	}
	for i := 0; i < nbEBands; i++ {
		w := int(eband5ms[i+1]-eband5ms[i]) << uint(LM)
		cap[i] = (int(cacheCaps50[nbEBands*(2*LM+C-1)+i]) + 64) * C * w >> 2
	}
	dynallocLogp := 6
	totalBits <<= bitRes
	tellF := dec.tellFrac()
	for i := st.start; i < st.end; i++ {
		width := C * int(eband5ms[i+1]-eband5ms[i]) << uint(LM)
		quanta := imin(width<<bitRes, imax(6<<bitRes, width))
		loopLogp := dynallocLogp
		boost := 0
		for tellF+(loopLogp<<bitRes) < totalBits && boost < cap[i] {
			flag := dec.bitLogp(uint(loopLogp))
			tellF = dec.tellFrac()
			if flag == 0 {
				break
			}
			boost += quanta
			totalBits -= quanta
			loopLogp = 1
		}
		offsets[i] = boost
		if boost > 0 {
			dynallocLogp = imax(2, dynallocLogp-1)
		}
	}
	allocTrim := 5
	if tellF+(6<<bitRes) <= totalBits {
		allocTrim = dec.icdf(trimICDF, 7)
	}
	bits := (len(data) * 8 << bitRes) - dec.tellFrac() - 1
	antiCollapseRsv := 0
	if isTransient != 0 && LM >= 2 && bits >= (LM+2)<<bitRes {
		antiCollapseRsv = 1 << bitRes
	}
	bits -= antiCollapseRsv
	var intensity, dualStereo, balance int
	codedBands := computeAllocation(st.start, st.end, offsets[:], cap[:], allocTrim, &intensity, &dualStereo,
		bits, &balance, pulses[:], fineQuant[:], finePriority[:], C, LM, dec, &st.alloc)

	st.unquantFineEnergy(fineQuant[:], dec, C)

	var collapseMasks [2 * nbEBands]uint8
	st.bands.d = dec
	st.bands.seed = st.rng
	var Y []float32
	if C == 2 {
		Y = X[N:]
	}
	st.bands.quantAllBands(st.start, st.end, X[:N], Y, collapseMasks[:], pulses[:], shortBlocks != 0, spread,
		dualStereo, intensity, tfRes[:], len(data)*(8<<bitRes)-antiCollapseRsv, balance, LM, codedBands,
		st.norm[:], st.lowbandScratch[:])
	st.rng = st.bands.seed

	antiCollapseOn := 0
	if antiCollapseRsv > 0 {
		antiCollapseOn = int(dec.bits(1))
	}
	st.unquantEnergyFinalise(fineQuant[:], finePriority[:], len(data)*8-dec.tell(), dec, C)
	if antiCollapseOn != 0 {
		antiCollapse(X, collapseMasks[:], LM, C, N, st.start, st.end, st.oldBandE[:], st.oldLogE[:], st.oldLogE2[:], pulses[:], st.rng)
	}

	freq := st.scratchF[:imax(CC, C)*N]
	if silence != 0 {
		for i := range freq {
			freq[i] = 0
		}
		for i := 0; i < C*nbEBands; i++ {
			st.oldBandE[i] = -28
		}
	} else {
		st.denormaliseBands(X, freq, effEnd, C, M)
	}
	for c := 0; c < CC; c++ {
		copy(st.decodeMem[c][:decodeBufferSize-N+celtOverlap], st.decodeMem[c][N:decodeBufferSize+celtOverlap])
	}
	if st.prefilterAndFold {
		st.prefilterAndFoldApply(N)
	}
	for c := 0; c < C; c++ {
		for i := 0; i < M*int(eband5ms[st.start]); i++ {
			freq[c*N+i] = 0
		}
		bound := M * int(eband5ms[effEnd])
		if st.downsample != 1 {
			bound = imin(bound, N/st.downsample)
		}
		for i := bound; i < N; i++ {
			freq[c*N+i] = 0
		}
	}
	if CC == 2 && C == 1 {
		copy(freq[N:2*N], freq[:N])
	}
	if CC == 1 && C == 2 {
		for i := 0; i < N; i++ {
			freq[i] = 0.5 * (freq[i] + freq[N+i])
		}
	}
	st.computeInvMDCTs(freq, shortBlocks, CC, LM)

	for c := 0; c < CC; c++ {
		st.postfilterPeriod = imax(st.postfilterPeriod, combMinPeriod)
		st.postfilterPeriodOld = imax(st.postfilterPeriodOld, combMinPeriod)
		mem := st.decodeMem[c][:]
		off := decodeBufferSize - N
		combFilter(mem, off, mem, off, st.postfilterPeriodOld, st.postfilterPeriod, shortMdctSize,
			st.postfilterGainOld, st.postfilterGain, st.postfilterTapsetOld, st.postfilterTapset, window120[:], celtOverlap)
		if LM != 0 {
			combFilter(mem, off+shortMdctSize, mem, off+shortMdctSize, st.postfilterPeriod, postfilterPitch, N-shortMdctSize,
				st.postfilterGain, postfilterGain, st.postfilterTapset, postfilterTapset, window120[:], celtOverlap)
		}
	}
	st.postfilterPeriodOld = st.postfilterPeriod
	st.postfilterGainOld = st.postfilterGain
	st.postfilterTapsetOld = st.postfilterTapset
	st.postfilterPeriod = postfilterPitch
	st.postfilterGain = postfilterGain
	st.postfilterTapset = postfilterTapset
	if LM != 0 {
		st.postfilterPeriodOld = st.postfilterPeriod
		st.postfilterGainOld = st.postfilterGain
		st.postfilterTapsetOld = st.postfilterTapset
	}
	if C == 1 {
		copy(st.oldBandE[nbEBands:], st.oldBandE[:nbEBands])
	}
	if isTransient == 0 {
		st.oldLogE2 = st.oldLogE
		st.oldLogE = st.oldBandE
	} else {
		for i := 0; i < 2*nbEBands; i++ {
			st.oldLogE[i] = min(st.oldLogE[i], st.oldBandE[i])
		}
	}
	maxBackgroundIncrease := float32(imin(160, st.lossDuration+M)) * 0.001
	for i := 0; i < 2*nbEBands; i++ {
		st.backgroundLogE[i] = min(st.backgroundLogE[i]+maxBackgroundIncrease, st.oldBandE[i])
	}
	for c := 0; c < 2; c++ {
		for i := 0; i < st.start; i++ {
			st.oldBandE[c*nbEBands+i] = 0
			st.oldLogE[c*nbEBands+i] = -28
			st.oldLogE2[c*nbEBands+i] = -28
		}
		for i := st.end; i < nbEBands; i++ {
			st.oldBandE[c*nbEBands+i] = 0
			st.oldLogE[c*nbEBands+i] = -28
			st.oldLogE2[c*nbEBands+i] = -28
		}
	}
	st.rng = dec.rng
	st.deemphasis(pcm, N, accum)
	st.lossDuration = 0
	st.plcDuration = 0
	st.lastFrameType = frameNormal
	st.prefilterAndFold = false
	if dec.tell() > 8*len(data) {
		return 0, errCorrupt
	}
	return frameSize / st.downsample, nil
}

func (st *celtDecoder) tfDecode(isTransient int, tfRes []int, LM int, dec *rangeDecoder) {
	budget := int(dec.storage) * 8
	tell := dec.tell()
	logp := 2
	if isTransient == 0 {
		logp = 4
	}
	tfSelectRsv := 0
	if LM > 0 && tell+logp+1 <= budget {
		tfSelectRsv = 1
	}
	budget -= tfSelectRsv
	tfChanged, curr := 0, 0
	for i := st.start; i < st.end; i++ {
		if tell+logp <= budget {
			curr ^= dec.bitLogp(uint(logp))
			tell = dec.tell()
			tfChanged |= curr
		}
		tfRes[i] = curr
		if isTransient != 0 {
			logp = 4
		} else {
			logp = 5
		}
	}
	tfSelect := 0
	if tfSelectRsv != 0 && tfSelectTable[LM*8+4*isTransient+0+tfChanged] != tfSelectTable[LM*8+4*isTransient+2+tfChanged] {
		tfSelect = dec.bitLogp(1)
	}
	for i := st.start; i < st.end; i++ {
		tfRes[i] = int(tfSelectTable[LM*8+4*isTransient+2*tfSelect+tfRes[i]])
	}
}

func (st *celtDecoder) unquantCoarseEnergy(intra int, dec *rangeDecoder, C, LM int) {
	prob := eProbModel[(LM*2+intra)*42:]
	var prev [2]float32
	var coef, beta float32
	if intra != 0 {
		coef = 0
		beta = betaIntra
	} else {
		beta = betaCoef[LM]
		coef = predCoef[LM]
	}
	budget := int(dec.storage) * 8
	for i := st.start; i < st.end; i++ {
		for c := 0; c < C; c++ {
			var qi int
			tell := dec.tell()
			if budget-tell >= 15 {
				pi := 2 * imin(i, 20)
				qi = dec.laplace(uint32(prob[pi])<<7, int(prob[pi+1])<<6)
			} else if budget-tell >= 2 {
				qi = dec.icdf(smallEnergyICDF, 2)
				qi = (qi >> 1) ^ -(qi & 1)
			} else if budget-tell >= 1 {
				qi = -dec.bitLogp(1)
			} else {
				qi = -1
			}
			q := float32(qi)
			e := &st.oldBandE[i+c*nbEBands]
			*e = max(-9, *e)
			tmp := coef*(*e) + prev[c] + q
			*e = tmp
			prev[c] = prev[c] + q - beta*q
		}
	}
}

func (st *celtDecoder) unquantFineEnergy(fineQuant []int, dec *rangeDecoder, C int) {
	for i := st.start; i < st.end; i++ {
		if fineQuant[i] <= 0 {
			continue
		}
		for c := 0; c < C; c++ {
			q2 := dec.bits(uint(fineQuant[i]))
			offset := (float32(q2)+0.5)*float32(int(1)<<uint(14-fineQuant[i]))*(1.0/16384) - 0.5
			st.oldBandE[i+c*nbEBands] += offset
		}
	}
}

func (st *celtDecoder) unquantEnergyFinalise(fineQuant, finePriority []int, bitsLeft int, dec *rangeDecoder, C int) {
	for prio := 0; prio < 2; prio++ {
		for i := st.start; i < st.end && bitsLeft >= C; i++ {
			if fineQuant[i] >= maxFineBits || finePriority[i] != prio {
				continue
			}
			for c := 0; c < C; c++ {
				q2 := dec.bits(1)
				offset := (float32(q2) - 0.5) * float32(int(1)<<uint(14-fineQuant[i]-1)) * (1.0 / 16384)
				st.oldBandE[i+c*nbEBands] += offset
				bitsLeft--
			}
		}
	}
}

// denormaliseBands converts the normalised shape X into MDCT coefficients
// using the decoded band energies (log2Amp + denormalise_bands).
func (st *celtDecoder) denormaliseBands(X, freq []float32, end, C, M int) {
	N := M * shortMdctSize
	for c := 0; c < C; c++ {
		f := freq[c*N : (c+1)*N]
		x := X[c*N:]
		for i := 0; i < M*int(eband5ms[st.start]); i++ {
			f[i] = 0
		}
		for i := st.start; i < end; i++ {
			lg := st.oldBandE[i+c*nbEBands] + eMeans[i]
			lg = min(32, lg)
			g := float32(math.Exp2(float64(lg)))
			for j := M * int(eband5ms[i]); j < M*int(eband5ms[i+1]); j++ {
				f[j] = x[j] * g
			}
		}
		for i := M * int(eband5ms[end]); i < N; i++ {
			f[i] = 0
		}
	}
}
