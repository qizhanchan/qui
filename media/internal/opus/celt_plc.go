package opus

import "math"

// CELT packet-loss concealment, a float port of celt_decode_lost() from
// libopus 1.6 (without the neural PLC): pitch-based periodic extension
// with LPC shaping for the first losses, then band-energy shaped noise.

const (
	frameNone        = 0
	frameNormal      = 1
	framePLCNoise    = 2
	framePLCPeriodic = 3

	plcPitchLagMax = 720
	plcPitchLagMin = 100
)

// prefilterAndFoldApply converts the raw extrapolated overlap left by the
// periodic PLC (now at decodeMem[DBS-N:]) into the windowed tail the next
// IMDCT overlap-adds, after undoing the post-filter on it.
func (st *celtDecoder) prefilterAndFoldApply(N int) {
	var etmp [celtOverlap]float32
	ov := celtOverlap
	w := window120[:]
	for c := 0; c < st.channels; c++ {
		mem := st.decodeMem[c][:]
		combFilter(etmp[:], 0, mem, decodeBufferSize-N, st.postfilterPeriodOld, st.postfilterPeriod, ov,
			-st.postfilterGainOld, -st.postfilterGain, st.postfilterTapsetOld, st.postfilterTapset, nil, 0)
		base := decodeBufferSize - N
		for i := 0; i < ov/2; i++ {
			x2 := w[i]*etmp[ov-1-i] + w[ov-1-i]*etmp[i]
			mem[base+i] = x2 * w[ov-1-i]
			mem[base+ov-1-i] = x2 * w[i]
		}
	}
}

