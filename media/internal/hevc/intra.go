package hevc

// Intra sample prediction (spec 8.4.4.2).

var intraPredAngle = [35]int{
	0, 0, 32, 26, 21, 17, 13, 9, 5, 2, 0, -2, -5, -9, -13, -17, -21, -26,
	-32, -26, -21, -17, -13, -9, -5, -2, 0, 2, 5, 9, 13, 17, 21, 26, 32,
}

var invAngle = [35]int{
	11: -4096, 12: -1638, 13: -910, 14: -630, 15: -482, 16: -390, 17: -315,
	18: -256, 19: -315, 20: -390, 21: -482, 22: -630, 23: -910, 24: -1638, 25: -4096,
}

// intraRefs holds the 4n+1 reference samples of an n x n block:
// left[i] = p[-1][i] for i in [-1, 2n) stored at left[i+1], and
// top[i] = p[i][-1] for i in [-1, 2n) stored at top[i+1] (left[0] ==
// top[0] == p[-1][-1]).
type intraRefs struct {
	left, top [65]int
}

// predictIntra writes the n x n prediction of mode into dst (stride ds).
// refs are already substituted; luma selects the luma-only filters.
func predictIntra(dst []byte, ds int, refs *intraRefs, n, mode int, luma, strongSmoothing bool) {
	r := refs
	if luma && mode != 1 && n != 4 {
		minDist := min(iabs(mode-26), iabs(mode-10))
		thres := 0
		switch n {
		case 8:
			thres = 7
		case 16:
			thres = 1
		}
		if minDist > thres {
			var f intraRefs
			p00 := r.left[0]
			bl, tr := r.left[2*n], r.top[2*n]
			if strongSmoothing && n == 32 &&
				iabs(p00+tr-2*r.top[n]) < 8 && iabs(p00+bl-2*r.left[n]) < 8 {
				f.left[0], f.top[0] = p00, p00
				for i := 0; i < 63; i++ {
					f.left[i+1] = ((63-i)*p00 + (i+1)*bl + 32) >> 6
					f.top[i+1] = ((63-i)*p00 + (i+1)*tr + 32) >> 6
				}
				f.left[64], f.top[64] = bl, tr
			} else {
				f.left[0] = (r.left[1] + 2*p00 + r.top[1] + 2) >> 2
				f.top[0] = f.left[0]
				for i := 1; i < 2*n; i++ {
					f.left[i] = (r.left[i+1] + 2*r.left[i] + r.left[i-1] + 2) >> 2
					f.top[i] = (r.top[i+1] + 2*r.top[i] + r.top[i-1] + 2) >> 2
				}
				f.left[2*n], f.top[2*n] = bl, tr
			}
			r = &f
		}
	}
	switch {
	case mode == 0: // planar
		shift := uint(log2(n) + 1)
		tr, bl := r.top[n+1], r.left[n+1]
		for y := 0; y < n; y++ {
			row := dst[y*ds : y*ds+n]
			for x := range row {
				row[x] = byte(((n-1-x)*r.left[y+1] + (x+1)*tr + (n-1-y)*r.top[x+1] + (y+1)*bl + n) >> shift)
			}
		}
	case mode == 1: // DC
		sum := n
		for i := 1; i <= n; i++ {
			sum += r.left[i] + r.top[i]
		}
		dc := sum >> uint(log2(n)+1)
		for y := 0; y < n; y++ {
			row := dst[y*ds : y*ds+n]
			for x := range row {
				row[x] = byte(dc)
			}
		}
		if luma && n < 32 {
			dst[0] = byte((r.left[1] + 2*dc + r.top[1] + 2) >> 2)
			for x := 1; x < n; x++ {
				dst[x] = byte((r.top[x+1] + 3*dc + 2) >> 2)
			}
			for y := 1; y < n; y++ {
				dst[y*ds] = byte((r.left[y+1] + 3*dc + 2) >> 2)
			}
		}
	default:
		angle := intraPredAngle[mode]
		var refBuf [3*64 + 1]int
		const o = 64 // ref[k] is refBuf[o+k], k in [-64, 2n]
		vertical := mode >= 18
		main, side := r.top[:], r.left[:]
		if !vertical {
			main, side = r.left[:], r.top[:]
		}
		for x := 0; x <= n; x++ {
			refBuf[o+x] = main[x] // p[-1+x][-1]
		}
		if angle < 0 {
			last := (n * angle) >> 5
			if last < -1 {
				inv := invAngle[mode]
				for x := last; x <= -1; x++ {
					refBuf[o+x] = side[((x*inv + 128) >> 8)]
				}
			}
		} else {
			for x := n + 1; x <= 2*n; x++ {
				refBuf[o+x] = main[x]
			}
		}
		for y := 0; y < n; y++ {
			idx := ((y + 1) * angle) >> 5
			fact := ((y + 1) * angle) & 31
			for x := 0; x < n; x++ {
				var v int
				if fact != 0 {
					v = ((32-fact)*refBuf[o+x+idx+1] + fact*refBuf[o+x+idx+2] + 16) >> 5
				} else {
					v = refBuf[o+x+idx+1]
				}
				if vertical {
					dst[y*ds+x] = byte(v)
				} else {
					dst[x*ds+y] = byte(v)
				}
			}
		}
		if luma && n < 32 {
			if mode == 26 {
				for y := 0; y < n; y++ {
					dst[y*ds] = clipU8(r.top[1] + ((r.left[y+1] - r.left[0]) >> 1))
				}
			} else if mode == 10 {
				for x := 0; x < n; x++ {
					dst[x] = clipU8(r.left[1] + ((r.top[x+1] - r.top[0]) >> 1))
				}
			}
		}
	}
}

func log2(n int) int {
	switch n {
	case 4:
		return 2
	case 8:
		return 3
	case 16:
		return 4
	case 32:
		return 5
	case 64:
		return 6
	}
	l := 0
	for 1<<uint(l) < n {
		l++
	}
	return l
}
