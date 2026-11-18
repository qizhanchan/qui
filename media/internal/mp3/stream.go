package mp3

import (
	"encoding/binary"
	"errors"
	"io"
	"time"
)

// decoderDelay is the MPEG audio decoder's own delay in samples (hybrid
// filterbank 528 + synthesis 1), which gapless trimming adds to the
// encoder delay, as FFmpeg and LAME do.
const decoderDelay = 529

const (
	maxSyncScan = 1 << 20 // bytes searched for the first frame / on resync
	confirmRun  = 3       // consecutive frames that confirm a sync point
)

// ErrNotMPEG reports input without a recognisable MPEG audio stream.
var ErrNotMPEG = errors.New("mp3: no MPEG audio frames found")

// Stream demultiplexes a raw MPEG audio elementary stream (.mp3, .mp2,
// .mpa): it skips ID3v2/ID3v1/APE tags, finds and validates frame sync,
// parses the Xing/Info/VBRI header and the LAME gapless tag, and returns
// one frame at a time.
type Stream struct {
	r         io.ReaderAt
	dataStart int64 // first audio frame (after any Xing/VBRI frame)
	dataEnd   int64 // end of audio (trailing tags excluded)
	tagStart  int64 // start of the Xing/VBRI frame or first frame
	first     Header

	pos   int64 // offset of the next frame
	frame int64 // index of the next frame

	frames   int64 // total audio frames, -1 if unknown
	tocBytes int64 // byte span the TOC refers to
	toc      []int64
	tocFrac  bool // toc holds Xing percent entries (0..256) instead of offsets
	vbr      bool // Xing (not Info) or VBRI header: frame sizes vary

	vbriPerEntry int64 // frames per VBRI TOC entry

	delay, padding int
	hasGapless     bool

	index []int64 // frame offsets once BuildIndex ran

	hdr [4]byte
	buf []byte
}

// Sniff reports whether head (the first bytes of a file, ideally a few
// KB) looks like an MPEG audio elementary stream: an ID3v2 tag, or a run
// of consecutive valid frame headers.
func Sniff(head []byte) bool {
	off := 0
	for {
		n, ok := id3v2Size(head[off:])
		if !ok {
			break
		}
		off += n
		if off >= len(head) {
			return true // tag larger than the probe: trust it
		}
	}
	limit := len(head)
	if limit > off+8192 {
		limit = off + 8192
	}
	for p := off; p+4 <= limit; p++ {
		h, ok := ParseHeader(head[p:])
		if !ok {
			continue
		}
		run, open := 1, false
		q := p + h.FrameSize()
		for run < confirmRun {
			if q+4 > len(head) {
				open = true
				break
			}
			h2, ok := ParseHeader(head[q:])
			if !ok || !compatible(h, h2) {
				break
			}
			run++
			q += h2.FrameSize()
		}
		if run >= confirmRun || (open && (run >= 2 || p == off)) {
			return true
		}
	}
	return false
}

// id3v2Size returns the total size of an ID3v2 tag at the start of b.
func id3v2Size(b []byte) (int, bool) {
	if len(b) < 10 || b[0] != 'I' || b[1] != 'D' || b[2] != '3' ||
		b[3] == 0xff || b[4] == 0xff ||
		b[6]&0x80 != 0 || b[7]&0x80 != 0 || b[8]&0x80 != 0 || b[9]&0x80 != 0 {
		return 0, false
	}
	n := 10 + (int(b[6])<<21 | int(b[7])<<14 | int(b[8])<<7 | int(b[9]))
	if b[5]&0x10 != 0 { // footer present
		n += 10
	}
	return n, true
}

