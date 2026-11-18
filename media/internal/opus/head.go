package opus

import (
	"encoding/binary"
	"fmt"
)

// Head is the identification header ("OpusHead", RFC 7845 section 5.1),
// as found at the start of an Ogg Opus stream or in Matroska/WebM
// CodecPrivate.
type Head struct {
	Version        int
	Channels       int // output channel count
	PreSkip        int // samples at 48 kHz to discard at the start
	InputRate      int // original sample rate (informational only)
	OutputGain     int // Q7.8 dB gain to apply to the output
	MappingFamily  int // 0 (mono/stereo), 1 (Vorbis order surround) or 255
	Streams        int // number of Opus streams
	CoupledStreams int // number of those streams that are stereo
	Mapping        []byte
}

// ParseOpusHead parses an OpusHead packet.
func ParseOpusHead(b []byte) (Head, error) {
	var h Head
	if len(b) < 19 || string(b[:8]) != "OpusHead" {
		return h, fmt.Errorf("opus: not an OpusHead: %w", ErrBadArg)
	}
	h.Version = int(b[8])
	if h.Version>>4 != 0 {
		return h, fmt.Errorf("opus: unsupported OpusHead version %d: %w", h.Version, ErrBadArg)
	}
	h.Channels = int(b[9])
	h.PreSkip = int(binary.LittleEndian.Uint16(b[10:]))
	h.InputRate = int(binary.LittleEndian.Uint32(b[12:]))
	h.OutputGain = int(int16(binary.LittleEndian.Uint16(b[16:])))
	h.MappingFamily = int(b[18])
	if h.Channels == 0 {
		return h, fmt.Errorf("opus: OpusHead with zero channels: %w", ErrBadArg)
	}
	if h.MappingFamily == 0 {
		if h.Channels > 2 {
			return h, fmt.Errorf("opus: mapping family 0 with %d channels: %w", h.Channels, ErrBadArg)
		}
		h.Streams = 1
		h.CoupledStreams = h.Channels - 1
		h.Mapping = []byte{0, 1}[:h.Channels]
		return h, nil
	}
	if len(b) < 21+h.Channels {
		return h, fmt.Errorf("opus: truncated OpusHead channel mapping: %w", ErrBadArg)
	}
	h.Streams = int(b[19])
	h.CoupledStreams = int(b[20])
	if h.Streams == 0 || h.CoupledStreams > h.Streams || h.Streams+h.CoupledStreams > 255 {
		return h, fmt.Errorf("opus: bad stream counts %d/%d: %w", h.Streams, h.CoupledStreams, ErrBadArg)
	}
	if h.MappingFamily == 1 && h.Channels > 8 {
		return h, fmt.Errorf("opus: mapping family 1 with %d channels: %w", h.Channels, ErrBadArg)
	}
	h.Mapping = append([]byte(nil), b[21:21+h.Channels]...)
	for _, m := range h.Mapping {
		if m != 255 && int(m) >= h.Streams+h.CoupledStreams {
			return h, fmt.Errorf("opus: channel mapping index %d out of range: %w", m, ErrBadArg)
		}
	}
	return h, nil
}
