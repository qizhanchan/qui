package opus

import "math"

// CELT band decoding: PVQ shape decoding with band splitting, stereo
// (mid/side, intensity), time-frequency changes, spreading, folding and
// anti-collapse. A float port of celt/bands.c and celt/vq.c (decoder side),
// including the RFC 8251 hybrid folding fix.

const (
	nbEBands      = 21
	shortMdctSize = 120
	celtOverlap   = 120
	maxLM         = 3
	nbAllocVecs   = 11

	spreadNone       = 0
	spreadLight      = 1
	spreadNormal     = 2
	spreadAggressive = 3

	qthetaOffset         = 4
	qthetaOffsetTwoPhase = 16
	maxFineBits          = 8
	fineOffset           = 21
	celtEpsilon          = 1e-15
	celtPI               = float32(3.141592653)
)

var eMeans = [25]float32{
	6.437500, 6.250000, 5.750000, 5.312500, 5.062500,
	4.812500, 4.500000, 4.375000, 4.875000, 4.687500,
	4.562500, 4.437500, 4.875000, 4.625000, 4.312500,
	4.500000, 4.375000, 4.625000, 4.750000, 4.437500,
	3.750000, 3.750000, 3.750000, 3.750000, 3.750000,
}

func celtLCGRand(seed uint32) uint32 { return 1664525*seed + 1013904223 }

func fracMul16(a, b int) int {
	return (16384 + int(int32(int16(a))*int32(int16(b)))) >> 15
}

func bitexactCos(x int) int {
	tmp := (4096 + x*x) >> 13
	x2 := tmp
	x2 = (32767 - x2) + fracMul16(x2, -7651+fracMul16(x2, 8277+fracMul16(-626, x2)))
	return 1 + x2
}

func bitexactLog2tan(isin, icos int) int {
	lc := ilog(uint32(icos))
	ls := ilog(uint32(isin))
	icos <<= uint(15 - lc)
	isin <<= uint(15 - ls)
	return (ls-lc)*(1<<11) + fracMul16(isin, fracMul16(isin, -2597)+7932) - fracMul16(icos, fracMul16(icos, -2597)+7932)
}

func isqrt32(v uint32) uint32 {
	// Bit-by-bit integer square root (celt/mathops.c).
	var g uint32
	bshift := (ilog(v) - 1) >> 1
	b := uint32(1) << uint(bshift)
	for bshift >= 0 {
		t := (g<<1 + b) << uint(bshift)
		if t <= v {
			g += b
			v -= t
		}
		b >>= 1
		bshift--
	}
	return g
}

func cacheBitsFor(LM, band int) []uint8 {
	idx := cacheIndex50[(LM+1)*nbEBands+band]
	if idx < 0 {
		return cacheBits50[:1:1] // never split; only reached on N==1 bands
	}
	return cacheBits50[idx:]
}

func getPulses(i int) int {
	if i < 8 {
		return i
	}
	return (8 + (i & 7)) << uint((i>>3)-1)
}

func bits2pulses(band, LM, b int) int {
	cache := cacheBitsFor(LM, band)
	if cacheIndex50[(LM+1)*nbEBands+band] < 0 {
		return 0
	}
	lo := 0
	hi := int(cache[0])
	b--
	for i := 0; i < 6; i++ {
		mid := (lo + hi + 1) >> 1
		if int(cache[mid]) >= b {
			hi = mid
		} else {
			lo = mid
		}
	}
	l := -1
	if lo != 0 {
		l = int(cache[lo])
	}
	if b-l <= int(cache[hi])-b {
		return lo
	}
	return hi
}

func pulses2bits(band, LM, pulses int) int {
	if pulses == 0 {
		return 0
	}
	cache := cacheBitsFor(LM, band)
	if pulses >= len(cache) {
		return 0
	}
	return int(cache[pulses]) + 1
}

// --- vq.c ---

