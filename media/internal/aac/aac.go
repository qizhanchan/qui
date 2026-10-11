// Package aac is a self-written pure-Go AAC-LC decoder. This file implements
// milestone A0: the bitstream framing — the MP4 AudioSpecificConfig and the
// ADTS transport header.
package aac

import "fmt"

// Object types (ISO/IEC 14496-3).
const (
	ObjectMain = 1
	ObjectLC   = 2
	ObjectSSR  = 3
	ObjectLTP  = 4
	ObjectSBR  = 5
	ObjectPS   = 29
)

// sampleRates is the AAC sampling-frequency-index table (Table 1.18).
var sampleRates = [16]int{
	96000, 88200, 64000, 48000, 44100, 32000, 24000, 22050,
	16000, 12000, 11025, 8000, 7350, 0, 0, 0,
}

// Config is a parsed AudioSpecificConfig (ISO/IEC 14496-3, 1.6.2.1).
type Config struct {
	ObjectType       int // 2 = AAC-LC
	SampleRateIndex  int
	SampleRate       int
	ChannelConfig    int
	Channels         int
	FrameLength      int // 1024 for LC, 960 if frameLengthFlag set
	SBRPresent       bool
	ExtensionRateIdx int
	ExtensionRate    int
}

// bitReader is a minimal MSB-first bit reader.
type bitReader struct {
	data []byte
	pos  int
}

func (r *bitReader) u(n int) (int, error) {
	v := 0
	for i := 0; i < n; i++ {
		if r.pos >= len(r.data)*8 {
			return 0, fmt.Errorf("aac: out of bits")
		}
		bit := (r.data[r.pos>>3] >> (7 - uint(r.pos&7))) & 1
		r.pos++
		v = v<<1 | int(bit)
	}
	return v, nil
}

func (r *bitReader) u1() (bool, error) {
	v, err := r.u(1)
	return v == 1, err
}

// ParseASC parses an AudioSpecificConfig blob (the MP4 `esds` DecoderSpecificInfo).
func ParseASC(data []byte) (*Config, error) {
	r := &bitReader{data: data}
	c := &Config{}

	ot, err := r.u(5)
	if err != nil {
		return nil, err
	}
	if ot == 31 {
		ext, err := r.u(6)
		if err != nil {
			return nil, err
		}
		ot = 32 + ext
	}
	c.ObjectType = ot

	sfi, err := r.u(4)
	if err != nil {
		return nil, err
	}
	c.SampleRateIndex = sfi
	if sfi == 15 {
		rate, err := r.u(24)
		if err != nil {
			return nil, err
		}
		c.SampleRate = rate
	} else {
		c.SampleRate = sampleRates[sfi]
	}

	ch, err := r.u(4)
	if err != nil {
		return nil, err
	}
	c.ChannelConfig = ch
	c.Channels = channelsFor(ch)

	// Explicit SBR/PS signalling (hierarchical).
	if c.ObjectType == ObjectSBR || c.ObjectType == ObjectPS {
		esfi, err := r.u(4)
		if err != nil {
			return nil, err
		}
		c.SBRPresent = true
		c.ExtensionRateIdx = esfi
		if esfi == 15 {
			rate, err := r.u(24)
			if err != nil {
				return nil, err
			}
			c.ExtensionRate = rate
		} else {
			c.ExtensionRate = sampleRates[esfi]
		}
		if c.ObjectType == ObjectPS {
			_, _ = r.u(5) // psObjectType
		}
		// The base object type follows.
		base, err := r.u(5)
		if err != nil {
			return nil, err
		}
		if base == 31 {
			ext, err := r.u(6)
			if err != nil {
				return nil, err
			}
			base = 32 + ext
		}
		c.ObjectType = base
	}

	// GASpecificConfig.
	c.FrameLength = 1024
	if flag, err := r.u1(); err == nil && flag {
		c.FrameLength = 960
	}
	if dep, err := r.u1(); err == nil && dep {
		_, _ = r.u(14) // coreCoderDelay
	}
	_, _ = r.u1() // extensionFlag

	return c, nil
}

func channelsFor(channelConfig int) int {
	switch channelConfig {
	case 0:
		return 0 // defined by a program_config_element
	case 7:
		return 8 // 7.1
	default:
		return channelConfig
	}
}

// ADTSHeader is a parsed ADTS frame header.
type ADTSHeader struct {
	ObjectType      int
	SampleRateIndex int
	SampleRate      int
	Channels        int
	FrameLength     int // total frame length including the header
	HeaderLength    int // 7 or 9 bytes
	RawBlocks       int // number_of_raw_data_blocks_in_frame + 1
}

// ParseADTS parses an ADTS header at the start of b. It returns the header and
// the number of bytes consumed (the header length).
func ParseADTS(b []byte) (*ADTSHeader, int, error) {
	if len(b) < 7 {
		return nil, 0, fmt.Errorf("aac: ADTS header too short")
	}
	if b[0] != 0xFF || b[1]&0xF0 != 0xF0 {
		return nil, 0, fmt.Errorf("aac: bad ADTS syncword")
	}
	protectionAbsent := b[1]&0x01 == 1
	profile := int(b[2]>>6) + 1
	sfi := int(b[2]>>2) & 0x0F
	ch := int(b[2]&0x01)<<2 | int(b[3]>>6)
	frameLen := int(b[3]&0x03)<<11 | int(b[4])<<3 | int(b[5]>>5)
	rawBlocks := int(b[6]&0x03) + 1

	h := &ADTSHeader{
		ObjectType:      profile,
		SampleRateIndex: sfi,
		SampleRate:      sampleRates[sfi],
		Channels:        ch,
		FrameLength:     frameLen,
		RawBlocks:       rawBlocks,
	}
	h.HeaderLength = 7
	if !protectionAbsent {
		h.HeaderLength = 9
	}
	return h, h.HeaderLength, nil
}

// RawBlocks splits an ADTS stream into the raw AAC access units (the payloads
// after each ADTS header).
func RawBlocks(b []byte) ([][]byte, error) {
	var out [][]byte
	for len(b) > 0 {
		h, hl, err := ParseADTS(b)
		if err != nil {
			return out, err
		}
		if h.FrameLength < hl || h.FrameLength > len(b) {
			return out, fmt.Errorf("aac: bad ADTS frame length %d", h.FrameLength)
		}
		out = append(out, b[hl:h.FrameLength])
		b = b[h.FrameLength:]
	}
	return out, nil
}
