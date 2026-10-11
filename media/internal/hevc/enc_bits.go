package hevc

// Bit writing and NAL packaging for the encoder.

// bitWriter writes an RBSP most-significant bit first.
type bitWriter struct {
	buf  []byte
	acc  uint64 // pending bits, right-aligned
	nacc int
}

func (w *bitWriter) reset() { w.buf, w.acc, w.nacc = w.buf[:0], 0, 0 }

// u writes the low n bits of v (n <= 32).
func (w *bitWriter) u(n int, v uint32) {
	if n == 0 {
		return
	}
	w.acc = w.acc<<uint(n) | uint64(v)&(1<<uint(n)-1)
	w.nacc += n
	for w.nacc >= 8 {
		w.nacc -= 8
		w.buf = append(w.buf, byte(w.acc>>uint(w.nacc)))
	}
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
	n := 0
	for t := x; t > 1; t >>= 1 {
		n++
	}
	w.u(n, 0)
	if n+1 > 32 {
		w.u(n+1-32, uint32(x>>32))
		w.u(32, uint32(x))
		return
	}
	w.u(n+1, uint32(x))
}

// se writes a signed Exp-Golomb code.
func (w *bitWriter) se(v int) {
	if v > 0 {
		w.ue(uint32(2*v - 1))
	} else {
		w.ue(uint32(-2 * v))
	}
}

// bits is the number of bits written.
func (w *bitWriter) bits() int { return len(w.buf)*8 + w.nacc }

// aligned reports byte alignment.
func (w *bitWriter) aligned() bool { return w.nacc == 0 }

// alignZero pads with zero bits to a byte boundary.
func (w *bitWriter) alignZero() {
	if w.nacc > 0 {
		w.u(8-w.nacc, 0)
	}
}

// trailing writes rbsp_trailing_bits (a one, then zeros to alignment).
func (w *bitWriter) trailing() {
	w.u(1, 1)
	w.alignZero()
}

// escape appends src to dst with emulation prevention bytes.
func escape(dst, src []byte) []byte {
	zeros := 0
	for _, b := range src {
		if zeros >= 2 && b <= 3 {
			dst = append(dst, 3)
			zeros = 0
		}
		dst = append(dst, b)
		if b == 0 {
			zeros++
		} else {
			zeros = 0
		}
	}
	return dst
}

// nalUnit builds a NAL unit (2-byte header, escaped payload) of type typ
// and temporal id 0.
func nalUnit(typ int, rbsp []byte) []byte {
	out := make([]byte, 2, 2+len(rbsp)+len(rbsp)/64+4)
	out[0] = byte(typ << 1)
	out[1] = 1 // nuh_temporal_id_plus1
	return escape(out, rbsp)
}

// AnnexB joins NAL units with 4-byte start codes.
func AnnexB(nals [][]byte) []byte {
	n := 0
	for _, u := range nals {
		n += 4 + len(u)
	}
	out := make([]byte, 0, n)
	for _, u := range nals {
		out = append(out, 0, 0, 0, 1)
		out = append(out, u...)
	}
	return out
}

// LengthPrefixed joins NAL units with 4-byte big-endian lengths (MP4
// samples).
func LengthPrefixed(nals [][]byte) []byte {
	n := 0
	for _, u := range nals {
		n += 4 + len(u)
	}
	out := make([]byte, 0, n)
	for _, u := range nals {
		l := len(u)
		out = append(out, byte(l>>24), byte(l>>16), byte(l>>8), byte(l))
		out = append(out, u...)
	}
	return out
}
