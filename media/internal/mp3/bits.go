package mp3

// bitReader reads MSB-first bit fields. Reads past the end yield zero bits
// and are detectable via pos > n, so malformed input can never index out of
// range.
type bitReader struct {
	data []byte
	pos  int // bit position
	n    int // total bits
}

func (b *bitReader) init(data []byte) {
	b.data = data
	b.pos = 0
	b.n = len(data) * 8
}

func (b *bitReader) bit() int {
	p := b.pos
	b.pos++
	if p >= b.n {
		return 0
	}
	return int(b.data[p>>3]>>(7-uint(p&7))) & 1
}

// read returns the next n (0..32) bits.
func (b *bitReader) read(n int) int {
	v := 0
	for n > 0 {
		p := b.pos
		if p >= b.n {
			b.pos += n
			return v << uint(n)
		}
		avail := 8 - p&7
		take := avail
		if take > n {
			take = n
		}
		x := int(b.data[p>>3]) >> uint(avail-take) & (1<<uint(take) - 1)
		v = v<<uint(take) | x
		b.pos += take
		n -= take
	}
	return v
}

func (b *bitReader) overrun() bool { return b.pos > b.n }
