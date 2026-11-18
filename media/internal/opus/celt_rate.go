package opus

// CELT bit allocation (RFC 6716 section 4.3.3), a port of the decoder side
// of celt/rate.c.

const allocSteps = 6

type allocScratch struct {
	bits1, bits2, thresh, trimOffset [nbEBands]int
}

func interpBits2Pulses(start, end, skipStart int, bits1, bits2, thresh, cap []int, total int, balanceOut *int,
	skipRsv int, intensity *int, intensityRsv int, dualStereo *int, dualStereoRsv int, bits, ebits, finePriority []int,
	C, LM int, d *rangeDecoder) int {
	allocFloor := C << bitRes
	stereo := 0
	if C > 1 {
		stereo = 1
	}
	logM := LM << bitRes
	lo := 0
	hi := 1 << allocSteps
	for i := 0; i < allocSteps; i++ {
		mid := (lo + hi) >> 1
		psum := 0
		done := false
		for j := end - 1; j >= start; j-- {
			tmp := bits1[j] + (mid * bits2[j] >> allocSteps)
			if tmp >= thresh[j] || done {
				done = true
				psum += imin(tmp, cap[j])
			} else if tmp >= allocFloor {
				psum += allocFloor
			}
		}
		if psum > total {
			hi = mid
		} else {
			lo = mid
		}
	}
	psum := 0
	done := false
	for j := end - 1; j >= start; j-- {
		tmp := bits1[j] + (lo * bits2[j] >> allocSteps)
		if tmp < thresh[j] && !done {
			if tmp >= allocFloor {
				tmp = allocFloor
			} else {
				tmp = 0
			}
		} else {
			done = true
		}
		tmp = imin(tmp, cap[j])
		bits[j] = tmp
		psum += tmp
	}

	codedBands := end
	for ; ; codedBands-- {
		j := codedBands - 1
		if j <= skipStart {
			total += skipRsv
			break
		}
		left := total - psum
		percoeff := left / int(eband5ms[codedBands]-eband5ms[start])
		left -= int(eband5ms[codedBands]-eband5ms[start]) * percoeff
		rem := imax(left-int(eband5ms[j]-eband5ms[start]), 0)
		bandWidth := int(eband5ms[codedBands] - eband5ms[j])
		bandBits := bits[j] + percoeff*bandWidth + rem
		if bandBits >= imax(thresh[j], allocFloor+(1<<bitRes)) {
			if d.bitLogp(1) != 0 {
				break
			}
			psum += 1 << bitRes
			bandBits -= 1 << bitRes
		}
		psum -= bits[j] + intensityRsv
		if intensityRsv > 0 {
			intensityRsv = int(log2FracTable[j-start])
		}
		psum += intensityRsv
		if bandBits >= allocFloor {
			psum += allocFloor
			bits[j] = allocFloor
		} else {
			bits[j] = 0
		}
	}

	if intensityRsv > 0 {
		*intensity = start + int(d.uint(uint32(codedBands+1-start)))
	} else {
		*intensity = 0
	}
	if *intensity <= start {
		total += dualStereoRsv
		dualStereoRsv = 0
	}
	if dualStereoRsv > 0 {
		*dualStereo = d.bitLogp(1)
	} else {
		*dualStereo = 0
	}

	left := total - psum
	percoeff := left / int(eband5ms[codedBands]-eband5ms[start])
	left -= int(eband5ms[codedBands]-eband5ms[start]) * percoeff
	for j := start; j < codedBands; j++ {
		bits[j] += percoeff * int(eband5ms[j+1]-eband5ms[j])
	}
	for j := start; j < codedBands; j++ {
		tmp := imin(left, int(eband5ms[j+1]-eband5ms[j]))
		bits[j] += tmp
		left -= tmp
	}

	balance := 0
	j := start
	for ; j < codedBands; j++ {
		N0 := int(eband5ms[j+1] - eband5ms[j])
		N := N0 << uint(LM)
		bits[j] += balance
		var excess int
		if N > 1 {
			excess = imax(bits[j]-cap[j], 0)
			bits[j] -= excess
			den := C * N
			if C == 2 && N > 2 && *dualStereo == 0 && j < *intensity {
				den++
			}
			NClogN := den * (int(logN400[j]) + logM)
			offset := (NClogN >> 1) - den*fineOffset
			if N == 2 {
				offset += den << bitRes >> 2
			}
			if bits[j]+offset < den*2<<bitRes {
				offset += NClogN >> 2
			} else if bits[j]+offset < den*3<<bitRes {
				offset += NClogN >> 3
			}
			ebits[j] = imax(0, (bits[j]+offset+(den<<(bitRes-1)))/(den<<bitRes))
			if C*ebits[j] > (bits[j] >> bitRes) {
				ebits[j] = bits[j] >> uint(stereo) >> bitRes
			}
			ebits[j] = imin(ebits[j], maxFineBits)
			finePriority[j] = b2i(ebits[j]*(den<<bitRes) >= bits[j]+offset)
			bits[j] -= C * ebits[j] << bitRes
		} else {
			excess = imax(0, bits[j]-(C<<bitRes))
			bits[j] -= excess
			ebits[j] = 0
			finePriority[j] = 1
		}
		if excess > 0 {
			extraFine := imin(excess>>uint(stereo+bitRes), maxFineBits-ebits[j])
			ebits[j] += extraFine
			extraBits := extraFine * C << bitRes
			finePriority[j] = b2i(extraBits >= excess-balance)
			excess -= extraBits
		}
		balance = excess
	}
	*balanceOut = balance
	for ; j < end; j++ {
		ebits[j] = bits[j] >> uint(stereo) >> bitRes
		bits[j] = 0
		finePriority[j] = b2i(ebits[j] < 1)
	}
	return codedBands
}

