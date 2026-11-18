package opus

import "math/bits"

// Fixed-point helpers mirroring silk/SigProc_FIX.h, silk/macros.h and
// silk/Inlines.h. 32-bit wrap-around follows the reference's two's
// complement behaviour.

func smulwb(a, b int32) int32      { return int32((int64(a) * int64(int16(b))) >> 16) }
func smlawb(a, b, c int32) int32   { return a + smulwb(b, c) }
func smulwt(a, b int32) int32      { return int32((int64(a) * int64(b>>16)) >> 16) }
func smlawt(a, b, c int32) int32   { return a + smulwt(b, c) }
func smulbb(a, b int32) int32      { return int32(int16(a)) * int32(int16(b)) }
func smlabb(a, b, c int32) int32   { return a + smulbb(b, c) }
func smulbt(a, b int32) int32      { return int32(int16(a)) * (b >> 16) }
func smlabt(a, b, c int32) int32   { return a + smulbt(b, c) }
func smultt(a, b int32) int32      { return (a >> 16) * (b >> 16) }
func smulww(a, b int32) int32      { return int32((int64(a) * int64(b)) >> 16) }
func smlaww(a, b, c int32) int32   { return a + smulww(b, c) }
func smmul(a, b int32) int32       { return int32((int64(a) * int64(b)) >> 32) }
func smull(a, b int32) int64       { return int64(a) * int64(b) }
func silkRand(seed int32) int32    { return int32(uint32(907633515) + uint32(seed)*196314165) }
func mlaOvflw(a, b, c int32) int32 { return int32(uint32(a) + uint32(b)*uint32(c)) }

func rshiftRound(a int32, s uint) int32 {
	if s == 1 {
		return (a >> 1) + (a & 1)
	}
	return ((a >> (s - 1)) + 1) >> 1
}

func rshiftRound64(a int64, s uint) int64 {
	if s == 1 {
		return (a >> 1) + (a & 1)
	}
	return ((a >> (s - 1)) + 1) >> 1
}

func sat16(a int32) int32 {
	if a > 32767 {
		return 32767
	}
	if a < -32768 {
		return -32768
	}
	return a
}

func addSat16(a, b int32) int32 { return sat16(a + b) }

func addSat32(a, b int32) int32 {
	s := int64(a) + int64(b)
	if s > 0x7FFFFFFF {
		return 0x7FFFFFFF
	}
	if s < -0x80000000 {
		return -0x80000000
	}
	return int32(s)
}

func subSat32(a, b int32) int32 {
	s := int64(a) - int64(b)
	if s > 0x7FFFFFFF {
		return 0x7FFFFFFF
	}
	if s < -0x80000000 {
		return -0x80000000
	}
	return int32(s)
}

func limit32(a, l1, l2 int32) int32 {
	if l1 > l2 {
		if a > l1 {
			return l1
		}
		if a < l2 {
			return l2
		}
		return a
	}
	if a > l2 {
		return l2
	}
	if a < l1 {
		return l1
	}
	return a
}

func limitInt(a, l1, l2 int) int { return int(limit32(int32(a), int32(l1), int32(l2))) }

func lshiftSat32(a int32, s uint) int32 {
	return int32(uint32(limit32(a, int32(-0x80000000)>>s, 0x7FFFFFFF>>s)) << s)
}

func clz32(x int32) int32 { return int32(bits.LeadingZeros32(uint32(x))) }

func abs32(a int32) int32 {
	if a > 0 {
		return a
	}
	return -a
}

func ror32(a int32, rot int) int32 {
	x := uint32(a)
	if rot == 0 {
		return a
	}
	if rot < 0 {
		m := uint(-rot)
		return int32(x<<m | x>>(32-m))
	}
	r := uint(rot)
	return int32(x<<(32-r) | x>>r)
}

func clzFrac(in int32) (lz, fracQ7 int32) {
	lz = clz32(in)
	fracQ7 = ror32(in, int(24-lz)) & 0x7f
	return
}

func sqrtApprox(x int32) int32 {
	if x <= 0 {
		return 0
	}
	lz, fracQ7 := clzFrac(x)
	var y int32
	if lz&1 != 0 {
		y = 32768
	} else {
		y = 46214
	}
	y >>= uint(lz >> 1)
	return smlawb(y, y, smulbb(213, fracQ7))
}

func div32VarQ(a32, b32 int32, qres int) int32 {
	aHead := clz32(abs32(a32)) - 1
	aNrm := a32 << uint(aHead)
	bHead := clz32(abs32(b32)) - 1
	bNrm := b32 << uint(bHead)
	bInv := (0x7FFFFFFF >> 2) / (bNrm >> 16)
	result := smulwb(aNrm, bInv)
	aNrm = int32(uint32(aNrm) - uint32(smmul(bNrm, result))<<3)
	result = smlawb(result, aNrm, bInv)
	lshift := 29 + int(aHead) - int(bHead) - qres
	if lshift < 0 {
		return lshiftSat32(result, uint(-lshift))
	}
	if lshift < 32 {
		return result >> uint(lshift)
	}
	return 0
}

func inverse32VarQ(b32 int32, qres int) int32 {
	bHead := clz32(abs32(b32)) - 1
	bNrm := b32 << uint(bHead)
	bInv := (0x7FFFFFFF >> 2) / (bNrm >> 16)
	result := bInv << 16
	errQ32 := ((1 << 29) - smulwb(bNrm, bInv)) << 3
	result = smlaww(result, errQ32, bInv)
	lshift := 61 - int(bHead) - qres
	if lshift <= 0 {
		return lshiftSat32(result, uint(-lshift))
	}
	if lshift < 32 {
		return result >> uint(lshift)
	}
	return 0
}

func log2lin(inLogQ7 int32) int32 {
	if inLogQ7 < 0 {
		return 0
	} else if inLogQ7 >= 3967 {
		return 0x7FFFFFFF
	}
	out := int32(1) << uint(inLogQ7>>7)
	fracQ7 := inLogQ7 & 0x7F
	if inLogQ7 < 2048 {
		out = out + ((out * smlawb(fracQ7, smulbb(fracQ7, 128-fracQ7), -174)) >> 7)
	} else {
		out = out + (out>>7)*smlawb(fracQ7, smulbb(fracQ7, 128-fracQ7), -174)
	}
	return out
}

func lin2log(inLin int32) int32 {
	lz, fracQ7 := clzFrac(inLin)
	return ((31 - lz) << 7) + smlawb(fracQ7, fracQ7*(128-fracQ7), 179)
}
