package av1

import "math"

// Inverse transforms (spec 7.13.2 / 7.13.3).

var cos128Lookup [65]int32

func init() {
	for i := range cos128Lookup {
		cos128Lookup[i] = int32(math.Round(4096 * math.Cos(float64(i)*math.Pi/128)))
	}
}

func cos128(angle int) int32 {
	a := angle & 255
	switch {
	case a <= 64:
		return cos128Lookup[a]
	case a <= 128:
		return -cos128Lookup[128-a]
	case a <= 192:
		return -cos128Lookup[a-128]
	}
	return cos128Lookup[256-a]
}

func sin128(angle int) int32 { return cos128(angle - 64) }

func round12(x int64) int32 { return int32((x + 2048) >> 12) }

// bfly is B(a, b, angle, flip).
func bfly(t []int32, a, b, angle int, flip bool) {
	x, y := int64(t[a]), int64(t[b])
	c, s := int64(cos128(angle)), int64(sin128(angle))
	t[a] = round12(x*c - y*s)
	t[b] = round12(x*s + y*c)
	if flip {
		t[a], t[b] = t[b], t[a]
	}
}

// hada is H(a, b, flip).
func hada(t []int32, a, b int, flip bool) {
	if flip {
		a, b = b, a
	}
	x, y := t[a], t[b]
	t[a], t[b] = x+y, x-y
}

func brev(numBits, x int) int {
	r := 0
	for i := 0; i < numBits; i++ {
		r |= (x >> i & 1) << (numBits - 1 - i)
	}
	return r
}

// idct is the inverse DCT of 2^n values in place (spec 7.13.2.3).
func idct(t []int32, n int) {
	var cp [64]int32
	n0 := 1 << n
	copy(cp[:n0], t)
	for i := 0; i < n0; i++ {
		t[i] = cp[brev(n, i)]
	}
	if n == 6 {
		for i := 0; i < 16; i++ {
			bfly(t, 32+i, 63-i, 63-4*brev(4, i), false)
		}
	}
	if n >= 5 {
		for i := 0; i < 8; i++ {
			bfly(t, 16+i, 31-i, 6+(brev(3, 7-i)<<3), false)
		}
	}
	if n == 6 {
		for i := 0; i < 16; i++ {
			hada(t, 32+i*2, 33+i*2, i&1 != 0)
		}
	}
	if n >= 4 {
		for i := 0; i < 4; i++ {
			bfly(t, 8+i, 15-i, 12+(brev(2, 3-i)<<4), false)
		}
	}
	if n >= 5 {
		for i := 0; i < 8; i++ {
			hada(t, 16+2*i, 17+2*i, i&1 != 0)
		}
	}
	if n == 6 {
		for i := 0; i < 4; i++ {
			for j := 0; j < 2; j++ {
				bfly(t, 62-i*4-j, 33+i*4+j, 60-16*brev(2, i)+64*j, true)
			}
		}
	}
	if n >= 3 {
		for i := 0; i < 2; i++ {
			bfly(t, 4+i, 7-i, 56-32*i, false)
		}
	}
	if n >= 4 {
		for i := 0; i < 4; i++ {
			hada(t, 8+2*i, 9+2*i, i&1 != 0)
		}
	}
	if n >= 5 {
		for i := 0; i < 2; i++ {
			for j := 0; j < 2; j++ {
				bfly(t, 30-4*i-j, 17+4*i+j, 24+(j<<6)+((1-i)<<5), true)
			}
		}
	}
	if n == 6 {
		for i := 0; i < 8; i++ {
			for j := 0; j < 2; j++ {
				hada(t, 32+i*4+j, 35+i*4-j, i&1 != 0)
			}
		}
	}
	for i := 0; i < 2; i++ {
		bfly(t, 2*i, 2*i+1, 32+16*i, i == 0)
	}
	if n >= 3 {
		for i := 0; i < 2; i++ {
			hada(t, 4+2*i, 5+2*i, i != 0)
		}
	}
	if n >= 4 {
		for i := 0; i < 2; i++ {
			bfly(t, 14-i, 9+i, 48+64*i, true)
		}
	}
	if n >= 5 {
		for i := 0; i < 4; i++ {
			for j := 0; j < 2; j++ {
				hada(t, 16+4*i+j, 19+4*i-j, i&1 != 0)
			}
		}
	}
	if n == 6 {
		for i := 0; i < 2; i++ {
			for j := 0; j < 4; j++ {
				bfly(t, 61-i*8-j, 34+i*8+j, 56-i*32+(j>>1)*64, true)
			}
		}
	}
	for i := 0; i < 2; i++ {
		hada(t, i, 3-i, false)
	}
	if n >= 3 {
		bfly(t, 6, 5, 32, true)
	}
	if n >= 4 {
		for i := 0; i < 2; i++ {
			for j := 0; j < 2; j++ {
				hada(t, 8+4*i+j, 11+4*i-j, i != 0)
			}
		}
	}
	if n >= 5 {
		for i := 0; i < 4; i++ {
			bfly(t, 29-i, 18+i, 48+(i>>1)*64, true)
		}
	}
	if n == 6 {
		for i := 0; i < 4; i++ {
			for j := 0; j < 4; j++ {
				hada(t, 32+8*i+j, 39+8*i-j, i&1 != 0)
			}
		}
	}
	if n >= 3 {
		for i := 0; i < 4; i++ {
			hada(t, i, 7-i, false)
		}
	}
	if n >= 4 {
		for i := 0; i < 2; i++ {
			bfly(t, 13-i, 10+i, 32, true)
		}
	}
	if n >= 5 {
		for i := 0; i < 2; i++ {
			for j := 0; j < 4; j++ {
				hada(t, 16+i*8+j, 23+i*8-j, i != 0)
			}
		}
	}
	if n == 6 {
		for i := 0; i < 8; i++ {
			a := 112
			if i < 4 {
				a = 48
			}
			bfly(t, 59-i, 36+i, a, true)
		}
	}
	if n >= 4 {
		for i := 0; i < 8; i++ {
			hada(t, i, 15-i, false)
		}
	}
	if n >= 5 {
		for i := 0; i < 4; i++ {
			bfly(t, 27-i, 20+i, 32, true)
		}
	}
	if n == 6 {
		for i := 0; i < 8; i++ {
			hada(t, 32+i, 47-i, false)
			hada(t, 48+i, 63-i, true)
		}
	}
	if n >= 5 {
		for i := 0; i < 16; i++ {
			hada(t, i, 31-i, false)
		}
	}
	if n == 6 {
		for i := 0; i < 8; i++ {
			bfly(t, 55-i, 40+i, 32, true)
		}
	}
	if n == 6 {
		for i := 0; i < 32; i++ {
			hada(t, i, 63-i, false)
		}
	}
}

