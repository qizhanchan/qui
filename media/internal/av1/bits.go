package av1

import "errors"

// ErrUnsupported reports a stream feature this decoder does not implement.
var ErrUnsupported = errors.New("av1: unsupported stream feature")

// ErrInvalid reports a malformed bitstream.
var ErrInvalid = errors.New("av1: invalid bitstream")

// bitReader reads the fixed-width, MSB-first syntax of the AV1 headers
// (spec 4.10). Reading past the end yields zero bits; callers check
// overrun() where it matters.
type bitReader struct {
	data []byte
	pos  int // bit position

	// trace, when set, receives every syntax element: its first bit
	// position and decoded value (tests compare it with FFmpeg's
	// trace_headers).
	trace func(pos, val int)
	base  int // added to traced positions
}

func (r *bitReader) bit() int {
	i := r.pos >> 3
	r.pos++
	if i >= len(r.data) {
		return 0
	}
	return int(r.data[i]>>(7-uint(r.pos-1)&7)) & 1
}

func (r *bitReader) bits(n int) int {
	v := 0
	for ; n > 0; n-- {
		v = v<<1 | r.bit()
	}
	return v
}

func (r *bitReader) emit(start, v int) int {
	if r.trace != nil {
		r.trace(r.base+start, v)
	}
	return v
}

// f reads an n-bit unsigned value, f(n).
func (r *bitReader) f(n int) int {
	start := r.pos
	return r.emit(start, r.bits(n))
}

// flag reads one bit as a bool.
func (r *bitReader) flag() bool { return r.f(1) != 0 }

// su reads an n-bit two's-complement value, su(n).
func (r *bitReader) su(n int) int {
	start := r.pos
	v := r.bits(n)
	if v&(1<<(n-1)) != 0 {
		v -= 1 << n
	}
	return r.emit(start, v)
}

// ns reads a non-symmetric unsigned value below n, ns(n).
func (r *bitReader) ns(n int) int {
	start := r.pos
	w := 0
	for x := n; x != 0; x >>= 1 {
		w++
	}
	m := (1 << w) - n
	v := r.bits(w - 1)
	if v >= m {
		v = v<<1 - m + r.bit()
	}
	return r.emit(start, v)
}

// le reads an n-byte little-endian value, le(n).
func (r *bitReader) le(n int) int {
	start := r.pos
	t := 0
	for i := 0; i < n; i++ {
		t += r.bits(8) << (8 * i)
	}
	return r.emit(start, t)
}

// uvlc reads uvlc().
func (r *bitReader) uvlc() int {
	start := r.pos
	lz := 0
	for r.bit() == 0 {
		lz++
		if lz >= 32 {
			return r.emit(start, (1<<32)-1)
		}
	}
	return r.emit(start, r.bits(lz)+(1<<lz)-1)
}

// byteAlign skips to the next byte boundary.
func (r *bitReader) byteAlign() { r.pos = (r.pos + 7) &^ 7 }

func (r *bitReader) overrun() bool { return r.pos > len(r.data)*8 }

// leb128 decodes an unsigned LEB128 value from b, returning the value and
// its length in bytes (0 on error).
func leb128(b []byte) (uint64, int) {
	var v uint64
	for i := 0; i < 8 && i < len(b); i++ {
		v |= uint64(b[i]&0x7f) << (7 * i)
		if b[i]&0x80 == 0 {
			return v, i + 1
		}
	}
	return 0, 0
}