// Open parses the stream structure of r (size bytes long) and positions
// it at the first audio frame.
func Open(r io.ReaderAt, size int64) (*Stream, error) {
	s := &Stream{r: r, dataEnd: size, frames: -1}
	// Leading ID3v2 tags (possibly several).
	var head [10]byte
	for {
		if _, err := r.ReadAt(head[:], s.dataStart); err != nil {
			break
		}
		n, ok := id3v2Size(head[:])
		if !ok {
			break
		}
		s.dataStart += int64(n)
	}
	s.stripTrailingTags()
	if s.dataStart >= s.dataEnd {
		return nil, ErrNotMPEG
	}
	pos, h, ok := s.syncFrom(s.dataStart, nil)
	if !ok {
		return nil, ErrNotMPEG
	}
	s.first = h
	s.tagStart = pos
	s.dataStart = pos
	s.parseInfoFrame(pos, h)
	if s.dataStart != pos {
		// The info frame may use another bitrate than the audio (LAME
		// picks one large enough for the tag); describe the audio.
		if h2, ok := s.headerAt(s.dataStart); ok && compatible(h, h2) {
			s.first = h2
		}
	}
	s.pos = s.dataStart
	return s, nil
}

func (s *Stream) stripTrailingTags() {
	var b [128]byte
	if s.dataEnd-s.dataStart >= 128 {
		if _, err := s.r.ReadAt(b[:], s.dataEnd-128); err == nil && string(b[:3]) == "TAG" {
			s.dataEnd -= 128
			// Enhanced "TAG+" block in front of ID3v1.
			if s.dataEnd-s.dataStart >= 227 {
				var e [4]byte
				if _, err := s.r.ReadAt(e[:], s.dataEnd-227); err == nil && string(e[:]) == "TAG+" {
					s.dataEnd -= 227
				}
			}
		}
	}
	if s.dataEnd-s.dataStart >= 32 {
		var f [32]byte
		if _, err := s.r.ReadAt(f[:], s.dataEnd-32); err == nil && string(f[:8]) == "APETAGEX" {
			n := int64(binary.LittleEndian.Uint32(f[12:16]))
			flags := binary.LittleEndian.Uint32(f[20:24])
			if flags&(1<<31) != 0 {
				n += 32
			}
			if n >= 32 && n <= s.dataEnd-s.dataStart {
				s.dataEnd -= n
			}
		}
	}
}

func (s *Stream) readAt(b []byte, off int64) bool {
	if off < 0 || off+int64(len(b)) > s.dataEnd {
		return false
	}
	n, err := s.r.ReadAt(b, off)
	return n == len(b) && (err == nil || err == io.EOF)
}

func (s *Stream) headerAt(off int64) (Header, bool) {
	if !s.readAt(s.hdr[:], off) {
		return Header{}, false
	}
	return ParseHeader(s.hdr[:])
}

// syncFrom finds the first offset >= from holding a header confirmed by
// confirmRun consecutive compatible frames (or frames reaching the end of
// the data). With ref non-nil the frames must also be compatible with it.
func (s *Stream) syncFrom(from int64, ref *Header) (int64, Header, bool) {
	const chunk = 4096
	buf := make([]byte, chunk+3)
	limit := from + maxSyncScan
	if limit > s.dataEnd {
		limit = s.dataEnd
	}
	for base := from; base < limit; base += chunk {
		n := int64(len(buf))
		if base+n > s.dataEnd {
			n = s.dataEnd - base
		}
		if n < 4 {
			break
		}
		got, _ := s.r.ReadAt(buf[:n], base)
		for i := 0; i+4 <= got && i < chunk; i++ {
			if buf[i] != 0xff || buf[i+1]&0xe0 != 0xe0 {
				continue
			}
			h, ok := ParseHeader(buf[i:])
			if !ok || (ref != nil && !compatible(*ref, h)) {
				continue
			}
			if s.confirm(base+int64(i), h) {
				return base + int64(i), h, true
			}
		}
	}
	return 0, Header{}, false
}

