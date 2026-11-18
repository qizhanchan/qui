package mp3

// MPEG audio versions as coded in the header (ISO/IEC 11172-3 2.4.2.3,
// ISO/IEC 13818-3 2.4.2.3 and the unofficial MPEG-2.5 extension).
const (
	MPEG25 = 0
	MPEG2  = 2
	MPEG1  = 3
)

// Channel modes.
const (
	ModeStereo      = 0
	ModeJointStereo = 1
	ModeDual        = 2
	ModeMono        = 3
)

// Header is a decoded 32-bit MPEG audio frame header.
type Header struct {
	Version    int // MPEG1, MPEG2 or MPEG25
	Layer      int // 1, 2 or 3
	CRC        bool
	Bitrate    int // bits per second
	SampleRate int
	Padding    bool
	Mode       int // ModeStereo ... ModeMono
	ModeExt    int
	Emphasis   int

	srIndex int // 0..8 over 44.1, 48, 32, 22.05, 24, 16, 11.025, 12, 8 kHz
}

var bitrateTable = [2][3][15]int{
	{ // MPEG-1
		{0, 32, 64, 96, 128, 160, 192, 224, 256, 288, 320, 352, 384, 416, 448},
		{0, 32, 48, 56, 64, 80, 96, 112, 128, 160, 192, 224, 256, 320, 384},
		{0, 32, 40, 48, 56, 64, 80, 96, 112, 128, 160, 192, 224, 256, 320},
	},
	{ // MPEG-2 / 2.5 (LSF)
		{0, 32, 48, 56, 64, 80, 96, 112, 128, 144, 160, 176, 192, 224, 256},
		{0, 8, 16, 24, 32, 40, 48, 56, 64, 80, 96, 112, 128, 144, 160},
		{0, 8, 16, 24, 32, 40, 48, 56, 64, 80, 96, 112, 128, 144, 160},
	},
}

var sampleRates = [9]int{44100, 48000, 32000, 22050, 24000, 16000, 11025, 12000, 8000}

// headerOK reports whether h has a valid sync word and no reserved field
// values. Free-format streams (bitrate index 0) are rejected.
func headerOK(h uint32) bool {
	return h&0xffe00000 == 0xffe00000 &&
		(h>>19)&3 != 1 && // reserved version
		(h>>17)&3 != 0 && // reserved layer
		(h>>12)&15 != 15 && (h>>12)&15 != 0 &&
		(h>>10)&3 != 3 &&
		h&3 != 2 // reserved emphasis
}

// ParseHeader decodes the 4-byte frame header at the start of b.
func ParseHeader(b []byte) (Header, bool) {
	if len(b) < 4 {
		return Header{}, false
	}
	return parseHeader(uint32(b[0])<<24 | uint32(b[1])<<16 | uint32(b[2])<<8 | uint32(b[3]))
}

func parseHeader(h uint32) (Header, bool) {
	if !headerOK(h) {
		return Header{}, false
	}
	var hd Header
	hd.Version = int(h>>19) & 3
	hd.Layer = 4 - int(h>>17)&3
	hd.CRC = (h>>16)&1 == 0
	lsf := 0
	if hd.Version != MPEG1 {
		lsf = 1
	}
	hd.Bitrate = bitrateTable[lsf][hd.Layer-1][(h>>12)&15] * 1000
	sr := int(h>>10) & 3
	switch hd.Version {
	case MPEG1:
		hd.srIndex = sr
	case MPEG2:
		hd.srIndex = 3 + sr
	default:
		hd.srIndex = 6 + sr
	}
	hd.SampleRate = sampleRates[hd.srIndex]
	hd.Padding = (h>>9)&1 == 1
	hd.Mode = int(h>>6) & 3
	hd.ModeExt = int(h>>4) & 3
	hd.Emphasis = int(h) & 3
	return hd, true
}

// LSF reports whether the frame uses the MPEG-2/2.5 low sampling
// frequency syntax.
func (h Header) LSF() bool { return h.Version != MPEG1 }

// Channels is 1 for mono, else 2.
func (h Header) Channels() int {
	if h.Mode == ModeMono {
		return 1
	}
	return 2
}

// SamplesPerFrame is the number of PCM samples per channel in one frame.
func (h Header) SamplesPerFrame() int {
	switch {
	case h.Layer == 1:
		return 384
	case h.Layer == 3 && h.LSF():
		return 576
	}
	return 1152
}

// FrameSize is the total frame length in bytes, header included.
func (h Header) FrameSize() int {
	pad := 0
	if h.Padding {
		pad = 1
	}
	switch {
	case h.Layer == 1:
		return (12*h.Bitrate/h.SampleRate + pad) * 4
	case h.Layer == 3 && h.LSF():
		return 72*h.Bitrate/h.SampleRate + pad
	}
	return 144*h.Bitrate/h.SampleRate + pad
}

// sideInfoSize is the Layer III side information length in bytes.
func (h Header) sideInfoSize() int {
	if h.LSF() {
		if h.Mode == ModeMono {
			return 9
		}
		return 17
	}
	if h.Mode == ModeMono {
		return 17
	}
	return 32
}

// compatible reports whether two headers can belong to the same stream
// (same version, layer and sample rate), the test used when resyncing.
func compatible(a, b Header) bool {
	return a.Version == b.Version && a.Layer == b.Layer && a.SampleRate == b.SampleRate
}
