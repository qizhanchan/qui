package h264

// NAL unit types (spec Table 7-1).
const (
	nalSlice     = 1
	nalDPA       = 2
	nalDPB       = 3
	nalDPC       = 4
	nalIDR       = 5
	nalSEI       = 6
	nalSPS       = 7
	nalPPS       = 8
	nalAUD       = 9
	nalEndSeq    = 10
	nalEndStream = 11
	nalFiller    = 12
	nalSPSExt    = 13
)

func nalType(h byte) int   { return int(h & 0x1f) }
func nalRefIDC(h byte) int { return int((h >> 5) & 0x03) }

// SplitAnnexB splits an Annex-B byte stream into NAL units (each starting
// with its header byte). Both 3- and 4-byte start codes are accepted and
// trailing zero bytes of each unit are trimmed.
func SplitAnnexB(data []byte) [][]byte {
	var out [][]byte
	n := len(data)
	start := -1
	i := 0
	for i+2 < n {
		if data[i] == 0 && data[i+1] == 0 && data[i+2] == 1 {
			if start >= 0 {
				out = appendNAL(out, data[start:i])
			}
			i += 3
			start = i
			continue
		}
		i++
	}
	if start >= 0 && start < n {
		out = appendNAL(out, data[start:])
	}
	return out
}

func appendNAL(out [][]byte, nal []byte) [][]byte {
	for len(nal) > 0 && nal[len(nal)-1] == 0 {
		nal = nal[:len(nal)-1]
	}
	if len(nal) > 0 {
		out = append(out, nal)
	}
	return out
}

// SplitLengthPrefixed splits an MP4 ("AVCC") sample whose NAL units carry
// big-endian length prefixes of lengthSize (1, 2 or 4) bytes.
func SplitLengthPrefixed(data []byte, lengthSize int) ([][]byte, error) {
	var out [][]byte
	for len(data) > 0 {
		if len(data) < lengthSize {
			return out, errf("h264: truncated NAL length prefix")
		}
		var l int
		for k := 0; k < lengthSize; k++ {
			l = l<<8 | int(data[k])
		}
		data = data[lengthSize:]
		if l > len(data) {
			return out, errf("h264: NAL length exceeds sample")
		}
		if l > 0 {
			out = append(out, data[:l])
		}
		data = data[l:]
	}
	return out, nil
}

// DecoderConfig is a parsed AVCDecoderConfigurationRecord (ISO 14496-15
// "avcC" box payload).
type DecoderConfig struct {
	Profile, Compat, Level byte
	LengthSize             int
	SPS, PPS               [][]byte
}

// ParseDecoderConfig parses an avcC payload.
func ParseDecoderConfig(b []byte) (*DecoderConfig, error) {
	if len(b) < 7 || b[0] != 1 {
		return nil, errf("h264: bad avcC record")
	}
	c := &DecoderConfig{Profile: b[1], Compat: b[2], Level: b[3], LengthSize: int(b[4]&3) + 1}
	p := 5
	read := func(count int) ([][]byte, error) {
		var out [][]byte
		for i := 0; i < count; i++ {
			if p+2 > len(b) {
				return nil, errf("h264: truncated avcC")
			}
			l := int(b[p])<<8 | int(b[p+1])
			p += 2
			if p+l > len(b) {
				return nil, errf("h264: truncated avcC")
			}
			out = append(out, b[p:p+l])
			p += l
		}
		return out, nil
	}
	var err error
	p = 6
	if c.SPS, err = read(int(b[5] & 0x1f)); err != nil {
		return nil, err
	}
	if p >= len(b) {
		return nil, errf("h264: truncated avcC")
	}
	n := int(b[p])
	p++
	if c.PPS, err = read(n); err != nil {
		return nil, err
	}
	return c, nil
}