func (s *Stream) confirm(pos int64, h Header) bool {
	q := pos
	cur := h
	for run := 1; ; run++ {
		q += int64(cur.FrameSize())
		if q >= s.dataEnd || q+4 > s.dataEnd {
			// The run reached the end of the data: accept a lone frame
			// only if it ends exactly there.
			return run > 1 || q == s.dataEnd
		}
		if run >= confirmRun {
			return true
		}
		h2, ok := s.headerAt(q)
		if !ok || !compatible(h, h2) {
			return false
		}
		cur = h2
	}
}

// parseInfoFrame inspects the first frame for a Xing/Info or VBRI header
// (FFmpeg mp3_parse_info_tag / mp3_parse_vbri_tag).
func (s *Stream) parseInfoFrame(pos int64, h Header) {
	size := h.FrameSize()
	fb := make([]byte, size)
	if !s.readAt(fb, pos) {
		return
	}
	if h.Layer == 3 {
		off := 4 + h.sideInfoSize()
		if off+8 <= size && (string(fb[off:off+4]) == "Xing" || string(fb[off:off+4]) == "Info") {
			s.vbr = string(fb[off:off+4]) == "Xing"
			p := off + 4
			flags := be32(fb, p)
			p += 4
			s.dataStart = pos + int64(size)
			if flags&1 != 0 && p+4 <= size {
				s.frames = int64(be32(fb, p))
				p += 4
			}
			if flags&2 != 0 && p+4 <= size {
				s.tocBytes = int64(be32(fb, p))
				p += 4
			}
			if flags&4 != 0 && p+100 <= size {
				s.toc = make([]int64, 100)
				for i := range s.toc {
					s.toc[i] = int64(fb[p+i])
				}
				s.tocFrac = true
				p += 100
			}
			if flags&8 != 0 {
				p += 4
			}
			// LAME / Lavc extension: 9-byte version, then delay/padding
			// at +21 (two 12-bit fields).
			if p+24 <= size {
				v := string(fb[p : p+4])
				if v == "LAME" || v == "Lavf" || v == "Lavc" {
					d := int(fb[p+21])<<16 | int(fb[p+22])<<8 | int(fb[p+23])
					s.delay = d >> 12
					s.padding = d & 0xfff
					s.hasGapless = true
				}
			}
			if s.tocBytes <= 0 {
				s.tocBytes = s.dataEnd - pos
			}
			return
		}
	}
	if off := 4 + 32; off+26 <= size && string(fb[off:off+4]) == "VBRI" && be16(fb, off+4) == 1 {
		s.dataStart = pos + int64(size)
		s.vbr = true
		s.tocBytes = int64(be32(fb, off+10))
		s.frames = int64(be32(fb, off+14))
		n := be16(fb, off+18)
		scale := int64(be16(fb, off+20))
		esz := be16(fb, off+22)
		perEntry := int64(be16(fb, off+24))
		p := off + 26
		if esz >= 1 && esz <= 4 && p+n*esz <= size && perEntry > 0 && n > 0 {
			// toc[i] = byte offset of frame i*perEntry, relative to the
			// VBRI frame; seeking interpolates between entries.
			s.toc = make([]int64, n+1)
			acc := int64(0)
			for i := 0; i < n; i++ {
				var v int64
				for k := 0; k < esz; k++ {
					v = v<<8 | int64(fb[p])
					p++
				}
				s.toc[i] = acc
				acc += v * scale
			}
			s.toc[n] = acc
			s.vbriPerEntry = perEntry
		}
	}
}

func be32(b []byte, p int) uint32 { return binary.BigEndian.Uint32(b[p:]) }
func be16(b []byte, p int) int    { return int(binary.BigEndian.Uint16(b[p:])) }

// Header is the first audio frame's header.
func (s *Stream) Header() Header { return s.first }

// SampleRate of the stream.
func (s *Stream) SampleRate() int { return s.first.SampleRate }

// Channels of the stream (1 or 2).
func (s *Stream) Channels() int { return s.first.Channels() }