func expRotation1(X []float32, length, stride int, c, s float32) {
	for i := 0; i < length-stride; i++ {
		x1 := X[i]
		x2 := X[i+stride]
		X[i+stride] = float32(c*x2) + float32(s*x1)
		X[i] = float32(c*x1) - float32(s*x2)
	}
	for i := length - 2*stride - 1; i >= 0; i-- {
		x1 := X[i]
		x2 := X[i+stride]
		X[i+stride] = float32(c*x2) + float32(s*x1)
		X[i] = float32(c*x1) - float32(s*x2)
	}
}

var spreadFactor = [3]int{15, 10, 5}

func expRotation(X []float32, length, dir, stride, K, spread int) {
	if 2*K >= length || spread == spreadNone {
		return
	}
	factor := spreadFactor[spread-1]
	gain := float32(length) / float32(length+factor*K)
	theta := 0.5 * gain * gain
	c := float32(math.Cos(float64(0.5 * celtPI * theta)))
	s := float32(math.Cos(float64(0.5 * celtPI * (1 - theta))))
	stride2 := 0
	if length >= 8*stride {
		stride2 = 1
		for (stride2*stride2+stride2)*stride+(stride>>2) < length {
			stride2++
		}
	}
	length /= stride
	for i := 0; i < stride; i++ {
		x := X[i*length:]
		if dir < 0 {
			if stride2 != 0 {
				expRotation1(x, length, stride2, s, c)
			}
			expRotation1(x, length, 1, c, s)
		} else {
			expRotation1(x, length, 1, c, -s)
			if stride2 != 0 {
				expRotation1(x, length, stride2, s, -c)
			}
		}
	}
}

func extractCollapseMask(iy []int, N, B int) uint {
	if B <= 1 {
		return 1
	}
	N0 := N / B
	var mask uint
	for i := 0; i < B; i++ {
		for j := 0; j < N0; j++ {
			if iy[i*N0+j] != 0 {
				mask |= 1 << uint(i)
			}
		}
	}
	return mask
}

func renormaliseVector(X []float32, N int, gain float32) {
	E := float32(celtEpsilon)
	for i := 0; i < N; i++ {
		E += X[i] * X[i]
	}
	g := float32(1/math.Sqrt(float64(E))) * gain
	for i := 0; i < N; i++ {
		X[i] *= g
	}
}

// --- bands.c ---

func haar1(X []float32, N0, stride int) {
	N0 >>= 1
	const s = float32(0.70710678)
	for i := 0; i < stride; i++ {
		for j := 0; j < N0; j++ {
			t1 := s * X[stride*2*j+i]
			t2 := s * X[stride*(2*j+1)+i]
			X[stride*2*j+i] = t1 + t2
			X[stride*(2*j+1)+i] = t1 - t2
		}
	}
}

func deinterleaveHadamard(tmp, X []float32, N0, stride int, hadamard bool) {
	N := N0 * stride
	if hadamard {
		ordery := orderyTable[stride-2:]
		for i := 0; i < stride; i++ {
			for j := 0; j < N0; j++ {
				tmp[ordery[i]*N0+j] = X[j*stride+i]
			}
		}
	} else {
		for i := 0; i < stride; i++ {
			for j := 0; j < N0; j++ {
				tmp[i*N0+j] = X[j*stride+i]
			}
		}
	}
	copy(X[:N], tmp[:N])
}

func interleaveHadamard(tmp, X []float32, N0, stride int, hadamard bool) {
	N := N0 * stride
	if hadamard {
		ordery := orderyTable[stride-2:]
		for i := 0; i < stride; i++ {
			for j := 0; j < N0; j++ {
				tmp[j*stride+i] = X[ordery[i]*N0+j]
			}
		}
	} else {
		for i := 0; i < stride; i++ {
			for j := 0; j < N0; j++ {
				tmp[j*stride+i] = X[i*N0+j]
			}
		}
	}
	copy(X[:N], tmp[:N])
}

var exp2Table8 = [8]int{16384, 17866, 19483, 21247, 23170, 25267, 27554, 30048}