func (st *celtDecoder) decodeLost(pcm []float32, N, LM int, accum bool) {
	C := st.channels
	M := 1 << uint(LM)
	start := st.start
	lossDuration := st.lossDuration
	frameType := framePLCPeriodic
	if st.plcDuration >= 40 || start != 0 || st.skipPLC {
		frameType = framePLCNoise
	}
	if frameType == framePLCNoise {
		end := st.end
		effEnd := imax(start, imin(end, nbEBands))
		X := st.scratchX[:C*N]
		clear(X)
		for c := 0; c < C; c++ {
			copy(st.decodeMem[c][:decodeBufferSize-N+celtOverlap], st.decodeMem[c][N:decodeBufferSize+celtOverlap])
		}
		if st.prefilterAndFold {
			st.prefilterAndFoldApply(N)
		}
		decay := float32(0.5)
		if lossDuration == 0 {
			decay = 1.5
		}
		for c := 0; c < C; c++ {
			for i := start; i < end; i++ {
				k := c*nbEBands + i
				st.oldBandE[k] = max(st.backgroundLogE[k], st.oldBandE[k]-decay)
			}
		}
		seed := st.rng
		for c := 0; c < C; c++ {
			for i := start; i < effEnd; i++ {
				boffs := N*c + (int(eband5ms[i]) << uint(LM))
				blen := int(eband5ms[i+1]-eband5ms[i]) << uint(LM)
				for j := 0; j < blen; j++ {
					seed = celtLCGRand(seed)
					X[boffs+j] = float32(int32(seed) >> 20)
				}
				renormaliseVector(X[boffs:], blen, 1)
			}
		}
		st.rng = seed
		freq := st.scratchF[:C*N]
		saveStart := st.start
		st.denormaliseBands(X, freq, effEnd, C, M)
		st.start = saveStart
		for c := 0; c < C; c++ {
			bound := M * int(eband5ms[effEnd])
			if st.downsample != 1 {
				bound = imin(bound, N/st.downsample)
			}
			for i := bound; i < N; i++ {
				freq[c*N+i] = 0
			}
		}
		st.computeInvMDCTs(freq, 0, C, LM)
		for c := 0; c < C; c++ {
			st.postfilterPeriod = imax(st.postfilterPeriod, combMinPeriod)
			st.postfilterPeriodOld = imax(st.postfilterPeriodOld, combMinPeriod)
			mem := st.decodeMem[c][:]
			off := decodeBufferSize - N
			combFilter(mem, off, mem, off, st.postfilterPeriodOld, st.postfilterPeriod, shortMdctSize,
				st.postfilterGainOld, st.postfilterGain, st.postfilterTapsetOld, st.postfilterTapset, window120[:], celtOverlap)
			if LM != 0 {
				combFilter(mem, off+shortMdctSize, mem, off+shortMdctSize, st.postfilterPeriod, st.postfilterPeriod, N-shortMdctSize,
					st.postfilterGain, st.postfilterGain, st.postfilterTapset, st.postfilterTapset, window120[:], celtOverlap)
			}
		}
		st.postfilterPeriodOld = st.postfilterPeriod
		st.postfilterGainOld = st.postfilterGain
		st.postfilterTapsetOld = st.postfilterTapset
		st.prefilterAndFold = false
		st.skipPLC = true
	} else {
		fade := float32(1)
		var pitchIndex int
		if st.lastFrameType != framePLCPeriodic {
			pitchIndex = st.plcPitchSearch(C)
			st.lastPitchIndex = pitchIndex
		} else {
			pitchIndex = st.lastPitchIndex
			fade = 0.8
		}
		pitchIndex = imax(pitchIndex, 1)
		excLength := imin(2*pitchIndex, maxPeriod)
		var excBuf [maxPeriod + lpcOrder]float32
		var firTmp [maxPeriod]float32
		exc := excBuf[lpcOrder:]
		ov := celtOverlap
		w := window120[:]
		for c := 0; c < C; c++ {
			buf := st.decodeMem[c][:]
			for i := 0; i < maxPeriod+lpcOrder; i++ {
				excBuf[i] = buf[decodeBufferSize-maxPeriod-lpcOrder+i]
			}
			lpc := st.lpc[c][:]
			if st.lastFrameType != framePLCPeriodic {
				var ac [lpcOrder + 1]float32
				celtAutocorr(exc[:maxPeriod], ac[:], w, ov, lpcOrder)
				ac[0] *= 1.0001
				for i := 1; i <= lpcOrder; i++ {
					ac[i] -= ac[i] * (0.008 * 0.008) * float32(i*i)
				}
				celtLPC(lpc, ac[:], lpcOrder)
			}
			// Excitation for excLength samples before the loss.
			xo := lpcOrder + maxPeriod - excLength
			for i := 0; i < excLength; i++ {
				sum := excBuf[xo+i]
				for k := 0; k < lpcOrder; k++ {
					sum += lpc[k] * excBuf[xo+i-k-1]
				}
				firTmp[i] = sum
			}
			copy(exc[maxPeriod-excLength:maxPeriod], firTmp[:excLength])
			// Decay estimate over the last two pitch periods.
			E1, E2 := float32(1), float32(1)
			decayLength := excLength >> 1
			for i := 0; i < decayLength; i++ {
				e := exc[maxPeriod-decayLength+i]
				E1 += e * e
				e = exc[maxPeriod-2*decayLength+i]
				E2 += e * e
			}
			E1 = min(E1, E2)
			decay := float32(math.Sqrt(float64(E1 / E2)))

			copy(buf[:decodeBufferSize-N], buf[N:decodeBufferSize])
			extrapOffset := maxPeriod - pitchIndex
			extrapLen := N + ov
			attenuation := fade * decay
			var S1 float32
			for i, j := 0, 0; i < extrapLen; i, j = i+1, j+1 {
				if j >= pitchIndex {
					j -= pitchIndex
					attenuation *= decay
				}
				buf[decodeBufferSize-N+i] = attenuation * exc[extrapOffset+j]
				tmp := buf[decodeBufferSize-maxPeriod-N+extrapOffset+j]
				S1 += tmp * tmp
			}
			var lpcMem [lpcOrder]float32
			for i := 0; i < lpcOrder; i++ {
				lpcMem[i] = buf[decodeBufferSize-N-1-i]
			}
			celtIIR(buf[decodeBufferSize-N:decodeBufferSize-N+extrapLen], lpc, lpcMem[:])
			var S2 float32
			for i := 0; i < extrapLen; i++ {
				tmp := buf[decodeBufferSize-N+i]
				S2 += tmp * tmp
			}
			if !(S1 > 0.2*S2) {
				clear(buf[decodeBufferSize-N : decodeBufferSize-N+extrapLen])
			} else if S1 < S2 {
				ratio := float32(math.Sqrt(float64((S1 + 1) / (S2 + 1))))
				for i := 0; i < ov; i++ {
					g := 1 - w[i]*(1-ratio)
					buf[decodeBufferSize-N+i] *= g
				}
				for i := ov; i < extrapLen; i++ {
					buf[decodeBufferSize-N+i] *= ratio
				}
			}
		}
		st.prefilterAndFold = true
	}
	st.lossDuration = imin(10000, lossDuration+M)
	st.plcDuration = imin(10000, st.plcDuration+M)
	st.lastFrameType = frameType
	st.deemphasis(pcm, N, accum)
}