// SamplesPerFrame per channel.
func (s *Stream) SamplesPerFrame() int { return s.first.SamplesPerFrame() }

// NumFrames is the number of audio frames, from the Xing/VBRI header or
// BuildIndex; ok is false when unknown.
func (s *Stream) NumFrames() (n int64, ok bool) { return s.frames, s.frames >= 0 }

// Gapless returns the LAME/Lavc tag's encoder delay and end padding in
// samples; ok is false when the stream carries no such tag.
func (s *Stream) Gapless() (delay, padding int, ok bool) {
	return s.delay, s.padding, s.hasGapless
}

// trimStart is the number of leading decoded samples (per channel) that
// gapless playback drops.
func (s *Stream) trimStart() int64 {
	if !s.hasGapless {
		return 0
	}
	return int64(s.delay) + decoderDelay
}

// validSamples is the gapless length in samples per channel, -1 when the
// frame count is unknown.
func (s *Stream) validSamples() int64 {
	if s.frames < 0 {
		return -1
	}
	n := s.frames * int64(s.SamplesPerFrame())
	if s.hasGapless {
		n -= int64(s.delay + s.padding)
		if n < 0 {
			n = 0
		}
	}
	return n
}

// Duration is the playable duration: exact when the frame count is known
// (gapless-trimmed when the stream has a LAME tag), otherwise estimated
// from the first frame's bitrate.
func (s *Stream) Duration() time.Duration {
	sr := int64(s.SampleRate())
	if n := s.validSamples(); n >= 0 {
		return time.Duration(n * int64(time.Second) / sr)
	}
	bits := (s.dataEnd - s.dataStart) * 8
	return time.Duration(float64(bits) / float64(s.first.Bitrate) * float64(time.Second))
}

// FrameIndex is the index of the frame the next ReadFrame returns
// (estimated after a seek without an exact index).
func (s *Stream) FrameIndex() int64 { return s.frame }

// ReadFrame returns the next frame, header included. The slice is valid
// until the next call. Garbage between frames is skipped by resyncing on
// a confirmed header; io.EOF marks the end of the audio data, including a
// final frame cut short by the end of the file.
func (s *Stream) ReadFrame() ([]byte, error) {
	for {
		if s.pos+4 > s.dataEnd {
			return nil, io.EOF
		}
		h, ok := s.headerAt(s.pos)
		if !ok || !compatible(s.first, h) {
			pos, _, found := s.syncFrom(s.pos+1, &s.first)
			if !found {
				s.pos = s.dataEnd
				return nil, io.EOF
			}
			s.pos = pos
			continue
		}
		size := int64(h.FrameSize())
		if s.pos+size > s.dataEnd {
			s.pos = s.dataEnd
			return nil, io.EOF
		}
		if cap(s.buf) < int(size) {
			s.buf = make([]byte, size)
		}
		b := s.buf[:size]
		if !s.readAt(b, s.pos) {
			s.pos = s.dataEnd
			return nil, io.EOF
		}
		s.pos += size
		s.frame++
		return b, nil
	}
}

// BuildIndex scans the whole stream once and records every frame offset,
// making NumFrames, Duration and seeking exact. The read position is
// restored.
func (s *Stream) BuildIndex() error {
	pos, frame := s.pos, s.frame
	s.pos, s.frame = s.dataStart, 0
	var idx []int64
	for {
		if s.pos+4 > s.dataEnd {
			break
		}
		h, ok := s.headerAt(s.pos)
		if !ok || !compatible(s.first, h) {
			p, _, found := s.syncFrom(s.pos+1, &s.first)
			if !found {
				break
			}
			s.pos = p
			continue
		}
		size := int64(h.FrameSize())
		if s.pos+size > s.dataEnd {
			break
		}
		idx = append(idx, s.pos)
		s.pos += size
	}
	s.index = idx
	s.frames = int64(len(idx))
	s.pos, s.frame = pos, frame
	return nil
}

