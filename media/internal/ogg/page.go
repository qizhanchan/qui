package ogg

import (
	"encoding/binary"
	"io"
)

// Ogg pages (RFC 3533).

const (
	headerLen   = 27
	maxPageSize = headerLen + 255 + 255*255

	flagContinued = 0x01
	flagBOS       = 0x02
	flagEOS       = 0x04
)

var crcTable = func() (t [256]uint32) {
	for i := range t {
		r := uint32(i) << 24
		for j := 0; j < 8; j++ {
			if r&0x80000000 != 0 {
				r = r<<1 ^ 0x04C11DB7
			} else {
				r <<= 1
			}
		}
		t[i] = r
	}
	return t
}()

func crcUpdate(crc uint32, b []byte) uint32 {
	for _, c := range b {
		crc = crc<<8 ^ crcTable[byte(crc>>24)^c]
	}
	return crc
}

// page is one parsed, CRC-checked page.
type page struct {
	off     int64
	next    int64 // offset just past the page
	flags   byte
	granule int64 // -1: no packet ends on this page
	serial  uint32
	seq     uint32
	lacing  []byte
	body    []byte
}

// source reads the input through a one-window cache.
type source struct {
	ra     io.ReaderAt
	size   int64
	win    []byte
	winOff int64
	winLen int
	err    error // first non-EOF ReaderAt error
}

const winSize = 128 << 10

// bytes returns up to n (≤ winSize) bytes at off; the slice is valid
// until the next call.
func (s *source) bytes(off int64, n int) []byte {
	if off < 0 || off >= s.size || n <= 0 {
		return nil
	}
	if rem := s.size - off; int64(n) > rem {
		n = int(rem)
	}
	if off >= s.winOff && off+int64(n) <= s.winOff+int64(s.winLen) {
		i := off - s.winOff
		return s.win[i : i+int64(n)]
	}
	if s.win == nil {
		s.win = make([]byte, winSize)
	}
	want := int64(winSize)
	if rem := s.size - off; want > rem {
		want = rem
	}
	m, err := s.ra.ReadAt(s.win[:want], off)
	if err != nil && err != io.EOF && s.err == nil {
		s.err = err
	}
	s.winOff, s.winLen = off, m
	return s.win[:min(n, m)]
}

// pageAt parses the page that starts exactly at off. The returned page
// owns copies of its lacing table and body.
func (s *source) pageAt(off int64) (page, bool) {
	h := s.bytes(off, headerLen+255)
	if len(h) < headerLen || h[0] != 'O' || h[1] != 'g' || h[2] != 'g' || h[3] != 'S' || h[4] != 0 {
		return page{}, false
	}
	nseg := int(h[26])
	if len(h) < headerLen+nseg {
		return page{}, false
	}
	bodyLen := 0
	for _, l := range h[headerLen : headerLen+nseg] {
		bodyLen += int(l)
	}
	total := headerLen + nseg + bodyLen
	b := s.bytes(off, total)
	if len(b) < total {
		return page{}, false
	}
	want := binary.LittleEndian.Uint32(b[22:26])
	crc := crcUpdate(0, b[:22])
	crc = crcUpdate(crc, []byte{0, 0, 0, 0})
	crc = crcUpdate(crc, b[26:])
	if crc != want {
		return page{}, false
	}
	p := page{
		off:     off,
		next:    off + int64(total),
		flags:   b[5],
		granule: int64(binary.LittleEndian.Uint64(b[6:14])),
		serial:  binary.LittleEndian.Uint32(b[14:18]),
		seq:     binary.LittleEndian.Uint32(b[18:22]),
	}
	buf := append([]byte(nil), b[headerLen:]...)
	p.lacing, p.body = buf[:nseg:nseg], buf[nseg:]
	return p, true
}

// nextPage returns the first valid page starting at or after off and
// before limit, resynchronising on the capture pattern past damage.
func (s *source) nextPage(off, limit int64) (page, bool) {
	limit = min(limit, s.size)
	for off+headerLen <= limit {
		if p, ok := s.pageAt(off); ok {
			return p, true
		}
		// Scan for the next "OggS".
		scan, found := off+1, false
		for scan+headerLen <= limit && !found {
			n := int(min(int64(winSize/2), limit-scan))
			b := s.bytes(scan, n)
			if len(b) < 4 {
				return page{}, false
			}
			for i := 0; i+4 <= len(b); i++ {
				if b[i] == 'O' && b[i+1] == 'g' && b[i+2] == 'g' && b[i+3] == 'S' {
					off, found = scan+int64(i), true
					break
				}
			}
			if !found {
				scan += int64(len(b) - 3)
			}
		}
		if !found {
			return page{}, false
		}
	}
	return page{}, false
}
