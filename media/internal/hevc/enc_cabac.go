package hevc

import "math"

// CABAC encoding (the arithmetic coder is H.264's) and a bit estimator
// with the same interface, so every syntax writer serves both rate
// estimation and coding.

// binSink receives the bins of syntax elements.
type binSink interface {
	decision(ctx int, bin int)
	bypass(bin int)
	bypassBits(v, n int) // the n low bits of v, most significant first
	terminate(bin int)
	contexts() *[numCtx]uint8
}

// cabacEnc is the arithmetic coder writing into a bitWriter.
type cabacEnc struct {
	w           *bitWriter
	low, rng    uint32
	outstanding int
	first       bool
	ctx         [numCtx]uint8
}

// start begins a substream at a byte boundary of w (contexts are set by
// the caller).
func (c *cabacEnc) start(w *bitWriter) {
	c.w = w
	c.low, c.rng = 0, 510
	c.outstanding = 0
	c.first = true
}

func (c *cabacEnc) contexts() *[numCtx]uint8 { return &c.ctx }

func (c *cabacEnc) putBit(b uint32) {
	if c.first {
		c.first = false
	} else {
		c.w.u(1, b)
	}
	for ; c.outstanding > 0; c.outstanding-- {
		c.w.u(1, 1-b)
	}
}

func (c *cabacEnc) renorm() {
	for c.rng < 256 {
		switch {
		case c.low < 256:
			c.putBit(0)
		case c.low >= 512:
			c.low -= 512
			c.putBit(1)
		default:
			c.low -= 256
			c.outstanding++
		}
		c.rng <<= 1
		c.low <<= 1
	}
}

func (c *cabacEnc) decision(ctx int, bin int) {
	st := &c.ctx[ctx]
	s := *st
	lps := uint32(lpsRange[s][(c.rng>>6)&3])
	c.rng -= lps
	if uint8(bin) != s&1 {
		c.low += c.rng
		c.rng = lps
		*st = nextLPS[s]
	} else {
		*st = nextMPS[s]
	}
	c.renorm()
}

func (c *cabacEnc) bypass(bin int) {
	c.low <<= 1
	if bin != 0 {
		c.low += c.rng
	}
	switch {
	case c.low >= 1024:
		c.putBit(1)
		c.low -= 1024
	case c.low < 512:
		c.putBit(0)
	default:
		c.low -= 512
		c.outstanding++
	}
}

func (c *cabacEnc) bypassBits(v, n int) {
	for i := n - 1; i >= 0; i-- {
		c.bypass(v >> uint(i) & 1)
	}
}

// terminate codes a terminating bin. A 1 ends the arithmetic codeword:
// the flush's final bit is the rbsp_stop_one_bit or the
// alignment_bit_equal_to_one, and the caller pads with zeros.
func (c *cabacEnc) terminate(bin int) {
	c.rng -= 2
	if bin != 0 {
		c.low += c.rng
		c.rng = 2
		c.renorm()
		c.putBit((c.low >> 9) & 1)
		c.w.u(2, ((c.low>>7)&3)|1)
		return
	}
	c.renorm()
}

// fracBits[state][bin] is the cost of coding bin in a context with that
// state, in 1/32768 bit.
var fracBits [128][2]uint32

const bitScale = 1 << 15

func init() {
	alpha := math.Pow(0.01875/0.5, 1.0/63)
	for s := 0; s < 128; s++ {
		pLPS := 0.5 * math.Pow(alpha, float64(s>>1))
		mps := s & 1
		fracBits[s][mps] = uint32(-math.Log2(1-pLPS)*bitScale + 0.5)
		fracBits[s][1-mps] = uint32(-math.Log2(pLPS)*bitScale + 0.5)
	}
}

// cabacEst estimates the bits of coded bins, evolving its own contexts.
type cabacEst struct {
	ctx  [numCtx]uint8
	bits uint64 // 1/32768 bit
}

func (e *cabacEst) contexts() *[numCtx]uint8 { return &e.ctx }

func (e *cabacEst) decision(ctx int, bin int) {
	st := &e.ctx[ctx]
	s := *st
	e.bits += uint64(fracBits[s][bin&1])
	if uint8(bin) != s&1 {
		*st = nextLPS[s]
	} else {
		*st = nextMPS[s]
	}
}

func (e *cabacEst) bypass(int)              { e.bits += bitScale }
func (e *cabacEst) bypassBits(_ int, n int) { e.bits += uint64(n) * bitScale }
func (e *cabacEst) terminate(int)           { e.bits += 1 << 6 } // ~0.002 bit
