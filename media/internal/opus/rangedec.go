package opus

import "math/bits"

// Range decoder (RFC 6716 section 4.1), a port of celt/entdec.c.

const (
	ecSymBits   = 8
	ecCodeBits  = 32
	ecSymMax    = (1 << ecSymBits) - 1
	ecCodeTop   = uint32(1) << (ecCodeBits - 1)
	ecCodeBot   = ecCodeTop >> ecSymBits
	ecCodeExtra = (ecCodeBits-2)%ecSymBits + 1
	ecUintBits  = 8
	ecWindow    = 32
	bitRes      = 3
)

type rangeDecoder struct {
	buf        []byte
	storage    uint32
	endOffs    uint32
	endWindow  uint32
	nendBits   int
	nbitsTotal int
	offs       uint32
	rng        uint32
	val        uint32
	ext        uint32
	rem        int
	err        bool
}

func ilog(x uint32) int { return bits.Len32(x) }

func (d *rangeDecoder) readByte() int {
	if d.offs < d.storage {
		b := d.buf[d.offs]
		d.offs++
		return int(b)
	}
	return 0
}

func (d *rangeDecoder) readByteFromEnd() int {
	if d.endOffs < d.storage {
		d.endOffs++
		return int(d.buf[d.storage-d.endOffs])
	}
	return 0
}

func (d *rangeDecoder) normalize() {
	for d.rng <= ecCodeBot {
		d.nbitsTotal += ecSymBits
		d.rng <<= ecSymBits
		sym := d.rem
		d.rem = d.readByte()
		sym = (sym<<ecSymBits | d.rem) >> (ecSymBits - ecCodeExtra)
		d.val = ((d.val << ecSymBits) + uint32(ecSymMax&^sym)) & (ecCodeTop - 1)
	}
}

func (d *rangeDecoder) init(buf []byte) {
	d.buf = buf
	d.storage = uint32(len(buf))
	d.endOffs = 0
	d.endWindow = 0
	d.nendBits = 0
	d.nbitsTotal = ecCodeBits + 1 - ((ecCodeBits-ecCodeExtra)/ecSymBits)*ecSymBits
	d.offs = 0
	d.rng = 1 << ecCodeExtra
	d.rem = d.readByte()
	d.val = d.rng - 1 - uint32(d.rem>>(ecSymBits-ecCodeExtra))
	d.err = false
	d.normalize()
}

func (d *rangeDecoder) decode(ft uint32) uint32 {
	d.ext = d.rng / ft
	s := d.val / d.ext
	if s+1 < ft {
		return ft - (s + 1)
	}
	return 0
}

func (d *rangeDecoder) decodeBin(b uint) uint32 {
	d.ext = d.rng >> b
	s := d.val / d.ext
	ft := uint32(1) << b
	if s+1 < ft {
		return ft - (s + 1)
	}
	return 0
}

func (d *rangeDecoder) update(fl, fh, ft uint32) {
	s := d.ext * (ft - fh)
	d.val -= s
	if fl > 0 {
		d.rng = d.ext * (fh - fl)
	} else {
		d.rng -= s
	}
	d.normalize()
}

func (d *rangeDecoder) bitLogp(logp uint) int {
	r := d.rng
	v := d.val
	s := r >> logp
	ret := 0
	if v < s {
		ret = 1
		d.rng = s
	} else {
		d.val = v - s
		d.rng = r - s
	}
	d.normalize()
	return ret
}

func (d *rangeDecoder) icdf(icdf []uint8, ftb uint) int {
	s := d.rng
	v := d.val
	r := s >> ftb
	ret := -1
	var t uint32
	for {
		t = s
		ret++
		if ret >= len(icdf) { // malformed table use; cannot happen with valid tables
			ret = len(icdf) - 1
			s = 0
			break
		}
		s = r * uint32(icdf[ret])
		if v >= s {
			break
		}
	}
	d.val = v - s
	d.rng = t - s
	d.normalize()
	return ret
}

func (d *rangeDecoder) uint(ft uint32) uint32 {
	if ft <= 1 {
		return 0
	}
	ft--
	ftb := ilog(ft)
	if ftb > ecUintBits {
		ftb -= ecUintBits
		f := (ft >> uint(ftb)) + 1
		s := d.decode(f)
		d.update(s, s+1, f)
		t := s<<uint(ftb) | d.bits(uint(ftb))
		if t <= ft {
			return t
		}
		d.err = true
		return ft
	}
	ft++
	s := d.decode(ft)
	d.update(s, s+1, ft)
	return s
}

func (d *rangeDecoder) bits(n uint) uint32 {
	window := d.endWindow
	available := d.nendBits
	if available < int(n) {
		for {
			window |= uint32(d.readByteFromEnd()) << uint(available)
			available += ecSymBits
			if available > ecWindow-ecSymBits {
				break
			}
		}
	}
	ret := window & ((uint32(1) << n) - 1)
	window >>= n
	available -= int(n)
	d.endWindow = window
	d.nendBits = available
	d.nbitsTotal += int(n)
	return ret
}

func (d *rangeDecoder) tell() int { return d.nbitsTotal - ilog(d.rng) }

func (d *rangeDecoder) tellFrac() int {
	nbits := d.nbitsTotal << bitRes
	l := ilog(d.rng)
	r := d.rng >> uint(l-16)
	for i := bitRes; i > 0; i-- {
		r = r * r >> 15
		b := int(r >> 16)
		l = l<<1 | b
		r >>= uint(b)
	}
	return nbits - l
}

// laplace decodes a Laplace-distributed value (celt/laplace.c).
func (d *rangeDecoder) laplace(fs uint32, decay int) int {
	const minP = 1
	const logMinP = 0
	const nminP = 16
	val := 0
	fm := d.decodeBin(15)
	fl := uint32(0)
	if fm >= fs {
		val++
		fl = fs
		// ec_laplace_get_freq1
		ft := uint32(32768) - minP*(2*nminP) - fs
		fs = uint32((int64(ft)*int64(16384-decay))>>15) + minP
		for fs > minP && fm >= fl+2*fs {
			fs *= 2
			fl += fs
			fs = uint32((int64(fs-2*minP) * int64(decay)) >> 15)
			fs += minP
			val++
		}
		if fs <= minP {
			di := (fm - fl) >> (logMinP + 1)
			val += int(di)
			fl += 2 * di * minP
		}
		if fm < fl+fs {
			val = -val
		} else {
			fl += fs
		}
	}
	fh := fl + fs
	if fh > 32768 {
		fh = 32768
	}
	if fl >= fh { // corrupt stream guard
		fl = fh - 1
	}
	d.update(fl, fh, 32768)
	return val
}
