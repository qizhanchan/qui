package h264

import "math/bits"

// cabac is the CABAC arithmetic decoding engine (spec 9.3.1.2, 9.3.3.2). It
// follows the spec's 9-bit codIOffset formulation exactly, so the bit
// position is always known (needed for I_PCM). Bits come from a 64-bit
// cache refilled 32 bits at a time rather than from the bitReader per bin.
type cabac struct {
	r      *bitReader
	rng    uint32
	off    uint32
	states [1024]uint8 // pStateIdx<<1 | valMPS

	cache   uint64 // next bits, MSB first
	cbits   int    // valid bits in cache
	data    []byte
	bytePos int // next byte of data to load
}

// State-transition tables indexed by the full state byte (pStateIdx<<1 |
// valMPS), sized 256 so a uint8 index needs no bounds check. lpsRange
// holds rangeTabLPS for the state's pStateIdx.
var (
	lpsRange [256][4]uint8
	nextMPS  [256]uint8
	nextLPS  [256]uint8
)

func init() {
	for s := 0; s < 128; s++ {
		st, mps := s>>1, s&1
		for q := 0; q < 4; q++ {
			lpsRange[s][q] = uint8(rangeTabLPS[st][q])
		}
		nextMPS[s] = transIdxMPS[st]<<1 | uint8(mps)
		if st == 0 {
			mps ^= 1
		}
		nextLPS[s] = transIdxLPS[st]<<1 | uint8(mps)
	}
}

func (c *cabac) refill() {
	for c.cbits <= 56 {
		var b byte
		if c.bytePos < len(c.data) {
			b = c.data[c.bytePos]
		}
		c.cache |= uint64(b) << uint(56-c.cbits)
		c.cbits += 8
		c.bytePos++
	}
}

func (c *cabac) readBits(n int) uint32 {
	if c.cbits < n {
		c.refill()
	}
	v := uint32(c.cache >> uint(64-n))
	c.cache <<= uint(n)
	c.cbits -= n
	return v
}

// pos is the bit position of the next unread bit.
func (c *cabac) pos() int { return c.bytePos*8 - c.cbits }

// sync moves the bitReader to the engine's position (before I_PCM).
func (c *cabac) sync() { c.r.pos = c.pos() }

// start initialises the decoding engine at the current (byte-aligned)
// position.
func (c *cabac) start(r *bitReader) error {
	c.r = r
	c.rng = 510
	if r.bitsLeft() < 9 {
		return errShort
	}
	c.data = r.data
	c.bytePos = r.pos >> 3
	c.cache, c.cbits = 0, 0
	c.refill()
	c.off = c.readBits(9)
	if c.off >= 510 {
		return errf("h264: bad CABAC initial offset")
	}
	return nil
}

// initContexts sets the context variables for a slice (spec 9.3.1.1).
func (c *cabac) initContexts(sliceType, initIDC, qp int) {
	tab := &cabacContextInitI
	if sliceType != sliceI {
		tab = &cabacContextInitPB[initIDC]
	}
	qp = clip3(0, 51, qp)
	for i := range c.states {
		m, n := int(tab[i][0]), int(tab[i][1])
		pre := clip3(1, 126, ((m*qp)>>4)+n)
		if pre <= 63 {
			c.states[i] = uint8((63 - pre) << 1)
		} else {
			c.states[i] = uint8((pre-64)<<1 | 1)
		}
	}
}

// decision decodes one context-coded bin.
func (c *cabac) decision(ctx int) int {
	st := &c.states[ctx&1023]
	s := *st
	lps := uint32(lpsRange[s][(c.rng>>6)&3])
	c.rng -= lps
	if c.off < c.rng {
		*st = nextMPS[s]
		if c.rng < 256 {
			c.rng <<= 1
			c.off = c.off<<1 | c.readBits(1)
		}
		return int(s & 1)
	}
	c.off -= c.rng
	c.rng = lps
	*st = nextLPS[s]
	n := bits.LeadingZeros32(lps) - 23
	c.rng <<= uint(n)
	c.off = c.off<<uint(n) | c.readBits(n)
	return int(s&1) ^ 1
}

func (c *cabac) bypass() int {
	c.off = c.off<<1 | c.readBits(1)
	if c.off >= c.rng {
		c.off -= c.rng
		return 1
	}
	return 0
}