const (
	sinpi19 = 1321
	sinpi29 = 2482
	sinpi39 = 3344
	sinpi49 = 3803
)

func iadst4(t []int32) {
	x0, x1, x2, x3 := int64(t[0]), int64(t[1]), int64(t[2]), int64(t[3])
	s0 := sinpi19 * x0
	s1 := sinpi29 * x0
	s2 := sinpi39 * x1
	s3 := sinpi49 * x2
	s4 := sinpi19 * x2
	s5 := sinpi29 * x3
	s6 := sinpi49 * x3
	b7 := x0 - x2 + x3
	s0 += s3
	s1 -= s4
	s3 = s2
	s2 = sinpi39 * b7
	s0 += s5
	s1 -= s6
	t[0] = round12(s0 + s3)
	t[1] = round12(s1 + s3)
	t[2] = round12(s2)
	t[3] = round12(s0 + s1 - s3)
}

var (
	adstOutPerm8  = [8]int{0, 4, 6, 2, 3, 7, 5, 1}
	adstOutPerm16 = [16]int{0, 8, 12, 4, 6, 14, 10, 2, 3, 11, 15, 7, 5, 13, 9, 1}
)

func adstInPerm(t []int32, n0 int) {
	var cp [16]int32
	copy(cp[:n0], t)
	for i := 0; i < n0; i++ {
		if i&1 != 0 {
			t[i] = cp[i-1]
		} else {
			t[i] = cp[n0-i-1]
		}
	}
}

func adstOutPerm(t []int32, perm []int) {
	var cp [16]int32
	copy(cp[:len(perm)], t)
	for i, p := range perm {
		if i&1 != 0 {
			t[i] = -cp[p]
		} else {
			t[i] = cp[p]
		}
	}
}

func iadst8(t []int32) {
	adstInPerm(t, 8)
	for i := 0; i < 4; i++ {
		bfly(t, 2*i, 2*i+1, 60-16*i, true)
	}
	for i := 0; i < 4; i++ {
		hada(t, i, 4+i, false)
	}
	for i := 0; i < 2; i++ {
		bfly(t, 4+3*i, 5+i, 48-32*i, true)
	}
	for i := 0; i < 2; i++ {
		for j := 0; j < 2; j++ {
			hada(t, 4*j+i, 2+4*j+i, false)
		}
	}
	for i := 0; i < 2; i++ {
		bfly(t, 2+4*i, 3+4*i, 32, true)
	}
	adstOutPerm(t, adstOutPerm8[:])
}

func iadst16(t []int32) {
	adstInPerm(t, 16)
	for i := 0; i < 8; i++ {
		bfly(t, 2*i, 2*i+1, 62-8*i, true)
	}
	for i := 0; i < 8; i++ {
		hada(t, i, 8+i, false)
	}
	for i := 0; i < 2; i++ {
		bfly(t, 8+2*i, 9+2*i, 56-32*i, true)
		bfly(t, 13+2*i, 12+2*i, 8+32*i, true)
	}
	for i := 0; i < 4; i++ {
		for j := 0; j < 2; j++ {
			hada(t, 8*j+i, 4+8*j+i, false)
		}
	}
	for i := 0; i < 2; i++ {
		for j := 0; j < 2; j++ {
			bfly(t, 4+8*j+3*i, 5+8*j+i, 48-32*i, true)
		}
	}
	for i := 0; i < 2; i++ {
		for j := 0; j < 4; j++ {
			hada(t, 4*j+i, 2+4*j+i, false)
		}
	}
	for i := 0; i < 4; i++ {
		bfly(t, 2+4*i, 3+4*i, 32, true)
	}
	adstOutPerm(t, adstOutPerm16[:])
}
