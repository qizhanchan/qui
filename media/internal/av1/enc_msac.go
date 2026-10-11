package av1

import (
	"math"
	"math/bits"
	"unsafe"
)

// msacEnc is the multi-symbol range encoder matching msac (libaom's
// od_ec_enc): low is the window's lower bound, precarry collects output
// in 16-bit units so a carry can still propagate.
type msacEnc struct {
	precarry []uint16
	low      uint64
	rng      uint32
	cnt      int
}

func (e *msacEnc) init() {
	e.precarry = e.precarry[:0]
	e.low, e.rng, e.cnt = 0, 0x8000, -9
}

// encode codes symbol s of an n-symbol inverted CDF (no adaptation).
func (e *msacEnc) encode(cdf []uint16, s, n int) {
	l, r := e.low, e.rng
	fh := uint32(cdf[s])
	if s > 0 {
		fl := uint32(cdf[s-1])
		u := (r>>8)*(fl>>6)>>1 + 4*uint32(n-s)
		v := (r>>8)*(fh>>6)>>1 + 4*uint32(n-1-s)
		l += uint64(r - u)
		r = u - v
	} else {
		r -= (r>>8)*(fh>>6)>>1 + 4*uint32(n-1)
	}
	e.normalize(l, r)
}

func (e *msacEnc) normalize(low uint64, rng uint32) {
	d := 16 - (32 - bits.LeadingZeros32(rng))
	c := e.cnt
	s := c + d
	if s >= 0 {
		c += 16
		m := uint64(1)<<uint(c) - 1
		if s >= 8 {
			e.precarry = append(e.precarry, uint16(low>>uint(c)))
			low &= m
			c -= 8
			m >>= 8
		}
		e.precarry = append(e.precarry, uint16(low>>uint(c)))
		s = c + d - 24
		low &= m
	}
	e.low = low << uint(d)
	e.rng = rng << uint(d)
	e.cnt = s
}

// sym codes s and adapts the CDF as the decoder does.
func (e *msacEnc) sym(cdf []uint16, s, n int) {
	e.encode(cdf, s, n)
	adapt(cdf, s, n)
}

func (e *msacEnc) bit(b int) { e.encode(literalCDF[:], b, 2) }

// done flushes the coder and returns the tile data: enough bits for the
// decoder to resolve every symbol, then a one bit and zero padding (as
// libaom's od_ec_enc_done).
func (e *msacEnc) done() []byte {
	l := e.low
	c := e.cnt
	s := 10
	m := uint64(0x3fff)
	en := (l+m)&^m | (m + 1)
	s += c
	if s > 0 {
		n := uint64(1)<<uint(c+16) - 1
		for {
			e.precarry = append(e.precarry, uint16(en>>uint(c+16)))
			en &= n
			s -= 8
			c -= 8
			n >>= 8
			if s <= 0 {
				break
			}
		}
	}
	out := make([]byte, len(e.precarry))
	carry := uint32(0)
	for i := len(e.precarry) - 1; i >= 0; i-- {
		carry += uint32(e.precarry[i])
		out[i] = byte(carry)
		carry >>= 8
	}
	return out
}

// symCostTab[p] is -log2(p / 32768) in 1/256 bit units.
var symCostTab = func() (t [32769]int32) {
	for p := 1; p <= 32768; p++ {
		t[p] = int32(math.Round(-math.Log2(float64(p)/32768) * 256))
	}
	t[0] = t[1]
	return
}()

// symCost is the cost of symbol s of an n-symbol CDF in 1/256 bits.
func symCost(cdf []uint16, s int) int32 {
	hi := 32768
	if s > 0 {
		hi = int(cdf[s-1])
	}
	return symCostTab[max(hi-int(cdf[s]), 1)]
}

// Symbol sinks. The encoder's syntax writers emit through a symSink:
// with record set they log every symbol (adapting the live CDFs, as the
// decoder will), otherwise they only add up its estimated cost. A frame's
// records are replayed into the range encoder once the in-loop filter
// choices that the tile data carries (CDEF indices) are known.
type symSink struct {
	record bool
	cost   int64 // 1/256 bits
	recs   []symRec
}

const (
	recSym  = iota // adaptive symbol: cdf, n, s
	recBool        // non-adaptive binary symbol of probability val (icdf[0])
	recLit         // n literal bits of val
	recCdef        // the CDEF index of the 64x64 at f.cdefIdx[val]
)

type symRec struct {
	cdf  *uint16
	kind uint8
	n    uint8
	s    uint8
	val  uint32
}

func (k *symSink) sym(cdf []uint16, s, n int) {
	if !k.record {
		k.cost += int64(symCost(cdf, s))
		return
	}
	k.recs = append(k.recs, symRec{cdf: &cdf[0], kind: recSym, n: uint8(n), s: uint8(s)})
	adapt(cdf, s, n)
}

func (k *symSink) flag(cdf []uint16, b bool) {
	s := 0
	if b {
		s = 1
	}
	k.sym(cdf, s, 2)
}

// boolProb codes b with the fixed inverted probability p (icdf[0]).
func (k *symSink) boolProb(p int, b bool) {
	s := 0
	if b {
		s = 1
	}
	if !k.record {
		k.cost += int64(symCost([]uint16{uint16(p), 0}, s))
		return
	}
	k.recs = append(k.recs, symRec{kind: recBool, s: uint8(s), val: uint32(p)})
}

// lit codes the n-bit literal v (L(n)).
func (k *symSink) lit(n, v int) {
	if !k.record {
		k.cost += int64(n) * 256
		return
	}
	if n > 0 {
		k.recs = append(k.recs, symRec{kind: recLit, n: uint8(n), val: uint32(v)})
	}
}

func (k *symSink) cdefPlaceholder(idx, bits int) {
	if !k.record {
		k.cost += int64(bits) * 256
		return
	}
	k.recs = append(k.recs, symRec{kind: recCdef, val: uint32(idx)})
}

// replay codes the records with e. The CDFs the records point into must
// hold the state they had when recording started.
func (k *symSink) replay(e *msacEnc, cdefIdx []int8, cdefBits int) {
	for i := range k.recs {
		r := &k.recs[i]
		switch r.kind {
		case recSym:
			n := int(r.n)
			cdf := unsafe.Slice(r.cdf, n+1)
			e.sym(cdf, int(r.s), n)
		case recBool:
			e.encode([]uint16{uint16(r.val), 0}, int(r.s), 2)
		case recLit:
			for b := int(r.n) - 1; b >= 0; b-- {
				e.bit(int(r.val>>uint(b)) & 1)
			}
		case recCdef:
			v := int(cdefIdx[r.val])
			for b := cdefBits - 1; b >= 0; b-- {
				e.bit(v >> uint(b) & 1)
			}
		}
	}
}
