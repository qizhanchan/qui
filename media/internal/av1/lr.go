package av1

var (
	wienerTapsMin = [3]int{-5, -23, -17}
	wienerTapsMax = [3]int{10, 8, 46}
	wienerTapsK   = [3]int{1, 2, 3}
	wienerTapsMid = [3]int{3, -7, 15}
	sgrprojXqdMin = [2]int{-96, -32}
	sgrprojXqdMax = [2]int{31, 95}
	sgrprojXqdMid = [2]int{-32, 31}
)

// readLR is read_lr (spec 5.11.57): the restoration units whose top-left
// corner lies in this superblock.
func (t *tile) readLR(r, c, bSize int) {
	h := t.h
	if h.allowIntraBC || !h.usesLR {
		return
	}
	w, hh := num4x4W[bSize], num4x4H[bSize]
	for p := 0; p < t.seq.NumPlanes(); p++ {
		if h.lrType[p] == restoreNone {
			continue
		}
		ss := min(p, 1)
		lp := &t.f.lr[p]
		unit := h.lrUnitSize[p]
		rowStart := (r*(4>>ss) + unit - 1) / unit
		rowEnd := min(lp.rows, ((r+hh)*(4>>ss)+unit-1)/unit)
		num, den := 4>>ss, unit
		if h.superresDenom != 8 {
			num, den = (4>>ss)*h.superresDenom, unit*8
		}
		colStart := (c*num + den - 1) / den
		colEnd := min(lp.cols, ((c+w)*num+den-1)/den)
		for ur := rowStart; ur < rowEnd; ur++ {
			for uc := colStart; uc < colEnd; uc++ {
				t.readLRUnit(p, &lp.units[ur*lp.cols+uc])
			}
		}
	}
}

func (t *tile) readLRUnit(p int, u *lrUnit) {
	typ := restoreNone
	switch t.h.lrType[p] {
	case restoreWiener:
		if t.ms.flag(t.cdf.wienerRestore[:]) {
			typ = restoreWiener
		}
	case restoreSgrproj:
		if t.ms.flag(t.cdf.sgrprojRestore[:]) {
			typ = restoreSgrproj
		}
	default:
		typ = [3]int{restoreNone, restoreWiener, restoreSgrproj}[t.ms.sym(t.cdf.switchableRestore[:], 3)]
	}
	u.typ = uint8(typ)
	switch typ {
	case restoreWiener:
		for pass := 0; pass < 2; pass++ {
			first := 0
			if p > 0 {
				first = 1
				u.wiener[pass][0] = 0
			}
			for j := first; j < 3; j++ {
				v := t.subexpBool(wienerTapsMin[j], wienerTapsMax[j]+1, wienerTapsK[j], t.refLrWiener[p][pass][j])
				u.wiener[pass][j] = int8(v)
				t.refLrWiener[p][pass][j] = v
			}
		}
	case restoreSgrproj:
		set := t.ms.lit(4)
		u.sgrSet = uint8(set)
		for i := 0; i < 2; i++ {
			v := 0
			if sgrParams[set][i*2] != 0 {
				v = t.subexpBool(sgrprojXqdMin[i], sgrprojXqdMax[i]+1, 4, t.refSgrXqd[p][i])
			} else if i == 1 {
				v = clip3(sgrprojXqdMin[i], sgrprojXqdMax[i], 128-t.refSgrXqd[p][0])
			}
			u.xqd[i] = int16(v)
			t.refSgrXqd[p][i] = v
		}
	}
}

// subexpBool is decode_signed_subexp_with_ref_bool.
func (t *tile) subexpBool(low, high, k, r int) int {
	mx := high - low
	r -= low
	// decode_subexp_bool
	i, mk, v := 0, 0, 0
	for {
		b2 := k
		if i != 0 {
			b2 = k + i - 1
		}
		a := 1 << b2
		if mx <= mk+3*a {
			v = t.ms.ns(mx-mk) + mk
			break
		}
		if t.ms.bit() == 0 {
			v = t.ms.lit(b2) + mk
			break
		}
		i++
		mk += a
	}
	if r<<1 <= mx {
		v = inverseRecenter(r, v)
	} else {
		v = mx - 1 - inverseRecenter(mx-1-r, v)
	}
	return v + low
}

// sgrParams is Sgr_Params: radius and scale of the two self-guided
// filters per set.
var sgrParams = [16][4]int{
	{2, 140, 1, 3236}, {2, 112, 1, 2158}, {2, 93, 1, 1618}, {2, 80, 1, 1438},
	{2, 70, 1, 1295}, {2, 58, 1, 1177}, {2, 47, 1, 1079}, {2, 37, 1, 996},
	{2, 30, 1, 925}, {2, 25, 1, 863}, {0, 0, 1, 2589}, {0, 0, 1, 1618},
	{0, 0, 1, 1177}, {0, 0, 1, 925}, {2, 56, 0, 0}, {2, 22, 0, 0},
}
