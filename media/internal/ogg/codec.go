package ogg

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"math/bits"
)

// OpusPacketSamples returns the number of 48 kHz samples a single-stream
// Opus packet decodes to (RFC 6716 §3.1: TOC config → frame size, frame
// count code 0–3), or 0 for a malformed packet.
func OpusPacketSamples(p []byte) int {
	if len(p) == 0 {
		return 0
	}
	toc := p[0]
	cfg := int(toc >> 3)
	var fs int // samples per frame at 48 kHz
	switch {
	case cfg < 12: // SILK: 10, 20, 40, 60 ms
		fs = [4]int{480, 960, 1920, 2880}[cfg&3]
	case cfg < 16: // Hybrid: 10, 20 ms
		fs = [2]int{480, 960}[cfg&1]
	default: // CELT: 2.5, 5, 10, 20 ms
		fs = [4]int{120, 240, 480, 960}[cfg&3]
	}
	var n int
	switch toc & 3 {
	case 0:
		n = 1
	case 1, 2:
		n = 2
	case 3:
		if len(p) < 2 {
			return 0
		}
		n = int(p[1] & 0x3F)
	}
	if fs*n > 5760 { // 120 ms is the most a packet may hold
		return 0
	}
	return fs * n
}

// OpusHead is the Opus identification header (RFC 7845 §5.1).
type OpusHead struct {
	Version         int
	Channels        int
	PreSkip         int // 48 kHz samples to discard at the start
	InputSampleRate int // informational only
	OutputGain      int // Q7.8 dB
	MappingFamily   int
	StreamCount     int
	CoupledCount    int
	ChannelMapping  []byte
}

// ParseOpusHead decodes an OpusHead packet.
func ParseOpusHead(b []byte) (OpusHead, error) {
	if len(b) < 19 || !bytes.HasPrefix(b, []byte("OpusHead")) {
		return OpusHead{}, errors.New("ogg: bad OpusHead")
	}
	h := OpusHead{
		Version:         int(b[8]),
		Channels:        int(b[9]),
		PreSkip:         int(binary.LittleEndian.Uint16(b[10:])),
		InputSampleRate: int(binary.LittleEndian.Uint32(b[12:])),
		OutputGain:      int(int16(binary.LittleEndian.Uint16(b[16:]))),
		MappingFamily:   int(b[18]),
		StreamCount:     1,
		CoupledCount:    max(int(b[9])-1, 0),
	}
	if h.Version>>4 != 0 || h.Channels == 0 {
		return OpusHead{}, fmt.Errorf("ogg: unsupported OpusHead version %d / %d channels", h.Version, h.Channels)
	}
	if h.MappingFamily != 0 {
		if len(b) < 21+h.Channels {
			return OpusHead{}, errors.New("ogg: truncated OpusHead channel mapping")
		}
		h.StreamCount = int(b[19])
		h.CoupledCount = int(b[20])
		h.ChannelMapping = append([]byte(nil), b[21:21+h.Channels]...)
	}
	return h, nil
}

// parseComments decodes a Vorbis comment block (also used by OpusTags).
func parseComments(b []byte) (vendor string, comments []string, err error) {
	bad := errors.New("ogg: bad comment header")
	if len(b) < 4 {
		return "", nil, bad
	}
	n := binary.LittleEndian.Uint32(b)
	b = b[4:]
	if uint64(n) > uint64(len(b)) {
		return "", nil, bad
	}
	vendor, b = string(b[:n]), b[n:]
	if len(b) < 4 {
		return vendor, nil, nil // tolerate a missing list
	}
	cnt := binary.LittleEndian.Uint32(b)
	b = b[4:]
	for i := uint32(0); i < cnt; i++ {
		if len(b) < 4 {
			return vendor, comments, bad
		}
		l := binary.LittleEndian.Uint32(b)
		b = b[4:]
		if uint64(l) > uint64(len(b)) {
			return vendor, comments, bad
		}
		comments = append(comments, string(b[:l]))
		b = b[l:]
	}
	return vendor, comments, nil
}

// vorbisInfo is what the demuxer needs from the Vorbis headers.
type vorbisInfo struct {
	channels   int
	rate       int
	blocksize  [2]int
	modeLong   []bool // per mode: blockflag
	modeMask   byte
	prevMask   byte
	prevBlock  int
	haveSetup  bool
	bitrateNom int
}