func computeQN(N, b, offset, pulseCap int, stereo bool) int {
	N2 := 2*N - 1
	if stereo && N == 2 {
		N2--
	}
	qb := b - pulseCap - (4 << bitRes)
	if v := (b + N2*offset) / N2; v < qb {
		qb = v
	}
	if qb > 8<<bitRes {
		qb = 8 << bitRes
	}
	if qb < (1 << bitRes >> 1) {
		return 1
	}
	qn := exp2Table8[qb&0x7] >> uint(14-(qb>>bitRes))
	return (qn + 1) >> 1 << 1
}

func stereoMerge(X, Y []float32, mid float32, N int) {
	var xp, side float32
	for j := 0; j < N; j++ {
		xp += X[j] * Y[j]
		side += Y[j] * Y[j]
	}
	xp = mid * xp
	mid2 := mid
	El := mid2*mid2 + side - 2*xp
	Er := mid2*mid2 + side + 2*xp
	if Er < 6e-4 || El < 6e-4 {
		copy(Y[:N], X[:N])
		return
	}
	lgain := float32(1 / math.Sqrt(float64(El)))
	rgain := float32(1 / math.Sqrt(float64(Er)))
	for j := 0; j < N; j++ {
		l := mid * X[j]
		r := Y[j]
		X[j] = lgain * (l - r)
		Y[j] = rgain * (l + r)
	}
}

var bitInterleaveTable = [16]uint8{0, 1, 1, 1, 2, 3, 3, 3, 2, 3, 3, 3, 2, 3, 3, 3}
var bitDeinterleaveTable = [16]uint8{
	0x00, 0x03, 0x0C, 0x0F, 0x30, 0x33, 0x3C, 0x3F,
	0xC0, 0xC3, 0xCC, 0xCF, 0xF0, 0xF3, 0xFC, 0xFF,
}

type bandCtx struct {
	d         *rangeDecoder
	remaining int
	seed      uint32
	intensity int
	spread    int
	tfChange  int
	band      int
	iy        [176]int
	u         [260]uint32
	tmp       [176]float32
}

func (ctx *bandCtx) algUnquant(X []float32, N, K, spread, B int, gain float32) uint {
	iy := ctx.iy[:N]
	if K+2 > len(ctx.u) {
		K = len(ctx.u) - 2
	}
	decodePulses(iy, N, K, ctx.d, ctx.u[:])
	var Ryy float32
	for i := 0; i < N; i++ {
		Ryy += float32(iy[i] * iy[i])
	}
	g := float32(1/math.Sqrt(float64(Ryy))) * gain
	if Ryy == 0 { // corrupt codeword
		g = 0
	}
	for i := 0; i < N; i++ {
		X[i] = g * float32(iy[i])
	}
	expRotation(X, N, -1, B, K, spread)
	return extractCollapseMask(iy, N, B)
}

