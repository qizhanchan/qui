// Package hevc is a pure-Go H.265/HEVC decoder (Main profile: 8-bit
// 4:2:0, progressive), written from ITU-T H.265 and verified plane-exact
// against FFmpeg.
package hevc

import (
	"errors"
	"math/bits"
)

var (
	errShort       = errors.New("hevc: truncated data")
	errUnsupported = errors.New("hevc: unsupported stream")
	errCorrupt     = errors.New("hevc: corrupt data")
)

// ErrUnsupported reports a stream outside the decoder's profile.
var ErrUnsupported = errUnsupported

// bitReader reads an RBSP (emulation prevention already removed).
type bitReader struct {
	data  []byte
	pos   int // bit position
	nbits int
	e     error
}

func (r *bitReader) reset(rbsp []byte) {
	r.data, r.pos, r.nbits, r.e = rbsp, 0, len(rbsp)*8, nil
}

func (r *bitReader) u(n int) uint32 {
	if n == 0 {
		return 0
	}
	if r.pos+n > r.nbits {
		r.e = errShort
		r.pos = r.nbits
		return 0
	}
	var v uint64
	p := r.pos >> 3
	for i := 0; i < 8 && p+i < len(r.data); i++ {
		v |= uint64(r.data[p+i]) << uint(56-8*i)
	}
	v <<= uint(r.pos & 7)
	r.pos += n
	return uint32(v >> uint(64-n))
}

func (r *bitReader) flag() bool { return r.u(1) == 1 }

func (r *bitReader) ue() uint32 {
	zeros := 0
	for r.pos < r.nbits {
		p := r.pos >> 3
		b := r.data[p] << uint(r.pos&7)
		if b != 0 {
			lz := bits.LeadingZeros8(b)
			zeros += lz
			r.pos += lz + 1
			break
		}
		n := 8 - r.pos&7
		zeros += n
		r.pos += n
		if zeros > 32 {
			r.e = errCorrupt
			return 0
		}
	}
	if r.pos > r.nbits {
		r.e = errShort
		return 0
	}
	if zeros == 0 {
		return 0
	}
	if zeros > 31 {
		r.e = errCorrupt
		return 0
	}
	return (1<<uint(zeros) - 1) + r.u(zeros)
}

func (r *bitReader) se() int32 {
	v := r.ue()
	if v&1 == 1 {
		return int32((v + 1) >> 1)
	}
	return -int32(v >> 1)
}

func (r *bitReader) skip(n int) { r.pos += n }

func (r *bitReader) align() { r.pos = (r.pos + 7) &^ 7 }

func (r *bitReader) bitsLeft() int { return r.nbits - r.pos }

// moreRBSPData reports whether payload remains before the stop bit.
func (r *bitReader) moreRBSPData() bool {
	if r.pos >= r.nbits {
		return false
	}
	last := len(r.data) - 1
	for last >= 0 && r.data[last] == 0 {
		last--
	}
	if last < 0 {
		return false
	}
	stop := last*8 + 7 - bits.TrailingZeros8(r.data[last])
	return r.pos < stop
}

// unescape removes emulation prevention bytes.
func unescape(dst, src []byte) []byte {
	dst = dst[:0]
	zeros := 0
	for i := 0; i < len(src); i++ {
		b := src[i]
		if zeros >= 2 && b == 3 {
			zeros = 0
			continue
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

func ceilLog2(v int) int {
	if v <= 1 {
		return 0
	}
	return bits.Len(uint(v - 1))
}

func clip3(lo, hi, v int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

func clipU8(v int) uint8 {
	if uint(v) > 255 {
		if v < 0 {
			return 0
		}
		return 255
	}
	return uint8(v)
}

func iabs(v int) int {
	if v < 0 {
		return -v
	}
	return v
}

// NAL unit types.
const (
	nalTrailN   = 0
	nalRASLN    = 8
	nalRASLR    = 9
	nalBLAWLP   = 16
	nalBLAWRADL = 17
	nalBLANLP   = 18
	nalIDRWRADL = 19
	nalIDRNLP   = 20
	nalCRA      = 21
	nalVPS      = 32
	nalSPS      = 33
	nalPPS      = 34
	nalAUD      = 35
	nalEOS      = 36
	nalEOB      = 37
)

// SplitAnnexB splits an Annex-B byte stream into NAL units.
func SplitAnnexB(data []byte) [][]byte {
	var out [][]byte
	start := -1
	i := 0
	for i+2 < len(data) {
		if data[i] == 0 && data[i+1] == 0 && data[i+2] == 1 {
			if start >= 0 {
				end := i
				for end > start && data[end-1] == 0 {
					end--
				}
				if end > start {
					out = append(out, data[start:end])
				}
			}
			i += 3
			start = i
			continue
		}
		i++
	}
	if start >= 0 && start < len(data) {
		out = append(out, data[start:])
	}
	return out
}

// SplitLengthPrefixed splits an MP4 sample into NAL units.
func SplitLengthPrefixed(data []byte, lengthSize int) ([][]byte, error) {
	var out [][]byte
	for len(data) > 0 {
		if len(data) < lengthSize {
			return out, errShort
		}
		n := 0
		for i := 0; i < lengthSize; i++ {
			n = n<<8 | int(data[i])
		}
		data = data[lengthSize:]
		if n > len(data) {
			return out, errShort
		}
		out = append(out, data[:n])
		data = data[n:]
	}
	return out, nil
}

// DecoderConfig is a parsed HEVCDecoderConfigurationRecord (hvcC).
type DecoderConfig struct {
	LengthSize int
	NALs       [][]byte // VPS, SPS, PPS (and SEI) arrays in order
}

// ParseDecoderConfig parses an hvcC record.
func ParseDecoderConfig(b []byte) (*DecoderConfig, error) {
	if len(b) < 23 {
		return nil, errShort
	}
	c := &DecoderConfig{LengthSize: int(b[21]&3) + 1}
	n := int(b[22])
	p := 23
	for i := 0; i < n; i++ {
		if p+3 > len(b) {
			return nil, errShort
		}
		cnt := int(b[p+1])<<8 | int(b[p+2])
		p += 3
		for j := 0; j < cnt; j++ {
			if p+2 > len(b) {
				return nil, errShort
			}
			l := int(b[p])<<8 | int(b[p+1])
			p += 2
			if p+l > len(b) {
				return nil, errShort
			}
			c.NALs = append(c.NALs, b[p:p+l])
			p += l
		}
	}
	return c, nil
}
