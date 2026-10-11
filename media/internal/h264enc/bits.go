package h264enc

import "math/bits"

// bitWriter appends big-endian bit fields to an RBSP.
type bitWriter struct {
	buf []byte
	acc uint64 // pending bits, the low n of them valid
	n   uint
}

func (w *bitWriter) reset() { w.buf, w.acc, w.n = w.buf[:0], 0, 0 }

// u writes the low n bits of v (n <= 32).
func (w *bitWriter) u(n int, v uint32) {
	if n == 0 {
		return
	}
	w.acc = w.acc<<uint(n) | uint64(v)&(1<<uint(n)-1)
	w.n += uint(n)
	for w.n >= 8 {
		w.n -= 8
		w.buf = append(w.buf, byte(w.acc>>w.n))
	}
	w.acc &= 1<<w.n - 1
}

func (w *bitWriter) flag(b bool) {
	if b {
		w.u(1, 1)
	} else {
		w.u(1, 0)
	}
}

// ue writes an unsigned Exp-Golomb code.
func (w *bitWriter) ue(v uint32) {
	x := uint64(v) + 1
	l := bits.Len64(x)
	if 2*l-1 <= 32 {
		w.u(2*l-1, uint32(x))
		return
	}
	w.u(l-1, 0)
	w.u(l, uint32(x))
}

// se writes a signed Exp-Golomb code.
func (w *bitWriter) se(v int32) {
	if v > 0 {
		w.ue(uint32(2*v - 1))
	} else {
		w.ue(uint32(-2 * v))
	}
}

// te writes a truncated Exp-Golomb code with range max.
func (w *bitWriter) te(max int, v uint32) {
	if max > 1 {
		w.ue(v)
	} else if max == 1 {
		w.u(1, ^v&1)
	}
}

// bitLen is the number of bits written so far.
func (w *bitWriter) bitLen() int { return len(w.buf)*8 + int(w.n) }

// aligned reports byte alignment.
func (w *bitWriter) aligned() bool { return w.n == 0 }

// trailing writes rbsp_trailing_bits.
func (w *bitWriter) trailing() {
	w.u(1, 1)
	w.align(0)
}

// align pads with bit b up to the next byte boundary.
func (w *bitWriter) align(b uint32) {
	for w.n != 0 {
		w.u(1, b)
	}
}

// ueBits is the length of the ue(v) code of v.
func ueBits(v uint32) int { return 2*bits.Len64(uint64(v)+1) - 1 }

// seBits is the length of the se(v) code of v.
func seBits(v int32) int {
	if v > 0 {
		return ueBits(uint32(2*v - 1))
	}
	return ueBits(uint32(-2 * v))
}

// nalUnit wraps an RBSP into a NAL unit (header byte + emulation-prevented
// payload).
func nalUnit(refIdc, typ int, rbsp []byte) []byte {
	out := make([]byte, 0, len(rbsp)+len(rbsp)/64+2)
	out = append(out, byte(refIdc<<5|typ))
	zeros := 0
	for _, b := range rbsp {
		if zeros >= 2 && b <= 3 {
			out = append(out, 3)
			zeros = 0
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

// AnnexB joins NAL units with 4-byte start codes.
func AnnexB(nals [][]byte) []byte {
	n := 0
	for _, x := range nals {
		n += len(x) + 4
	}
	out := make([]byte, 0, n)
	for _, x := range nals {
		out = append(out, 0, 0, 0, 1)
		out = append(out, x...)
	}
	return out
}

// LengthPrefixed joins NAL units with 4-byte big-endian lengths (MP4
// samples).
func LengthPrefixed(nals [][]byte) []byte {
	n := 0
	for _, x := range nals {
		n += len(x) + 4
	}
	out := make([]byte, 0, n)
	for _, x := range nals {
		l := len(x)
		out = append(out, byte(l>>24), byte(l>>16), byte(l>>8), byte(l))
		out = append(out, x...)
	}
	return out
}