// quantBand decodes one band (or half-band after a split), recursively.
// Y is nil for mono. It returns the collapse mask.
func (ctx *bandCtx) quantBand(X, Y []float32, N, b, B int, lowband []float32, LM int,
	lowbandOut []float32, level int, gain float32, lowbandScratch []float32, fill int) uint {
	d := ctx.d
	i := ctx.band
	N0 := N
	NB := N
	B0 := B
	timeDivide := 0
	recombine := 0
	inv := false
	var mid, side float32
	var cm uint
	longBlocks := B0 == 1
	tfChange := ctx.tfChange

	NB /= B
	stereo := Y != nil
	split := stereo

	if N == 1 {
		x := X
		for c := 0; c < 1+b2i(stereo); c++ {
			sign := 0
			if ctx.remaining >= 1<<bitRes {
				sign = int(d.bits(1))
				ctx.remaining -= 1 << bitRes
				b -= 1 << bitRes
			}
			if sign != 0 {
				x[0] = -1
			} else {
				x[0] = 1
			}
			x = Y
		}
		if lowbandOut != nil {
			lowbandOut[0] = X[0]
		}
		return 1
	}

	if !stereo && level == 0 {
		if tfChange > 0 {
			recombine = tfChange
		}
		if lowband != nil && (recombine != 0 || (NB&1 == 0 && tfChange < 0) || B0 > 1) {
			copy(lowbandScratch[:N], lowband[:N])
			lowband = lowbandScratch
		}
		for k := 0; k < recombine; k++ {
			if lowband != nil {
				haar1(lowband, N>>uint(k), 1<<uint(k))
			}
			fill = int(bitInterleaveTable[fill&0xF]) | int(bitInterleaveTable[fill>>4])<<2
		}
		B >>= uint(recombine)
		NB <<= uint(recombine)
		for NB&1 == 0 && tfChange < 0 {
			if lowband != nil {
				haar1(lowband, NB, B)
			}
			fill |= fill << uint(B)
			B <<= 1
			NB >>= 1
			timeDivide++
			tfChange++
		}
		B0 = B
		if B0 > 1 && lowband != nil {
			deinterleaveHadamard(ctx.tmp[:], lowband, NB>>uint(recombine), B0<<uint(recombine), longBlocks)
		}
	}
	NB0 := NB

	cache := cacheBitsFor(LM, i)
	if !stereo && LM != -1 && b > int(cache[cache[0]])+12 && N > 2 {
		N >>= 1
		Y = X[N:]
		split = true
		LM--
		if B == 1 {
			fill = (fill & 1) | (fill << 1)
		}
		B = (B + 1) >> 1
	}

	if split {
		itheta := 0
		var mbits, sbits, delta int
		pulseCap := int(logN400[i]) + LM*(1<<bitRes)
		off := qthetaOffset
		if stereo && N == 2 {
			off = qthetaOffsetTwoPhase
		}
		offset := (pulseCap >> 1) - off
		qn := computeQN(N, b, offset, pulseCap, stereo)
		if stereo && i >= ctx.intensity {
			qn = 1
		}
		tell := d.tellFrac()
		if qn != 1 {
			if stereo && N > 2 {
				p0 := 3
				x0 := qn / 2
				ft := uint32(p0*(x0+1) + x0)
				fs := int(d.decode(ft))
				var x int
				if fs < (x0+1)*p0 {
					x = fs / p0
				} else {
					x = x0 + 1 + (fs - (x0+1)*p0)
				}
				var fl, fh int
				if x <= x0 {
					fl, fh = p0*x, p0*(x+1)
				} else {
					fl, fh = (x-1-x0)+(x0+1)*p0, (x-x0)+(x0+1)*p0
				}
				d.update(uint32(fl), uint32(fh), ft)
				itheta = x
			} else if B0 > 1 || stereo {
				itheta = int(d.uint(uint32(qn + 1)))
			} else {
				ft := ((qn >> 1) + 1) * ((qn >> 1) + 1)
				fm := int(d.decode(uint32(ft)))
				var fs, fl int
				if fm < ((qn>>1)*((qn>>1)+1))>>1 {
					itheta = (int(isqrt32(8*uint32(fm)+1)) - 1) >> 1
					fs = itheta + 1
					fl = itheta * (itheta + 1) >> 1
				} else {
					itheta = (2*(qn+1) - int(isqrt32(8*uint32(ft-fm-1)+1))) >> 1
					fs = qn + 1 - itheta
					fl = ft - ((qn + 1 - itheta) * (qn + 2 - itheta) >> 1)
				}
				d.update(uint32(fl), uint32(fl+fs), uint32(ft))
			}
			itheta = itheta * 16384 / qn
		} else if stereo {
			if b > 2<<bitRes && ctx.remaining > 2<<bitRes {
				inv = d.bitLogp(2) != 0
			} else {
				inv = false
			}
			itheta = 0
		}
		qalloc := d.tellFrac() - tell
		b -= qalloc

		origFill := fill
		var imid, iside int
		if itheta == 0 {
			imid = 32767
			iside = 0
			fill &= (1 << uint(B)) - 1
			delta = -16384
		} else if itheta == 16384 {
			imid = 0
			iside = 32767
			fill &= ((1 << uint(B)) - 1) << uint(B)
			delta = 16384
		} else {
			imid = bitexactCos(itheta)
			iside = bitexactCos(16384 - itheta)
			delta = fracMul16((N-1)<<7, bitexactLog2tan(iside, imid))
		}
		mid = (1.0 / 32768) * float32(imid)
		side = (1.0 / 32768) * float32(iside)

		if N == 2 && stereo {
			mbits = b
			sbits = 0
			if itheta != 0 && itheta != 16384 {
				sbits = 1 << bitRes
			}
			mbits -= sbits
			c := itheta > 8192
			ctx.remaining -= qalloc + sbits
			x2, y2 := X, Y
			if c {
				x2, y2 = Y, X
			}
			sign := 0
			if sbits != 0 {
				sign = int(d.bits(1))
			}
			sign = 1 - 2*sign
			cm = ctx.quantBand(x2, nil, N, mbits, B, lowband, LM, lowbandOut, level, gain, lowbandScratch, origFill)
			y2[0] = -float32(sign) * x2[1]
			y2[1] = float32(sign) * x2[0]
			X[0] = mid * X[0]
			X[1] = mid * X[1]
			Y[0] = side * Y[0]
			Y[1] = side * Y[1]
			tmp := X[0]
			X[0] = tmp - Y[0]
			Y[0] = tmp + Y[0]
			tmp = X[1]
			X[1] = tmp - Y[1]
			Y[1] = tmp + Y[1]
		} else {
			var nextLowband2, nextLowbandOut1 []float32
			nextLevel := 0
			if B0 > 1 && !stereo && itheta&0x3fff != 0 {
				if itheta > 8192 {
					delta -= delta >> uint(4-LM)
				} else {
					delta = imin(0, delta+(N<<bitRes>>uint(5-LM)))
				}
			}
			mbits = imax(0, imin(b, (b-delta)/2))
			sbits = b - mbits
			ctx.remaining -= qalloc
			if lowband != nil && !stereo {
				nextLowband2 = lowband[N:]
			}
			if stereo {
				nextLowbandOut1 = lowbandOut
			} else {
				nextLevel = level + 1
			}
			rebalance := ctx.remaining
			midGain := gain * mid
			if stereo {
				midGain = 1
			}
			shift := uint((B0 >> 1) & (b2i(stereo) - 1))
			if mbits >= sbits {
				cm = ctx.quantBand(X, nil, N, mbits, B, lowband, LM, nextLowbandOut1, nextLevel, midGain, lowbandScratch, fill)
				rebalance = mbits - (rebalance - ctx.remaining)
				if rebalance > 3<<bitRes && itheta != 0 {
					sbits += rebalance - (3 << bitRes)
				}
				cm |= ctx.quantBand(Y, nil, N, sbits, B, nextLowband2, LM, nil, nextLevel, gain*side, nil, fill>>uint(B)) << shift
			} else {
				cm = ctx.quantBand(Y, nil, N, sbits, B, nextLowband2, LM, nil, nextLevel, gain*side, nil, fill>>uint(B)) << shift
				rebalance = sbits - (rebalance - ctx.remaining)
				if rebalance > 3<<bitRes && itheta != 16384 {
					mbits += rebalance - (3 << bitRes)
				}
				cm |= ctx.quantBand(X, nil, N, mbits, B, lowband, LM, nextLowbandOut1, nextLevel, midGain, lowbandScratch, fill)
			}
		}
	} else {
		q := bits2pulses(i, LM, b)
		currBits := pulses2bits(i, LM, q)
		ctx.remaining -= currBits
		for ctx.remaining < 0 && q > 0 {
			ctx.remaining += currBits
			q--
			currBits = pulses2bits(i, LM, q)
			ctx.remaining -= currBits
		}
		if q != 0 {
			K := getPulses(q)
			cm = ctx.algUnquant(X, N, K, ctx.spread, B, gain)
		} else {
			cmMask := uint(1)<<uint(B) - 1
			fill &= int(cmMask)
			if fill == 0 {
				for j := 0; j < N; j++ {
					X[j] = 0
				}
			} else {
				if lowband == nil {
					for j := 0; j < N; j++ {
						ctx.seed = celtLCGRand(ctx.seed)
						X[j] = float32(int32(ctx.seed) >> 20)
					}
					cm = cmMask
				} else {
					for j := 0; j < N; j++ {
						ctx.seed = celtLCGRand(ctx.seed)
						tmp := float32(1.0 / 256)
						if ctx.seed&0x8000 == 0 {
							tmp = -tmp
						}
						X[j] = lowband[j] + tmp
					}
					cm = uint(fill)
				}
				renormaliseVector(X, N, gain)
			}
		}
	}

	if stereo {
		if N != 2 {
			stereoMerge(X, Y, mid, N)
		}
		if inv {
			for j := 0; j < N; j++ {
				Y[j] = -Y[j]
			}
		}
	} else if level == 0 {
		if B0 > 1 {
			interleaveHadamard(ctx.tmp[:], X, NB>>uint(recombine), B0<<uint(recombine), longBlocks)
		}
		NB = NB0
		B = B0
		for k := 0; k < timeDivide; k++ {
			B >>= 1
			NB <<= 1
			cm |= cm >> uint(B)
			haar1(X, NB, B)
		}
		for k := 0; k < recombine; k++ {
			cm = uint(bitDeinterleaveTable[cm&0xF])
			haar1(X, N0>>uint(k), 1<<uint(k))
		}
		B <<= uint(recombine)
		if lowbandOut != nil {
			n := float32(math.Sqrt(float64(N0)))
			for j := 0; j < N0; j++ {
				lowbandOut[j] = n * X[j]
			}
		}
		cm &= (1 << uint(B)) - 1
	}
	return cm
}

