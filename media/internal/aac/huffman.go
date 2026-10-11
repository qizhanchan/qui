package aac

// Two-level lookup tables for the AAC Huffman codebooks.

const lutBits = 9

// lut entry: len<<16 | index, or (for a subtable pointer) 0x8000_0000 |
// subtable offset<<8 | sub bits. len 0 = invalid.
type huffLUT struct {
	tab []uint32
}

type huffCode struct {
	length int
	code   int
	index  int
}

func buildHuff(codes []huffCode) huffLUT {
	l := huffLUT{tab: make([]uint32, 1<<lutBits)}
	// Group long codes by their first lutBits bits.
	long := map[int][]huffCode{}
	for _, c := range codes {
		if c.length <= lutBits {
			shift := uint(lutBits - c.length)
			base := c.code << shift
			for i := 0; i < 1<<shift; i++ {
				l.tab[base+i] = uint32(c.length<<16 | c.index)
			}
		} else {
			p := c.code >> uint(c.length-lutBits)
			long[p] = append(long[p], c)
		}
	}
	for p, cs := range long {
		maxLen := 0
		for _, c := range cs {
			maxLen = max(maxLen, c.length)
		}
		sub := maxLen - lutBits
		off := len(l.tab)
		l.tab = append(l.tab, make([]uint32, 1<<uint(sub))...)
		for _, c := range cs {
			rest := c.code & (1<<uint(c.length-lutBits) - 1)
			shift := uint(maxLen - c.length)
			base := rest << shift
			for i := 0; i < 1<<shift; i++ {
				l.tab[off+base+i] = uint32(c.length<<16 | c.index)
			}
		}
		l.tab[p] = 0x80000000 | uint32(off)<<8 | uint32(sub)
	}
	return l
}

// decode returns the codebook index of the next codeword, or -1.
func (l *huffLUT) decode(b *bits) int {
	p := b.peek32()
	e := l.tab[p>>(32-lutBits)]
	if e&0x80000000 != 0 {
		sub := int(e & 0xff)
		off := int(e>>8) & 0x7fffff
		e = l.tab[off+int((p<<lutBits)>>uint(32-sub))]
	}
	n := int(e >> 16)
	if n == 0 {
		return -1
	}
	b.skip(n)
	return int(e & 0xffff)
}

var (
	spectralLUT [12]huffLUT // index 1..11
	sfLUT       huffLUT
	// spectralVals[cb][i] are the (up to 4) values of entry i.
	spectralVals [12][][4]int8
	unsignedCB   = [12]bool{false, false, false, true, true, false, false, true, true, true, true, true}
	quadCB       = [12]bool{false, true, true, true, true}
	lavCB        = [12]int{0, 1, 1, 2, 2, 4, 4, 7, 7, 12, 12, 16}
)

func init() {
	quads := [][][6]int32{nil, cb1, cb2, cb3, cb4}
	pairs := [][][4]int32{nil, nil, nil, nil, nil, cb5, cb6, cb7, cb8, cb9, cb10, cb11}
	for cb := 1; cb <= 11; cb++ {
		var codes []huffCode
		if cb <= 4 {
			for i, e := range quads[cb] {
				codes = append(codes, huffCode{int(e[0]), int(e[1]), i})
				spectralVals[cb] = append(spectralVals[cb], [4]int8{int8(e[2]), int8(e[3]), int8(e[4]), int8(e[5])})
			}
		} else {
			for i, e := range pairs[cb] {
				codes = append(codes, huffCode{int(e[0]), int(e[1]), i})
				spectralVals[cb] = append(spectralVals[cb], [4]int8{int8(e[2]), int8(e[3])})
			}
		}
		spectralLUT[cb] = buildHuff(codes)
	}
	var codes []huffCode
	for _, e := range cbSF {
		codes = append(codes, huffCode{int(e[0]), int(e[1]), int(e[2])})
	}
	sfLUT = buildHuff(codes)
}
