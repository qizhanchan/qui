package webm

import (
	"encoding/binary"
	"errors"
	"io"
	"math"
	"math/bits"
)

// Element IDs (with their length marker, as written in the file).
const (
	idEBML               = 0x1A45DFA3
	idEBMLMaxIDLength    = 0x42F2
	idEBMLMaxSizeLength  = 0x42F3
	idDocType            = 0x4282
	idDocTypeReadVersion = 0x4285

	idSegment = 0x18538067

	idSeekHead     = 0x114D9B74
	idSeek         = 0x4DBB
	idSeekID       = 0x53AB
	idSeekPosition = 0x53AC

	idInfo          = 0x1549A966
	idTimecodeScale = 0x2AD7B1
	idDuration      = 0x4489

	idTracks          = 0x1654AE6B
	idTrackEntry      = 0xAE
	idTrackNumber     = 0xD7
	idTrackUID        = 0x73C5
	idTrackType       = 0x83
	idFlagEnabled     = 0xB9
	idFlagDefault     = 0x88
	idDefaultDuration = 0x23E383
	idName            = 0x536E
	idLanguage        = 0x22B59C
	idCodecID         = 0x86
	idCodecPrivate    = 0x63A2
	idCodecDelay      = 0x56AA
	idSeekPreRoll     = 0x56BB

	idVideo         = 0xE0
	idPixelWidth    = 0xB0
	idPixelHeight   = 0xBA
	idDisplayWidth  = 0x54B0
	idDisplayHeight = 0x54BA
	idFrameRate     = 0x2383E3

	idAudio                   = 0xE1
	idSamplingFrequency       = 0xB5
	idOutputSamplingFrequency = 0x78B5
	idChannels                = 0x9F
	idBitDepth                = 0x6264

	idContentEncodings    = 0x6D80
	idContentEncoding     = 0x6240
	idContentEncodingType = 0x5033
	idContentCompression  = 0x5034
	idContentCompAlgo     = 0x4254
	idContentCompSettings = 0x4255
	idContentEncryption   = 0x5035

	idCues               = 0x1C53BB6B
	idCuePoint           = 0xBB
	idCueTime            = 0xB3
	idCueTrackPositions  = 0xB7
	idCueTrack           = 0xF7
	idCueClusterPosition = 0xF1

	idCluster        = 0x1F43B675
	idTimestamp      = 0xE7
	idSimpleBlock    = 0xA3
	idBlockGroup     = 0xA0
	idBlock          = 0xA1
	idBlockDuration  = 0x9B
	idReferenceBlock = 0xFB
	idDiscardPadding = 0x75A2

	idVoid  = 0xEC
	idCRC32 = 0xBF

	idTags        = 0x1254C367
	idChapters    = 0x1043A770
	idAttachments = 0x1941A469
)

// isTopLevel reports whether id is a Segment child (or a new EBML
// stream), which terminates an unknown-size Cluster.
func isTopLevel(id uint32) bool {
	switch id {
	case idCluster, idCues, idTags, idChapters, idAttachments, idSeekHead,
		idInfo, idTracks, idEBML, idSegment:
		return true
	}
	return false
}

var (
	// ErrInvalid reports input that is not a well-formed Matroska stream.
	ErrInvalid = errors.New("webm: invalid data")
	// ErrNoTrack reports a nil or foreign track.
	ErrNoTrack = errors.New("webm: no such track")
	// ErrUnsupported reports content the demuxer cannot return (for
	// example zlib-compressed or encrypted tracks).
	ErrUnsupported = errors.New("webm: unsupported content encoding")
)

// readID decodes an element ID (marker kept, 1–4 bytes).
func readID(b []byte) (uint32, int, bool) {
	if len(b) == 0 || b[0] == 0 {
		return 0, 0, false
	}
	n := bits.LeadingZeros8(b[0]) + 1
	if n > 4 || len(b) < n {
		return 0, 0, false
	}
	var v uint32
	for i := 0; i < n; i++ {
		v = v<<8 | uint32(b[i])
	}
	return v, n, true
}

// readVint decodes a data-size vint (marker stripped, 1–8 bytes). An
// all-ones value is the reserved "unknown size" and is returned as -1.
func readVint(b []byte) (int64, int, bool) {
	if len(b) == 0 || b[0] == 0 {
		return 0, 0, false
	}
	n := bits.LeadingZeros8(b[0]) + 1
	if len(b) < n {
		return 0, 0, false
	}
	v := uint64(b[0]) & (0xFF >> n)
	allOnes := v == 0xFF>>n
	for i := 1; i < n; i++ {
		v = v<<8 | uint64(b[i])
		if b[i] != 0xFF {
			allOnes = false
		}
	}
	if allOnes {
		return -1, n, true
	}
	if v > math.MaxInt64/2 {
		return 0, 0, false
	}
	return int64(v), n, true
}

// readSvint decodes a signed vint (EBML lacing differences).
func readSvint(b []byte) (int64, int, bool) {
	if len(b) == 0 || b[0] == 0 {
		return 0, 0, false
	}
	n := bits.LeadingZeros8(b[0]) + 1
	if len(b) < n {
		return 0, 0, false
	}
	v := uint64(b[0]) & (0xFF >> n)
	for i := 1; i < n; i++ {
		v = v<<8 | uint64(b[i])
	}
	bias := int64(1)<<(7*n-1) - 1
	return int64(v) - bias, n, true
}

