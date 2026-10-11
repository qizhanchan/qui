package hevc

// Scaling lists (spec 7.3.4, 7.4.5) and the derived ScalingFactor arrays.

var defaultIntra8 = [64]uint8{
	16, 16, 16, 16, 16, 16, 16, 16, 16, 16, 17, 16, 17, 16, 17, 18,
	17, 18, 18, 17, 18, 21, 19, 20, 21, 20, 19, 21, 24, 22, 22, 24,
	24, 22, 22, 24, 25, 25, 27, 30, 27, 25, 25, 29, 31, 35, 35, 31,
	29, 36, 41, 44, 41, 36, 47, 54, 54, 47, 65, 70, 65, 88, 88, 115,
}

var defaultInter8 = [64]uint8{
	16, 16, 16, 16, 16, 16, 16, 16, 16, 16, 17, 17, 17, 17, 17, 18,
	18, 18, 18, 18, 18, 20, 20, 20, 20, 20, 20, 20, 24, 24, 24, 24,
	24, 24, 24, 24, 25, 25, 25, 25, 25, 25, 25, 28, 28, 28, 28, 28,
	28, 33, 33, 33, 33, 33, 41, 41, 41, 41, 54, 54, 54, 71, 71, 91,
}

// scalingList holds ScalingList[sizeId][matrixId][i] (coefficients in
// up-right diagonal order) and the DC values for 16x16 / 32x32, plus the
// derived per-position factors.
type scalingList struct {
	list [4][6][64]uint8
	dc   [4][6]uint8
	// factor[sizeId][matrixId] is the raster m[x][y] (index y*n+x) for
	// n = 4 << sizeId.
	factor [4][6][]uint8
}

func defaultScalingList() *scalingList {
	s := &scalingList{}
	for size := 0; size < 4; size++ {
		for m := 0; m < 6; m++ {
			s.setDefault(size, m)
		}
	}
	s.derive()
	return s
}

func (s *scalingList) setDefault(size, m int) {
	if size == 0 {
		for i := 0; i < 16; i++ {
			s.list[0][m][i] = 16
		}
		return
	}
	if m < 3 {
		s.list[size][m] = defaultIntra8
	} else {
		s.list[size][m] = defaultInter8
	}
	s.dc[size][m] = 16
}

func parseScalingList(r *bitReader) (*scalingList, error) {
	s := &scalingList{}
	for size := 0; size < 4; size++ {
		step := 1
		if size == 3 {
			step = 3
		}
		for m := 0; m < 6; m += step {
			if !r.flag() { // scaling_list_pred_mode_flag
				delta := int(r.ue())
				if delta == 0 {
					s.setDefault(size, m)
				} else {
					ref := m - delta*step
					if ref < 0 {
						return nil, errCorrupt
					}
					s.list[size][m] = s.list[size][ref]
					s.dc[size][m] = s.dc[size][ref]
				}
				continue
			}
			next := 8
			n := min(64, 1<<(4+size<<1))
			if size > 1 {
				dc := int(r.se()) + 8
				if dc < 1 || dc > 255 {
					return nil, errCorrupt
				}
				next = dc
				s.dc[size][m] = uint8(dc)
			}
			for i := 0; i < n; i++ {
				d := int(r.se())
				next = (next + d + 256) % 256
				s.list[size][m][i] = uint8(next)
			}
			if size <= 1 {
				s.dc[size][m] = s.list[size][m][0]
			}
		}
	}
	// 32x32 chroma matrices (not coded for 4:2:0) mirror 16x16.
	for _, m := range []int{1, 2, 4, 5} {
		s.list[3][m] = s.list[2][m]
		s.dc[3][m] = s.dc[2][m]
	}
	if r.e != nil {
		return nil, r.e
	}
	s.derive()
	return s, nil
}

// diagScan returns the up-right diagonal scan of an n x n block as (x, y)
// pairs (6.5.3).
func diagScan(n int) [][2]uint8 {
	out := make([][2]uint8, 0, n*n)
	x, y := 0, 0
	for len(out) < n*n {
		for y >= 0 {
			if x < n && y < n {
				out = append(out, [2]uint8{uint8(x), uint8(y)})
			}
			y--
			x++
		}
		y = x
		x = 0
	}
	return out
}

// derive computes ScalingFactor (7.4.5).
func (s *scalingList) derive() {
	d4 := diagScan(4)
	d8 := diagScan(8)
	for size := 0; size < 4; size++ {
		n := 4 << size
		for m := 0; m < 6; m++ {
			f := make([]uint8, n*n)
			switch size {
			case 0:
				for i, p := range d4 {
					f[int(p[1])*4+int(p[0])] = s.list[0][m][i]
				}
			case 1:
				for i, p := range d8 {
					f[int(p[1])*8+int(p[0])] = s.list[1][m][i]
				}
			default:
				rep := n / 8
				for i, p := range d8 {
					for k := 0; k < rep; k++ {
						for j := 0; j < rep; j++ {
							x, y := int(p[0])*rep+j, int(p[1])*rep+k
							f[y*n+x] = s.list[size][m][i]
						}
					}
				}
				f[0] = s.dc[size][m]
			}
			s.factor[size][m] = f
		}
	}
}
