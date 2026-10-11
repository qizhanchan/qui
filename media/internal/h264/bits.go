// Package h264 is qui's self-written, pure-Go H.264/AVC decoder.
//
// It decodes progressive (frame_mbs_only) 8-bit 4:2:0 streams of the
// Constrained Baseline, Main and High profiles: CAVLC and CABAC entropy
// coding, I/P/B slices, multiple slices per picture, the 8x8 transform,
// custom scaling matrices, explicit and implicit weighted prediction,
// spatial and temporal direct prediction, long-term references and
// memory-management control operations, and display-order output.
//
// Correctness is checked plane-exact against FFmpeg's decoder (see the
// tests). Nothing here depends on cgo, root qui, or any third-party module.
package h264

import (
	"encoding/binary"
	"math/bits"
)

// bitReader reads big-endian bits from an RBSP. The data slice carries at
// least 8 bytes of zero padding past nbits so the fast paths can always load
// a full 64-bit word. The first error is latched.
type bitReader struct {
	data  []byte
	pos   int // bit offset
	nbits int // number of real bits
	e     error
	// lastOne is the position of the last set bit (rbsp_stop_one_bit).
	lastOne int
}

func newBitReader(rbsp []byte) *bitReader {
	r := &bitReader{}
	r.reset(rbsp)
	return r
}

// reset points the reader at rbsp, copying it into a padded buffer.
func (r *bitReader) reset(rbsp []byte) {
	n := len(rbsp)
	if cap(r.data) < n+8 {
		r.data = make([]byte, n+8, n+64)
	}
	r.data = r.data[:n+8]
	copy(r.data, rbsp)
	clear(r.data[n:])
	r.pos = 0
	r.nbits = n * 8
	r.e = nil
	r.lastOne = -1
	for i := n - 1; i >= 0; i-- {
		if b := rbsp[i]; b != 0 {
			r.lastOne = i*8 + 7 - bits.TrailingZeros8(b)
			break
		}
	}
}

func (r *bitReader) err() error { return r.e }

func (r *bitReader) bitsLeft() int { return r.nbits - r.pos }

// peek32 returns the next 32 bits (zero padded past the end).
func (r *bitReader) peek32() uint32 {
	i := r.pos >> 3
	if i+8 > len(r.data) {
		return 0
	}
	v := binary.BigEndian.Uint64(r.data[i:])
	return uint32((v << uint(r.pos&7)) >> 32)
}

// peekBits returns the next n (1..32) bits without consuming them.
func (r *bitReader) peekBits(n int) uint32 {
	return r.peek32() >> uint(32-n)
}

// u reads n (0..32) bits.
func (r *bitReader) u(n int) uint32 {
	if n == 0 || r.e != nil {
		return 0
	}
	if r.pos+n > r.nbits {
		r.e = errShort
		r.pos = r.nbits
		return 0
	}
	v := r.peek32() >> uint(32-n)
	r.pos += n
	return v
}

func (r *bitReader) u1() bool { return r.u(1) == 1 }

func (r *bitReader) skip(n int) {
	if r.pos+n > r.nbits {
		r.e = errShort
		r.pos = r.nbits
		return
	}
	r.pos += n
}

// ue reads an unsigned Exp-Golomb code (spec 9.1).
func (r *bitReader) ue() uint32 {
	if r.e != nil {
		return 0
	}
	p := r.peek32()
	if p != 0 {
		lz := bits.LeadingZeros32(p)
		if lz < 16 {
			n := 2*lz + 1
			if r.pos+n > r.nbits {
				r.e = errShort
				return 0
			}
			r.pos += n
			return (p >> uint(32-n)) - 1
		}
	}
	// Long code: count zeros bit by bit.
	zeros := 0
	for {
		if r.pos >= r.nbits {
			r.e = errShort
			return 0
		}
		if r.u(1) == 1 {
			break
		}
		zeros++
		if zeros > 31 {
			r.e = errBadField
			return 0
		}
	}
	return (1<<uint(zeros) - 1) + r.u(zeros)
}

// se reads a signed Exp-Golomb code (spec 9.1.1).
func (r *bitReader) se() int32 {
	k := r.ue()
	if k&1 == 1 {
		return int32((k + 1) / 2)
	}
	return -int32(k / 2)
}

// te reads a truncated Exp-Golomb code with the given range (spec 9.1).
func (r *bitReader) te(max int) int {
	if max > 1 {
		return int(r.ue())
	}
	if r.u1() {
		return 0
	}
	return 1
}

func (r *bitReader) byteAligned() bool { return r.pos&7 == 0 }

func (r *bitReader) align() {
	if rem := r.pos & 7; rem != 0 {
		r.skip(8 - rem)
	}
}

// moreRBSPData reports whether data remains before rbsp_trailing_bits.
func (r *bitReader) moreRBSPData() bool {
	return r.lastOne > r.pos
}

// unescapeRBSP removes emulation_prevention_three_bytes (spec 7.4.1.1),
// appending into dst.
func unescapeRBSP(dst, data []byte) []byte {
	dst = dst[:0]
	zeros := 0
	start := 0
	for i := 0; i < len(data); i++ {
		b := data[i]
		if zeros >= 2 && b == 0x03 {
			dst = append(dst, data[start:i]...)
			start = i + 1
			zeros = 0
			continue
		}
		if b == 0 {
			zeros++
		} else {
			zeros = 0
		}
	}
	return append(dst, data[start:]...)
}

type errString string

func (e errString) Error() string { return string(e) }
func errf(s string) error         { return errString(s) }

var (
	errShort    = errf("h264: unexpected end of bitstream")
	errBadField = errf("h264: invalid bit field")
	// ErrUnsupported is returned for valid streams using features this
	// decoder does not implement (interlace, 4:2:2/4:4:4, high bit depth,
	// slice groups, data partitioning).
	ErrUnsupported = errf("h264: unsupported stream feature")
)