func readUint(b []byte) uint64 {
	if len(b) > 8 {
		b = b[len(b)-8:]
	}
	var v uint64
	for _, c := range b {
		v = v<<8 | uint64(c)
	}
	return v
}

func readInt(b []byte) int64 {
	if len(b) == 0 {
		return 0
	}
	if len(b) > 8 {
		b = b[len(b)-8:]
	}
	v := int64(int8(b[0]))
	for _, c := range b[1:] {
		v = v<<8 | int64(c)
	}
	return v
}

func readFloat(b []byte) float64 {
	switch len(b) {
	case 4:
		return float64(math.Float32frombits(binary.BigEndian.Uint32(b)))
	case 8:
		return math.Float64frombits(binary.BigEndian.Uint64(b))
	}
	return 0
}

func readString(b []byte) string {
	for i, c := range b {
		if c == 0 {
			return string(b[:i])
		}
	}
	return string(b)
}

// elem is a parsed element header.
type elem struct {
	id      uint32
	off     int64 // start of the ID
	dataOff int64 // start of the payload
	size    int64 // payload size; -1 = unknown
}

// end returns the payload end, bounded by limit (unknown size → limit).
func (e elem) end(limit int64) int64 {
	if e.size < 0 || e.dataOff+e.size > limit {
		return limit
	}
	return e.dataOff + e.size
}

const winSize = 32 << 10

// source reads small spans of the input through a one-window cache, so
// walking element headers costs one ReadAt per window rather than per
// element. It is not safe for concurrent use; Reader guards it.
type source struct {
	ra     io.ReaderAt
	size   int64
	mem    []byte // the whole input (ra == nil)
	win    []byte
	winOff int64
	winLen int
}

func memSource(b []byte) *source { return &source{mem: b, size: int64(len(b))} }

// bytes returns up to n bytes at off (fewer at the end of the input).
// The slice is only valid until the next call.
func (s *source) bytes(off int64, n int) ([]byte, error) {
	if off < 0 || off >= s.size || n <= 0 {
		return nil, nil
	}
	if rem := s.size - off; int64(n) > rem {
		n = int(rem)
	}
	if s.mem != nil {
		return s.mem[off : off+int64(n)], nil
	}
	if off >= s.winOff && off+int64(n) <= s.winOff+int64(s.winLen) {
		i := off - s.winOff
		return s.win[i : i+int64(n)], nil
	}
	if n > winSize {
		buf := make([]byte, n)
		m, err := s.ra.ReadAt(buf, off)
		if m < n && err != nil && err != io.EOF {
			return nil, err
		}
		return buf[:m], nil
	}
	if s.win == nil {
		s.win = make([]byte, winSize)
	}
	want := int64(winSize)
	if rem := s.size - off; want > rem {
		want = rem
	}
	m, err := s.ra.ReadAt(s.win[:want], off)
	if m < n && err != nil && err != io.EOF {
		s.winLen = 0
		return nil, err
	}
	s.winOff, s.winLen = off, m
	if m < n {
		n = m
	}
	return s.win[:n], nil
}

// header reads the element header at off.
func (s *source) header(off int64) (elem, error) {
	b, err := s.bytes(off, 12)
	if err != nil {
		return elem{}, err
	}
	id, n, ok := readID(b)
	if !ok {
		return elem{}, ErrInvalid
	}
	size, m, ok := readVint(b[n:])
	if !ok {
		return elem{}, ErrInvalid
	}
	return elem{id: id, off: off, dataOff: off + int64(n+m), size: size}, nil
}

// payload returns a copy of e's payload, at most max bytes.
func (s *source) payload(e elem, limit int64, max int) ([]byte, error) {
	if e.size < 0 || e.size > int64(max) || e.dataOff+e.size > limit {
		return nil, ErrInvalid
	}
	b, err := s.bytes(e.dataOff, int(e.size))
	if err != nil {
		return nil, err
	}
	if int64(len(b)) < e.size {
		return nil, io.ErrUnexpectedEOF
	}
	return append([]byte(nil), b...), nil
}

// children walks the children of the master payload [off, end), calling
// fn for each. A child with an unknown size or one that overruns end
// stops the walk (fn still sees the latter, with size clamped by its
// own end check). fn returns false to stop.
func (s *source) children(off, end int64, fn func(e elem) bool) error {
	for off < end {
		e, err := s.header(off)
		if err != nil {
			return err
		}
		if e.size < 0 {
			return ErrInvalid
		}
		if !fn(e) {
			return nil
		}
		off = e.dataOff + e.size
	}
	return nil
}

// find searches for the 4-byte ID pattern id at or after off, before
// limit, returning -1 when absent.
func (s *source) find(id uint32, off, limit int64) int64 {
	pat := [4]byte{byte(id >> 24), byte(id >> 16), byte(id >> 8), byte(id)}
	for off+4 <= limit {
		n := winSize
		if rem := limit - off; int64(n) > rem {
			n = int(rem)
		}
		b, err := s.bytes(off, n)
		if err != nil || len(b) < 4 {
			return -1
		}
		for i := 0; i+4 <= len(b); i++ {
			if b[i] == pat[0] && b[i+1] == pat[1] && b[i+2] == pat[2] && b[i+3] == pat[3] {
				return off + int64(i)
			}
		}
		off += int64(len(b) - 3)
	}
	return -1
}
