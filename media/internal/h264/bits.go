// Package h264 is qui's self-written pure-Go H.264/AVC decoder.
//
// It is built bottom-up and verified incrementally against a reference
// decoder (Cisco OpenH264) using the harness in tools/media-poc.
//
// Milestones (see docs/media-foundation.md):
//
//	H0 bitstream   NAL parsing, RBSP, exp-Golomb, SPS/PPS        <- this file set
//	H1 intra       I_PCM, I16x16, I4x4, chroma, transform, deblock
//	H2..           P/B slices, CABAC, High profile
//
// Nothing here depends on cgo, root qui, or any third-party module.
package h264

// bitReader reads big-endian bits from an RBSP byte slice. The first
// error is latched; all subsequent reads return 0 and err() reports it.
type bitReader struct {
	data []byte
	pos  int // bit offset from the start
	e    error
}

func newBitReader(data []byte) *bitReader { return &bitReader{data: data} }

func (r *bitReader) err() error { return r.e }

// bitsLeft reports how many bits remain.
func (r *bitReader) bitsLeft() int {
	return len(r.data)*8 - r.pos
}

// u reads n (0..32) bits MSB-first and returns them right-aligned.
func (r *bitReader) u(n int) uint32 {
	if r.e != nil || n == 0 {
		return 0
	}
	if n < 0 || n > 32 {
		r.e = errBadField
		return 0
	}
	if r.bitsLeft() < n {
		r.e = errShort
		return 0
	}
	var v uint32
	for i := 0; i < n; i++ {
		byteIdx := r.pos >> 3
		bit := 7 - uint(r.pos&7)
		v = v<<1 | uint32((r.data[byteIdx]>>bit)&1)
		r.pos++
	}
	return v
}

// u1 reads one bit as a bool.
func (r *bitReader) u1() bool { return r.u(1) == 1 }

// ue reads an unsigned Exp-Golomb code (spec 9.1).
func (r *bitReader) ue() uint32 {
	if r.e != nil {
		return 0
	}
	zeros := 0
	for {
		if r.bitsLeft() < 1 {
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
	if zeros == 0 {
		return 0
	}
	suffix := r.u(zeros)
	return (1 << uint(zeros)) - 1 + suffix
}

// se reads a signed Exp-Golomb code (spec 9.1.1).
func (r *bitReader) se() int32 {
	k := r.ue()
	if k&1 == 1 {
		return int32((k + 1) / 2)
	}
	return -int32(k / 2)
}

// skip discards n bits.
func (r *bitReader) skip(n int) { _ = r.u(n) }

// peekBits returns the next n bits without consuming them. Missing bits
// past the end read as zero, matching the zero-padding a conforming
// bitstream carries in its rbsp_trailing_bits.
func (r *bitReader) peekBits(n int) uint32 {
	if n <= 0 {
		return 0
	}
	if n > 32 {
		n = 32
	}
	var v uint32
	for i := 0; i < n; i++ {
		p := r.pos + i
		if p >= len(r.data)*8 {
			v <<= 1
			continue
		}
		v = v<<1 | uint32((r.data[p>>3]>>(7-uint(p&7)))&1)
	}
	return v
}

// readBit consumes one bit and returns it (0 or 1).
func (r *bitReader) readBit() uint32 { return r.u(1) }

// readBytes aligns to a byte boundary and reads n whole bytes (for I_PCM).
func (r *bitReader) readBytes(n int) []byte {
	if r.e != nil {
		return nil
	}
	r.align()
	out := make([]byte, n)
	for i := 0; i < n; i++ {
		out[i] = byte(r.u(8))
	}
	if r.e != nil {
		return nil
	}
	return out
}

// align advances to the next byte boundary.
func (r *bitReader) align() {
	if rem := r.pos & 7; rem != 0 {
		r.skip(8 - rem)
	}
}

// moreRBSPData reports whether there is more than just rbsp_trailing_bits
// left. The trailing bits are a single 1 bit followed by zeros, so there is
// more data iff the current position is before the last set bit in the RBSP.
func (r *bitReader) moreRBSPData() bool {
	last := -1
	for i := len(r.data)*8 - 1; i >= 0; i-- {
		if (r.data[i>>3]>>(7-uint(i&7)))&1 == 1 {
			last = i
			break
		}
	}
	return last >= 0 && r.pos < last
}

func (r *bitReader) peekBit(i int) uint32 {
	p := r.pos + i
	if p >= len(r.data)*8 {
		return 0
	}
	return uint32((r.data[p>>3] >> (7 - uint(p&7))) & 1)
}

// unescapeRBSP removes emulation_prevention_three_byte (0x03) that follows
// two zero bytes, converting an EBSP to an RBSP (spec 7.4.1.1).
func unescapeRBSP(data []byte) []byte {
	out := make([]byte, 0, len(data))
	zeros := 0
	for _, b := range data {
		if zeros == 2 && b == 0x03 {
			zeros = 0
			continue
		}
		out = append(out, b)
		if b == 0 {
			zeros++
		} else {
			zeros = 0
		}
	}
	return out
}

var (
	errShort    = errf("h264: unexpected end of bitstream")
	errBadField = errf("h264: invalid bit field")
)

type errString string

func (e errString) Error() string { return string(e) }
func errf(s string) error         { return errString(s) }