func (st *celtDecoder) plcPitchSearch(C int) int {
	var lp [decodeBufferSize >> 1]float32
	var x [2][]float32
	for c := 0; c < C; c++ {
		x[c] = st.decodeMem[c][:decodeBufferSize]
	}
	pitchDownsample(x[:C], lp[:], decodeBufferSize>>1, 2)
	idx := pitchSearch(lp[plcPitchLagMax>>1:], lp[:], decodeBufferSize-plcPitchLagMax, plcPitchLagMax-plcPitchLagMin)
	return plcPitchLagMax - idx
}

func celtAutocorr(x, ac, window []float32, overlap, lag int) {
	n := len(x)
	xx := make([]float32, n)
	copy(xx, x)
	for i := 0; i < overlap; i++ {
		xx[i] = x[i] * window[i]
		xx[n-i-1] = x[n-i-1] * window[i]
	}
	for k := 0; k <= lag; k++ {
		var d float32
		for i := k; i < n; i++ {
			d += xx[i] * xx[i-k]
		}
		ac[k] = d
	}
}

func celtLPC(lpc, ac []float32, p int) {
	for i := 0; i < p; i++ {
		lpc[i] = 0
	}
	errv := ac[0]
	if !(ac[0] > 1e-10) {
		return
	}
	for i := 0; i < p; i++ {
		var rr float32
		for j := 0; j < i; j++ {
			rr += lpc[j] * ac[i-j]
		}
		rr += ac[i+1]
		r := -rr / errv
		lpc[i] = r
		for j := 0; j < (i+1)>>1; j++ {
			t1 := lpc[j]
			t2 := lpc[i-1-j]
			lpc[j] = t1 + r*t2
			lpc[i-1-j] = t2 + r*t1
		}
		errv = errv - r*r*errv
		if errv <= 0.001*ac[0] {
			break
		}
	}
}

// celtIIR filters y in place with the all-pole filter 1/(1+sum den[k] z^-k-1),
// mem holding the previous outputs (most recent first).
func celtIIR(y, den, mem []float32) {
	ord := len(mem)
	for i := range y {
		sum := y[i]
		for j := 0; j < ord; j++ {
			sum -= den[j] * mem[j]
		}
		copy(mem[1:], mem[:ord-1])
		mem[0] = sum
		y[i] = sum
	}
}