// quantAllBands decodes the normalised shape of all bands.
func (ctx *bandCtx) quantAllBands(start, end int, X, Y []float32, collapseMasks []uint8,
	pulses []int, shortBlocks bool, spread, dualStereo, intensity int, tfRes []int,
	totalBits, balance int, LM, codedBands int, norm, lowbandScratch []float32) {
	d := ctx.d
	M := 1 << uint(LM)
	B := 1
	if shortBlocks {
		B = M
	}
	C := 1
	if Y != nil {
		C = 2
	}
	normOff := M * int(eband5ms[nbEBands])
	norm2 := norm[normOff:]
	ctx.intensity = intensity
	ctx.spread = spread
	lowbandOffset := 0
	updateLowband := true
	for i := start; i < end; i++ {
		ctx.band = i
		x := X[M*int(eband5ms[i]):]
		var y []float32
		if Y != nil {
			y = Y[M*int(eband5ms[i]):]
		}
		N := M*int(eband5ms[i+1]) - M*int(eband5ms[i])
		tell := d.tellFrac()
		if i != start {
			balance -= tell
		}
		ctx.remaining = totalBits - tell - 1
		b := 0
		if i <= codedBands-1 {
			currBalance := balance / imin(3, codedBands-i)
			b = imax(0, imin(16383, imin(ctx.remaining+1, pulses[i]+currBalance)))
		}
		if (M*int(eband5ms[i])-N >= M*int(eband5ms[start]) || i == start+1) && (updateLowband || lowbandOffset == 0) {
			lowbandOffset = i
		}
		if i == start+1 {
			// RFC 8251 section 9: duplicate enough of the first band's folding
			// data to fold the second band.
			n1 := M * int(eband5ms[start+1]-eband5ms[start])
			n2 := M * int(eband5ms[start+2]-eband5ms[start+1])
			off := M * int(eband5ms[start])
			if n2 > n1 {
				copy(norm[off+n1:off+n2], norm[off+2*n1-n2:off+n1])
				if C == 2 {
					copy(norm2[off+n1:off+n2], norm2[off+2*n1-n2:off+n1])
				}
			}
		}
		ctx.tfChange = tfRes[i]
		effectiveLowband := -1
		var xcm, ycm uint
		if lowbandOffset != 0 && (spread != spreadAggressive || B > 1 || ctx.tfChange < 0) {
			effectiveLowband = imax(M*int(eband5ms[start]), M*int(eband5ms[lowbandOffset])-N)
			foldStart := lowbandOffset
			for {
				foldStart--
				if M*int(eband5ms[foldStart]) <= effectiveLowband {
					break
				}
			}
			foldEnd := lowbandOffset - 1
			for {
				foldEnd++
				if !(foldEnd < i && M*int(eband5ms[foldEnd]) < effectiveLowband+N) {
					break
				}
			}
			for fi := foldStart; ; {
				xcm |= uint(collapseMasks[fi*C+0])
				ycm |= uint(collapseMasks[fi*C+C-1])
				fi++
				if fi >= foldEnd {
					break
				}
			}
		} else {
			xcm = uint(1)<<uint(B) - 1
			ycm = xcm
		}
		if dualStereo != 0 && i == intensity {
			dualStereo = 0
			for j := M * int(eband5ms[start]); j < M*int(eband5ms[i]); j++ {
				norm[j] = 0.5 * (norm[j] + norm2[j])
			}
		}
		var lb, lb2 []float32
		if effectiveLowband != -1 {
			lb = norm[effectiveLowband:]
			lb2 = norm2[effectiveLowband:]
		}
		if dualStereo != 0 {
			xcm = ctx.quantBand(x, nil, N, b/2, B, lb, LM, norm[M*int(eband5ms[i]):], 0, 1, lowbandScratch, int(xcm))
			ycm = ctx.quantBand(y, nil, N, b/2, B, lb2, LM, norm2[M*int(eband5ms[i]):], 0, 1, lowbandScratch, int(ycm))
		} else {
			xcm = ctx.quantBand(x, y, N, b, B, lb, LM, norm[M*int(eband5ms[i]):], 0, 1, lowbandScratch, int(xcm|ycm))
			ycm = xcm
		}
		collapseMasks[i*C+0] = uint8(xcm)
		collapseMasks[i*C+C-1] = uint8(ycm)
		balance += pulses[i] + tell
		updateLowband = b > N<<bitRes
	}
}