func parseVorbisID(b []byte) (vorbisInfo, error) {
	if len(b) < 30 || b[0] != 1 || string(b[1:7]) != "vorbis" {
		return vorbisInfo{}, errors.New("ogg: bad Vorbis identification header")
	}
	v := vorbisInfo{
		channels:   int(b[11]),
		rate:       int(binary.LittleEndian.Uint32(b[12:])),
		bitrateNom: int(int32(binary.LittleEndian.Uint32(b[20:]))),
	}
	b0, b1 := int(b[28]&15), int(b[28]>>4)
	if binary.LittleEndian.Uint32(b[7:]) != 0 || v.channels == 0 || v.rate <= 0 || b0 < 6 || b1 > 13 || b0 > b1 {
		return vorbisInfo{}, errors.New("ogg: unsupported Vorbis identification header")
	}
	v.blocksize = [2]int{1 << b0, 1 << b1}
	v.prevBlock = v.blocksize[0]
	return v, nil
}

// parseSetup recovers the modes' block flags from the setup header by
// walking it backwards from the framing bit (the codebooks before the
// mode list are not needed), as libavcodec's vorbis_parser does.
func (v *vorbisInfo) parseSetup(b []byte) error {
	if len(b) < 7 || b[0] != 5 || string(b[1:7]) != "vorbis" {
		return errors.New("ogg: bad Vorbis setup header")
	}
	rev := make([]byte, len(b))
	for i := range b {
		rev[i] = b[len(b)-1-i]
	}
	br := bitReader{b: rev}
	total := len(rev) * 8
	framing := -1
	for total-br.pos > 97 {
		if br.bit() == 1 {
			framing = br.pos
			break
		}
	}
	if framing < 0 {
		return errors.New("ogg: Vorbis setup header has no framing bit")
	}
	modeCount, last := 0, 0
	for total-br.pos >= 97 {
		if br.read(8) > 63 || br.read(16) != 0 || br.read(16) != 0 {
			break
		}
		br.read(1)
		modeCount++
		if modeCount > 64 {
			break
		}
		save := br.pos
		if int(br.read(6))+1 == modeCount {
			last = modeCount
		}
		br.pos = save
	}
	if last == 0 {
		return errors.New("ogg: no Vorbis modes found")
	}
	v.modeLong = make([]bool, last)
	br.pos = framing
	for i := last - 1; i >= 0; i-- {
		br.read(40)
		v.modeLong[i] = br.bit() == 1
	}
	// The mode number takes ilog(modes-1) bits after the packet-type bit.
	v.modeMask = byte(((1 << bits.Len(uint(last-1))) - 1) << 1)
	v.prevMask = (v.modeMask | 1) + 1
	v.haveSetup = true
	return nil
}

// packetSamples returns the duration of a Vorbis audio packet in samples
// (previous block / 4 + current block / 4) and advances the state.
func (v *vorbisInfo) packetSamples(p []byte) int {
	if !v.haveSetup || len(p) == 0 || p[0]&1 != 0 {
		return 0
	}
	mode := 0
	if len(v.modeLong) > 1 {
		mode = int(p[0]&v.modeMask) >> 1
	}
	if mode >= len(v.modeLong) {
		return 0
	}
	prev := v.prevBlock
	if v.modeLong[mode] {
		flag := 0
		if p[0]&v.prevMask != 0 {
			flag = 1
		}
		prev = v.blocksize[flag]
	}
	cur := v.blocksize[0]
	if v.modeLong[mode] {
		cur = v.blocksize[1]
	}
	v.prevBlock = cur
	return (prev + cur) >> 2
}

// bitReader reads MSB-first.
type bitReader struct {
	b   []byte
	pos int
}

func (r *bitReader) bit() uint32 {
	if r.pos >= len(r.b)*8 {
		r.pos++
		return 0
	}
	v := uint32(r.b[r.pos>>3]>>(7-r.pos&7)) & 1
	r.pos++
	return v
}

func (r *bitReader) read(n int) uint32 {
	var v uint32
	for i := 0; i < n; i++ {
		v = v<<1 | r.bit()
	}
	return v
}
