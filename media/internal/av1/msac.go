package av1

import "math/bits"

// msac is the multi-symbol arithmetic decoder (spec 8.2), in libaom's
// windowed form: dif holds the inverted bitstream left-aligned in 64 bits.
type msac struct {
	buf     []byte
	pos     int
	dif     uint64
	rng     uint32
	cnt     int
	noAdapt bool
}

func (m *msac) init(b []byte, disableUpdate bool) {
	m.buf, m.pos = b, 0
	m.dif = 1<<63 - 1
	m.rng = 0x8000
	m.cnt = -15
	m.noAdapt = disableUpdate
	m.refill()
}

func (m *msac) refill() {
	s := 64 - 9 - (m.cnt + 15)
	for ; s >= 0 && m.pos < len(m.buf); s -= 8 {
		m.dif ^= uint64(m.buf[m.pos]) << uint(s)
		m.pos++
		m.cnt += 8
	}
	if m.pos >= len(m.buf) {
		m.cnt = 0x4000
	}
}

func (m *msac) normalize(rng uint32) {
	d := bits.LeadingZeros32(rng) - 16
	m.cnt -= d
	m.dif = ((m.dif + 1) << uint(d)) - 1
	m.rng = rng << uint(d)
	if m.cnt < 0 {
		m.refill()
	}
}

// decode reads a symbol of an n-symbol inverted CDF without adapting it.
func (m *msac) decode(cdf []uint16, n int) int {
	c := uint32(m.dif >> 48)
	r := m.rng >> 8
	u, v := m.rng, m.rng
	ret := 0
	for ; ; ret++ {
		u = v
		v = (r*uint32(cdf[ret]>>6))>>1 + 4*uint32(n-1-ret)
		if c >= v {
			break
		}
	}
	m.dif -= uint64(v) << 48
	m.normalize(u - v)
	return ret
}

// sym reads a symbol of an n-symbol CDF and adapts the CDF.
func (m *msac) sym(cdf []uint16, n int) int {
	s := m.decode(cdf, n)
	if !m.noAdapt {
		adapt(cdf, s, n)
	}
	return s
}

// flag reads a binary adaptive symbol.
func (m *msac) flag(cdf []uint16) bool { return m.sym(cdf, 2) != 0 }

func adapt(cdf []uint16, s, n int) {
	cnt := cdf[n]
	rate := 3 + int(cnt>>4&1|cnt>>5&1) + min(bits.Len(uint(n))-1, 2)
	if cnt > 31 {
		rate = 5 + min(bits.Len(uint(n))-1, 2)
	}
	for i := 0; i < n-1; i++ {
		if i < s {
			cdf[i] += (32768 - cdf[i]) >> uint(rate)
		} else {
			cdf[i] -= cdf[i] >> uint(rate)
		}
	}
	if cnt < 32 {
		cdf[n] = cnt + 1
	}
}

var literalCDF = [3]uint16{16384, 0, 0}

// bit reads one equiprobable bit, L(1).
func (m *msac) bit() int { return m.decode(literalCDF[:], 2) }

// lit reads an n-bit literal, L(n).
func (m *msac) lit(n int) int {
	v := 0
	for ; n > 0; n-- {
		v = v<<1 | m.bit()
	}
	return v
}

// ns reads NS(n): a non-symmetric value below n from literal bits.
func (m *msac) ns(n int) int {
	w := bits.Len(uint(n))
	k := (1 << w) - n
	v := m.lit(w - 1)
	if v < k {
		return v
	}
	return v<<1 - k + m.bit()
}
