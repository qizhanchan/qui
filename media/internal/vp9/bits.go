package vp9

// bitReader reads the uncompressed header MSB first. Reads past the end
// return zero bits and set err.
type bitReader struct {
	buf []byte
	pos int // in bits
	err bool
}

func (b *bitReader) bit() int {
	i := b.pos >> 3
	if i >= len(b.buf) {
		b.err = true
		b.pos++
		return 0
	}
	v := int(b.buf[i]>>(7-uint(b.pos&7))) & 1
	b.pos++
	return v
}

func (b *bitReader) f(n int) int {
	v := 0
	for i := 0; i < n; i++ {
		v = v<<1 | b.bit()
	}
	return v
}

// su reads an n-bit magnitude followed by a sign bit.
func (b *bitReader) su(n int) int {
	v := b.f(n)
	if b.bit() != 0 {
		return -v
	}
	return v
}

func (b *bitReader) bytePos() int { return (b.pos + 7) >> 3 }

// boolDecoder is the VP9 arithmetic decoder (libvpx vpx_reader).
type boolDecoder struct {
	buf   []byte
	pos   int
	value uint64 // the next bits, MSB aligned
	bits  int    // valid bits in value
	rng   uint32
}

func (d *boolDecoder) init(buf []byte) bool {
	d.buf, d.pos = buf, 0
	d.value, d.bits = 0, 0
	d.rng = 255
	if len(buf) == 0 {
		return false
	}
	d.fill()
	return d.readBit() == 0 // marker bit
}

func (d *boolDecoder) fill() {
	for d.bits <= 56 {
		if d.pos >= len(d.buf) {
			// Past the end: zeros (libvpx pads the same way).
			d.bits = 64
			return
		}
		d.value |= uint64(d.buf[d.pos]) << uint(56-d.bits)
		d.bits += 8
		d.pos++
	}
}

func (d *boolDecoder) read(prob uint8) int {
	split := 1 + (((d.rng - 1) * uint32(prob)) >> 8)
	if d.bits < 8 {
		d.fill()
	}
	big := uint64(split) << 56
	var bit int
	if d.value >= big {
		d.rng -= split
		d.value -= big
		bit = 1
	} else {
		d.rng = split
	}
	shift := vpxNorm[d.rng]
	d.rng <<= shift
	d.value <<= shift
	d.bits -= int(shift)
	return bit
}

func (d *boolDecoder) readBit() int { return d.read(128) }

func (d *boolDecoder) literal(n int) int {
	v := 0
	for i := 0; i < n; i++ {
		v = v<<1 | d.readBit()
	}
	return v
}

// tree decodes a libvpx-style tree (negative entries are leaves).
func (d *boolDecoder) tree(t []int8, probs []uint8) int {
	i := 0
	for {
		i = int(t[i+d.read(probs[i>>1])])
		if i <= 0 {
			return -i
		}
	}
}
