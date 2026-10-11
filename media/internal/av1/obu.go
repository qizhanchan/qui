package av1

import "fmt"

// OBU types (spec 6.2.2).
const (
	obuSequenceHeader       = 1
	obuTemporalDelimiter    = 2
	obuFrameHeader          = 3
	obuTileGroup            = 4
	obuMetadata             = 5
	obuFrame                = 6
	obuRedundantFrameHeader = 7
	obuTileList             = 8
	obuPadding              = 15
)

// obu is one open bitstream unit of the low-overhead format (spec 5.3).
type obu struct {
	typ        int
	temporalID int
	spatialID  int
	hasExt     bool
	hdrLen     int // bytes before the payload
	payload    []byte
}

// splitOBUs splits a temporal unit (or any run of OBUs, each carrying
// obu_size, as in IVF, MP4 samples and av1C) into OBUs.
func splitOBUs(b []byte) ([]obu, error) {
	var out []obu
	for len(b) > 0 {
		h := b[0]
		if h&0x80 != 0 {
			return out, fmt.Errorf("%w: forbidden bit set", ErrInvalid)
		}
		o := obu{typ: int(h>>3) & 15, hasExt: h&4 != 0}
		n := 1
		if o.hasExt {
			if len(b) < 2 {
				return out, fmt.Errorf("%w: truncated OBU header", ErrInvalid)
			}
			o.temporalID, o.spatialID = int(b[1]>>5), int(b[1]>>3)&3
			n = 2
		}
		size := uint64(len(b) - n)
		if h&2 != 0 {
			v, l := leb128(b[n:])
			if l == 0 {
				return out, fmt.Errorf("%w: bad obu_size", ErrInvalid)
			}
			size, n = v, n+l
		}
		if uint64(len(b)-n) < size {
			return out, fmt.Errorf("%w: OBU of %d bytes exceeds the %d left", ErrInvalid, size, len(b)-n)
		}
		o.hdrLen = n
		o.payload = b[n : n+int(size)]
		out = append(out, o)
		b = b[n+int(size):]
	}
	return out, nil
}
