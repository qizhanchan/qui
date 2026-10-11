package aac

import "encoding/binary"

// bits is a fast MSB-first reader over one access unit. Reads past the end
// return zeros and set over.
type bits struct {
	data []byte
	pos  int
	n    int
	over bool
}

func (b *bits) init(data []byte) {
	n := len(data)
	if cap(b.data) < n+8 {
		b.data = make([]byte, n+8, n+64)
	}
	b.data = b.data[:n+8]
	copy(b.data, data)
	clear(b.data[n:])
	b.pos, b.n, b.over = 0, n*8, false
}

func (b *bits) peek32() uint32 {
	i := b.pos >> 3
	if i+8 > len(b.data) {
		return 0
	}
	v := binary.BigEndian.Uint64(b.data[i:])
	return uint32((v << uint(b.pos&7)) >> 32)
}

func (b *bits) read(n int) int {
	if n == 0 {
		return 0
	}
	v := int(b.peek32() >> uint(32-n))
	b.pos += n
	if b.pos > b.n {
		b.over = true
		b.pos = b.n
	}
	return v
}

func (b *bits) bit() bool { return b.read(1) == 1 }

func (b *bits) skip(n int) {
	b.pos += n
	if b.pos > b.n {
		b.over = true
		b.pos = b.n
	}
}

func (b *bits) align() {
	if r := b.pos & 7; r != 0 {
		b.skip(8 - r)
	}
}

func (b *bits) left() int { return b.n - b.pos }