// SeekFrame positions the stream at the frame containing time t (measured
// on the gapless-trimmed timeline) and returns that frame's start time on
// the same timeline (it can be negative for the first frames of a gapless
// stream, whose output is partly trimmed). Without an index the position
// comes from the Xing/VBRI TOC or a constant-bitrate estimate and is then
// resynced to a confirmed frame header, so the returned time is an
// estimate. The decoder must be Reset after seeking.
func (s *Stream) SeekFrame(t time.Duration) (time.Duration, error) {
	if t < 0 {
		t = 0
	}
	sr := int64(s.SampleRate())
	spf := int64(s.SamplesPerFrame())
	sample := int64(t)*sr/int64(time.Second) + s.trimStart()
	k := sample / spf
	if err := s.seekToFrame(k); err != nil {
		return 0, err
	}
	return s.frameTime(s.frame), nil
}

func (s *Stream) frameTime(k int64) time.Duration {
	sr := int64(s.SampleRate())
	return samplesToDuration(k*int64(s.SamplesPerFrame())-s.trimStart(), sr)
}

// seekToFrame positions the stream at frame k (clamped to the stream).
func (s *Stream) seekToFrame(k int64) error {
	if k < 0 {
		k = 0
	}
	if s.frames >= 0 && k > s.frames {
		k = s.frames
	}
	if s.index != nil {
		if k >= int64(len(s.index)) {
			s.pos, s.frame = s.dataEnd, int64(len(s.index))
			return nil
		}
		s.pos, s.frame = s.index[k], k
		return nil
	}
	if k == 0 {
		s.pos, s.frame = s.dataStart, 0
		return nil
	}
	var pos int64
	switch {
	case !s.vbr:
		// Constant bitrate: frame k starts within a byte of k times the
		// mean frame size (padding spreads the fraction), so a search
		// starting just before lands on it exactly.
		bytesPerFrame := float64(s.SamplesPerFrame()) / 8 * float64(s.first.Bitrate) / float64(s.SampleRate())
		pos = s.dataStart + int64(float64(k)*bytesPerFrame) - 2
	case s.toc != nil && s.tocFrac && s.frames > 0:
		pct := float64(k) / float64(s.frames) * 100
		i := int(pct)
		if i > 99 {
			i = 99
		}
		a := float64(s.toc[i])
		b := 256.0
		if i < 99 {
			b = float64(s.toc[i+1])
		}
		f := a + (b-a)*(pct-float64(i))
		pos = s.tagStart + int64(f/256*float64(s.tocBytes))
	case s.toc != nil && s.vbriPerEntry > 0:
		i := k / s.vbriPerEntry
		if i >= int64(len(s.toc))-1 {
			i = int64(len(s.toc)) - 2
		}
		a, b := s.toc[i], s.toc[i+1]
		frac := float64(k-i*s.vbriPerEntry) / float64(s.vbriPerEntry)
		pos = s.tagStart + a + int64(frac*float64(b-a))
	default:
		// VBR without a usable TOC: interpolate over the byte range, or
		// fall back to the first frame's bitrate.
		if s.frames > 0 {
			pos = s.dataStart + (s.dataEnd-s.dataStart)*k/s.frames
		} else {
			bytesPerFrame := float64(s.SamplesPerFrame()) / 8 * float64(s.first.Bitrate) / float64(s.SampleRate())
			pos = s.dataStart + int64(float64(k)*bytesPerFrame)
		}
	}
	if pos < s.dataStart {
		pos = s.dataStart
	}
	if pos >= s.dataEnd {
		s.pos, s.frame = s.dataEnd, k
		return nil
	}
	p, _, found := s.syncFrom(pos, &s.first)
	if !found {
		s.pos, s.frame = s.dataEnd, k
		return nil
	}
	s.pos, s.frame = p, k
	return nil
}