func pitchDownsample(x [][]float32, xlp []float32, length, factor int) {
	offset := factor / 2
	for i := 1; i < length; i++ {
		xlp[i] = 0.25*x[0][factor*i-offset] + 0.25*x[0][factor*i+offset] + 0.5*x[0][factor*i]
	}
	xlp[0] = 0.25*x[0][offset] + 0.5*x[0][0]
	if len(x) == 2 {
		for i := 1; i < length; i++ {
			xlp[i] += 0.25*x[1][factor*i-offset] + 0.25*x[1][factor*i+offset] + 0.5*x[1][factor*i]
		}
		xlp[0] += 0.25*x[1][offset] + 0.5*x[1][0]
	}
	var ac [5]float32
	celtAutocorr(xlp[:length], ac[:], nil, 0, 4)
	ac[0] *= 1.0001
	for i := 1; i <= 4; i++ {
		ac[i] -= ac[i] * (0.008 * float32(i)) * (0.008 * float32(i))
	}
	var lpc [4]float32
	celtLPC(lpc[:], ac[:], 4)
	tmp := float32(1)
	for i := 0; i < 4; i++ {
		tmp *= 0.9
		lpc[i] *= tmp
	}
	const c1 = float32(0.8)
	lpc2 := [5]float32{lpc[0] + 0.8, lpc[1] + c1*lpc[0], lpc[2] + c1*lpc[1], lpc[3] + c1*lpc[2], c1 * lpc[3]}
	var mem [5]float32
	for i := 0; i < length; i++ {
		sum := xlp[i] + lpc2[0]*mem[0] + lpc2[1]*mem[1] + lpc2[2]*mem[2] + lpc2[3]*mem[3] + lpc2[4]*mem[4]
		mem[4], mem[3], mem[2], mem[1] = mem[3], mem[2], mem[1], mem[0]
		mem[0] = xlp[i]
		xlp[i] = sum
	}
}

func innerProd(x, y []float32, n int) float32 {
	var s float32
	for i := 0; i < n; i++ {
		s += x[i] * y[i]
	}
	return s
}

func findBestPitch(xcorr, y []float32, length, maxPitch int, best *[2]int) {
	Syy := float32(1)
	bestNum := [2]float32{-1, -1}
	bestDen := [2]float32{0, 0}
	best[0], best[1] = 0, 1
	for j := 0; j < length; j++ {
		Syy += y[j] * y[j]
	}
	for i := 0; i < maxPitch; i++ {
		if xcorr[i] > 0 {
			x16 := xcorr[i] * 1e-12
			num := x16 * x16
			if num*bestDen[1] > bestNum[1]*Syy {
				if num*bestDen[0] > bestNum[0]*Syy {
					bestNum[1], bestDen[1], best[1] = bestNum[0], bestDen[0], best[0]
					bestNum[0], bestDen[0], best[0] = num, Syy, i
				} else {
					bestNum[1], bestDen[1], best[1] = num, Syy, i
				}
			}
		}
		Syy += y[i+length]*y[i+length] - y[i]*y[i]
		Syy = max(1, Syy)
	}
}

func pitchSearch(xlp, y []float32, length, maxPitch int) int {
	lag := length + maxPitch
	xlp4 := make([]float32, length>>2)
	ylp4 := make([]float32, lag>>2)
	xcorr := make([]float32, maxPitch>>1)
	for j := range xlp4 {
		xlp4[j] = xlp[2*j]
	}
	for j := range ylp4 {
		ylp4[j] = y[2*j]
	}
	for i := 0; i < maxPitch>>2; i++ {
		xcorr[i] = innerProd(xlp4, ylp4[i:], length>>2)
	}
	var best [2]int
	findBestPitch(xcorr, ylp4, length>>2, maxPitch>>2, &best)
	for i := 0; i < maxPitch>>1; i++ {
		xcorr[i] = 0
		if iabs(i-2*best[0]) > 2 && iabs(i-2*best[1]) > 2 {
			continue
		}
		xcorr[i] = max(-1, innerProd(xlp, y[i:], length>>1))
	}
	findBestPitch(xcorr, y, length>>1, maxPitch>>1, &best)
	offset := 0
	if best[0] > 0 && best[0] < (maxPitch>>1)-1 {
		a, b, c := xcorr[best[0]-1], xcorr[best[0]], xcorr[best[0]+1]
		if c-a > 0.7*(b-a) {
			offset = 1
		} else if a-c > 0.7*(b-c) {
			offset = -1
		}
	}
	return 2*best[0] - offset
}

func iabs(a int) int {
	if a < 0 {
		return -a
	}
	return a
}