func (c *cabac) terminate() int {
	c.rng -= 2
	if c.off >= c.rng {
		return 1
	}
	if c.rng < 256 {
		c.rng <<= 1
		c.off = c.off<<1 | c.readBits(1)
	}
	return 0
}

// overrun reports whether the engine has read past the end of the slice
// data (a corrupt stream).
func (c *cabac) overrun() bool { return c.pos() > c.r.nbits+16 }

// Residual block categories (ctxBlockCat, Table 9-42).
const (
	catLumaDC   = 0
	catLumaAC   = 1
	catLuma4x4  = 2
	catChromaDC = 3
	catChromaAC = 4
	catLuma8x8  = 5
)

var (
	cbfCatOffset   = [6]int{0, 4, 8, 12, 16, 0}
	sigCatOffset   = [6]int{0, 15, 29, 44, 47, 0}
	absCatOffset   = [6]int{0, 10, 20, 30, 39, 0}
	sig8x8CtxInc   = [63]uint8{0, 1, 2, 3, 4, 5, 5, 4, 4, 3, 3, 4, 4, 4, 5, 5, 4, 4, 4, 4, 3, 3, 6, 7, 7, 7, 8, 9, 10, 9, 8, 7, 7, 6, 11, 12, 13, 11, 6, 7, 8, 9, 14, 10, 9, 8, 6, 11, 12, 13, 11, 6, 9, 14, 10, 9, 11, 12, 13, 11, 14, 10, 12}
	last8x8CtxInc  = [63]uint8{0, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 2, 2, 2, 2, 2, 2, 2, 2, 2, 2, 2, 2, 2, 2, 2, 2, 3, 3, 3, 3, 3, 3, 3, 3, 4, 4, 4, 4, 4, 4, 4, 4, 5, 5, 5, 5, 6, 6, 6, 6, 7, 7, 7, 7, 8, 8, 8}
	absLevel1Ctx   = [8]uint8{1, 2, 3, 4, 0, 0, 0, 0}
	absLevelGt1Ctx = [2][8]uint8{{5, 5, 5, 5, 6, 7, 8, 9}, {5, 5, 5, 5, 6, 7, 8, 8}}
	absLevelTrans  = [2][8]uint8{{1, 2, 3, 3, 4, 5, 6, 7}, {4, 4, 4, 4, 5, 6, 7, 7}}
)

// residual decodes the significance map and levels of one block
// (spec 7.3.5.3.3, coded_block_flag already decoded as 1). coeff receives
// the levels in scan order. It returns the number of non-zero coefficients.
func (c *cabac) residual(cat, maxNumCoeff int, coeff []int32) int {
	var sigBase, lastBase, absBase int
	if cat == catLuma8x8 {
		sigBase, lastBase, absBase = 402, 417, 426
	} else {
		sigBase = 105 + sigCatOffset[cat]
		lastBase = 166 + sigCatOffset[cat]
		absBase = 227 + absCatOffset[cat]
	}
	var pos [64]uint8
	n := 0
	last := maxNumCoeff - 1
	for i := 0; i < last; i++ {
		var sc, lc int
		if cat == catLuma8x8 {
			sc, lc = sigBase+int(sig8x8CtxInc[i]), lastBase+int(last8x8CtxInc[i])
		} else {
			sc, lc = sigBase+i, lastBase+i
		}
		if c.decision(sc) == 1 {
			pos[n] = uint8(i)
			n++
			if c.decision(lc) == 1 {
				goto levels
			}
		}
	}
	pos[n] = uint8(last)
	n++
levels:
	isDC := 0
	if cat == catChromaDC {
		isDC = 1
	}
	node := 0
	for k := n - 1; k >= 0; k-- {
		var v int32
		if c.decision(absBase+int(absLevel1Ctx[node])) == 0 {
			node = int(absLevelTrans[0][node])
			v = 1
		} else {
			ctx := absBase + int(absLevelGt1Ctx[isDC][node])
			node = int(absLevelTrans[1][node])
			abs := 2
			for abs < 15 && c.decision(ctx) == 1 {
				abs++
			}
			if abs >= 15 {
				k := 0
				for c.bypass() == 1 {
					k++
					if k > 24 {
						break
					}
				}
				suf := (1 << uint(k)) - 1
				for k > 0 {
					k--
					suf += c.bypass() << uint(k)
				}
				abs = 15 + suf
			}
			v = int32(abs)
		}
		if c.bypass() == 1 {
			v = -v
		}
		coeff[pos[k]] = v
	}
	return n
}