func antiCollapse(X []float32, collapseMasks []uint8, LM, C, size, start, end int,
	logE, prev1logE, prev2logE []float32, pulses []int, seed uint32) {
	for i := start; i < end; i++ {
		N0 := int(eband5ms[i+1] - eband5ms[i])
		depth := (1 + pulses[i]) / (N0 << uint(LM))
		thresh := 0.5 * float32(math.Exp2(float64(-0.125*float32(depth))))
		sqrt1 := float32(1 / math.Sqrt(float64(N0<<uint(LM))))
		for c := 0; c < C; c++ {
			prev1 := prev1logE[c*nbEBands+i]
			prev2 := prev2logE[c*nbEBands+i]
			if C == 1 {
				prev1 = max(prev1, prev1logE[nbEBands+i])
				prev2 = max(prev2, prev2logE[nbEBands+i])
			}
			Ediff := logE[c*nbEBands+i] - min(prev1, prev2)
			Ediff = max(0, Ediff)
			r := 2 * float32(math.Exp2(float64(-Ediff)))
			if LM == 3 {
				r *= 1.41421356
			}
			r = min(thresh, r)
			r = r * sqrt1
			x := X[c*size+(int(eband5ms[i])<<uint(LM)):]
			renorm := false
			for k := 0; k < 1<<uint(LM); k++ {
				if collapseMasks[i*C+c]&(1<<uint(k)) == 0 {
					for j := 0; j < N0; j++ {
						seed = celtLCGRand(seed)
						if seed&0x8000 != 0 {
							x[(j<<uint(LM))+k] = r
						} else {
							x[(j<<uint(LM))+k] = -r
						}
					}
					renorm = true
				}
			}
			if renorm {
				renormaliseVector(x, N0<<uint(LM), 1)
			}
		}
	}
}

func b2i(b bool) int {
	if b {
		return 1
	}
	return 0
}

func imin(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func imax(a, b int) int {
	if a > b {
		return a
	}
	return b
}