func computeAllocation(start, end int, offsets, cap []int, allocTrim int, intensity, dualStereo *int,
	total int, balance *int, pulses, ebits, finePriority []int, C, LM int, d *rangeDecoder, s *allocScratch) int {
	total = imax(total, 0)
	length := nbEBands
	skipStart := start
	skipRsv := 0
	if total >= 1<<bitRes {
		skipRsv = 1 << bitRes
	}
	total -= skipRsv
	intensityRsv, dualStereoRsv := 0, 0
	if C == 2 {
		intensityRsv = int(log2FracTable[end-start])
		if intensityRsv > total {
			intensityRsv = 0
		} else {
			total -= intensityRsv
			if total >= 1<<bitRes {
				dualStereoRsv = 1 << bitRes
			}
			total -= dualStereoRsv
		}
	}
	bits1, bits2, thresh, trimOffset := s.bits1[:], s.bits2[:], s.thresh[:], s.trimOffset[:]
	for j := start; j < end; j++ {
		w := int(eband5ms[j+1] - eband5ms[j])
		thresh[j] = imax(C<<bitRes, (3*w<<uint(LM)<<bitRes)>>4)
		trimOffset[j] = C * w * (allocTrim - 5 - LM) * (end - j - 1) * (1 << uint(LM+bitRes)) >> 6
		if w<<uint(LM) == 1 {
			trimOffset[j] -= C << bitRes
		}
	}
	lo := 1
	hi := nbAllocVecs - 1
	for lo <= hi {
		done := false
		psum := 0
		mid := (lo + hi) >> 1
		for j := end - 1; j >= start; j-- {
			N := int(eband5ms[j+1] - eband5ms[j])
			bitsj := C * N * int(bandAllocation[mid*length+j]) << uint(LM) >> 2
			if bitsj > 0 {
				bitsj = imax(0, bitsj+trimOffset[j])
			}
			bitsj += offsets[j]
			if bitsj >= thresh[j] || done {
				done = true
				psum += imin(bitsj, cap[j])
			} else if bitsj >= C<<bitRes {
				psum += C << bitRes
			}
		}
		if psum > total {
			hi = mid - 1
		} else {
			lo = mid + 1
		}
	}
	hi = lo
	lo--
	for j := start; j < end; j++ {
		N := int(eband5ms[j+1] - eband5ms[j])
		bits1j := C * N * int(bandAllocation[lo*length+j]) << uint(LM) >> 2
		var bits2j int
		if hi >= nbAllocVecs {
			bits2j = cap[j]
		} else {
			bits2j = C * N * int(bandAllocation[hi*length+j]) << uint(LM) >> 2
		}
		if bits1j > 0 {
			bits1j = imax(0, bits1j+trimOffset[j])
		}
		if bits2j > 0 {
			bits2j = imax(0, bits2j+trimOffset[j])
		}
		if lo > 0 {
			bits1j += offsets[j]
		}
		bits2j += offsets[j]
		if offsets[j] > 0 {
			skipStart = j
		}
		bits2j = imax(0, bits2j-bits1j)
		bits1[j] = bits1j
		bits2[j] = bits2j
	}
	return interpBits2Pulses(start, end, skipStart, bits1, bits2, thresh, cap, total, balance, skipRsv,
		intensity, intensityRsv, dualStereo, dualStereoRsv, pulses, ebits, finePriority, C, LM, d)
}
