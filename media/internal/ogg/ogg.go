// Package ogg is a pure-Go Ogg demuxer for Opus and Vorbis audio.
//
// It parses pages (CRC-checked, resynchronising past damage), reassembles
// packets across pages, picks the first Opus or Vorbis logical stream of
// a multiplexed file, and derives each packet's presentation time and
// duration from granule positions plus the codec's packet durations
// (the Opus TOC; Vorbis block sizes from the setup header). Seeking
// bisects on granule positions.
//
// Timestamps follow FFmpeg's conventions: PTS = (granule-derived start
// sample − pre-skip) / rate, so the first Opus packet starts at
// −pre-skip and the first Vorbis packet (which decodes to nothing) at
// minus its own duration; the last packet's duration is trimmed to the
// final granule position.
package ogg

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"time"
)

var (
	// ErrNoStream reports a file without an Opus or Vorbis stream.
	ErrNoStream = errors.New("ogg: no Opus or Vorbis stream")
	// ErrInvalid reports a stream whose headers are malformed.
	ErrInvalid = errors.New("ogg: invalid stream")
)

// Packet is one compressed audio packet.
type Packet struct {
	Data []byte
	// PTS is the time of the packet's first sample with the pre-skip
	// removed (negative for priming packets).
	PTS time.Duration
	// Duration is the packet's duration, trimmed at the end of the
	// stream to the last granule position.
	Duration time.Duration
	// Samples is the untrimmed number of samples (at SampleRate) the
	// packet decodes to.
	Samples int
	// Granule is the granule position (samples, pre-skip included) at
	// the packet's end.
	Granule int64
}

// Reader demuxes one logical Opus or Vorbis stream.
type Reader struct {
	Codec  string // "opus" | "vorbis"
	Serial uint32
	// SampleRate is the granule rate: 48000 for Opus, the
	// identification header's rate for Vorbis.
	SampleRate int
	Channels   int
	// PreSkip is the number of samples (at SampleRate) to drop from the
	// start of the decoded stream (Opus); 0 for Vorbis.
	PreSkip int
	// SeekPreRoll is how far before a seek target decoding must start
	// to converge (80 ms for Opus, RFC 7845 §4.6; 0 for Vorbis). Seek
	// does not apply it: call Seek(t - SeekPreRoll) and discard up to t.
	SeekPreRoll time.Duration
	// Opus is the parsed OpusHead (Codec "opus").
	Opus     OpusHead
	Vendor   string
	Comments []string
	// Headers are the raw header packets: OpusHead + OpusTags, or the
	// Vorbis identification, comment and setup headers.
	Headers [][]byte
	// Bitrate is the Vorbis nominal bitrate (0 if unset).
	Bitrate int

	src     *source
	vorbis  vorbisInfo
	start   streamState // state right after the headers
	st      streamState
	firstTS int64 // granule-space start sample of the first packet
	lastGr  int64 // final granule position; -1 if none was found
	dataOff int64
}

// streamState is the packet reassembly and timing state.
type streamState struct {
	pos       int64
	lastSeq   uint32
	haveSeq   bool
	partial   []byte
	inPartial bool
	drop      bool // the packet being assembled exceeded maxPacket
	skipCont  bool // skip a continued packet whose start was lost
	granPos   int64
	granKnown bool
	held      []Packet // complete packets awaiting a granule position
	queue     []Packet
	eos       bool
	prevBlock int // Vorbis block-size state
}

func (s streamState) clone() streamState {
	s.partial = append([]byte(nil), s.partial...)
	s.held = append([]Packet(nil), s.held...)
	s.queue = append([]Packet(nil), s.queue...)
	return s
}

// bisectMin is the byte range below which Seek scans linearly.
var bisectMin int64 = 64 << 10

const (
	maxPacket   = 16 << 20 // larger packets are dropped
	maxHeld     = 4096     // packets without timing before a forced start
	bosScan     = 1 << 20  // how far to look for the stream's first page
	maxHdrBytes = 32 << 20 // cap on the header packets' total size
)

// Sniff reports whether head starts with an Ogg beginning-of-stream
// page and holds an Opus or Vorbis identification header (in a
// multiplexed file it may be on a later BOS page, so pass a few hundred
// bytes).
func Sniff(head []byte) bool {
	if len(head) < headerLen || !bytes.HasPrefix(head, []byte("OggS")) || head[4] != 0 || head[5]&flagBOS == 0 {
		return false
	}
	return bytes.Contains(head, []byte("OpusHead")) || bytes.Contains(head, []byte("\x01vorbis"))
}

// Open finds the first Opus or Vorbis stream, parses its headers and the
// final granule position (for Duration).
func Open(r io.ReaderAt, size int64) (*Reader, error) {
	if r == nil || size <= 0 {
		return nil, ErrNoStream
	}
	rd := &Reader{src: &source{ra: r, size: size}, lastGr: -1}
	if err := rd.findStream(); err != nil {
		return nil, err
	}
	if err := rd.readHeaders(); err != nil {
		return nil, err
	}
	rd.start = rd.st.clone()
	// The first packet's start sample.
	if p, err := rd.ReadPacket(); err == nil {
		rd.firstTS = p.Granule - int64(p.Samples)
	}
	rd.st = rd.start.clone()
	rd.lastGr = rd.findLastGranule()
	if rd.src.err != nil {
		return nil, rd.src.err
	}
	return rd, nil
}

func (r *Reader) findStream() error {
	off := int64(0)
	for off < min(r.src.size, bosScan) {
		p, ok := r.src.nextPage(off, r.src.size)
		if !ok {
			break
		}
		off = p.next
		if p.flags&flagBOS == 0 || len(p.lacing) == 0 {
			continue
		}
		switch {
		case bytes.HasPrefix(p.body, []byte("OpusHead")):
			r.Codec = "opus"
		case bytes.HasPrefix(p.body, []byte("\x01vorbis")):
			r.Codec = "vorbis"
		default:
			continue
		}
		r.Serial = p.serial
		r.st = streamState{pos: p.off}
		return nil
	}
	if r.src.err != nil {
		return r.src.err
	}
	return ErrNoStream
}

func (r *Reader) readHeaders() error {
	want := 2
	if r.Codec == "vorbis" {
		want = 3
	}
	total := 0
	for len(r.Headers) < want {
		p, ok := r.nextOwnPage()
		if !ok {
			return fmt.Errorf("ogg: %s headers incomplete: %w", r.Codec, ErrInvalid)
		}
		for _, pkt := range r.assemble(p) {
			if len(r.Headers) < want {
				total += len(pkt)
				if total > maxHdrBytes {
					return fmt.Errorf("ogg: oversized headers: %w", ErrInvalid)
				}
				r.Headers = append(r.Headers, pkt)
				if len(r.Headers) == want {
					if err := r.parseHeaders(); err != nil {
						return err
					}
				}
				continue
			}
			// Audio packets sharing the last header page (non-conforming).
			r.st.held = append(r.st.held, r.newPacket(pkt))
		}
		if len(r.Headers) == want {
			r.time(p)
		}
	}
	r.dataOff = r.st.pos
	return nil
}

func (r *Reader) parseHeaders() error {
	switch r.Codec {
	case "opus":
		h, err := ParseOpusHead(r.Headers[0])
		if err != nil {
			return err
		}
		r.Opus = h
		r.SampleRate, r.Channels, r.PreSkip = 48000, h.Channels, h.PreSkip
		r.SeekPreRoll = 80 * time.Millisecond
		if !bytes.HasPrefix(r.Headers[1], []byte("OpusTags")) {
			return fmt.Errorf("ogg: missing OpusTags: %w", ErrInvalid)
		}
		r.Vendor, r.Comments, _ = parseComments(r.Headers[1][8:])
	case "vorbis":
		v, err := parseVorbisID(r.Headers[0])
		if err != nil {
			return err
		}
		if len(r.Headers[1]) < 7 || r.Headers[1][0] != 3 || string(r.Headers[1][1:7]) != "vorbis" {
			return fmt.Errorf("ogg: bad Vorbis comment header: %w", ErrInvalid)
		}
		r.Vendor, r.Comments, _ = parseComments(r.Headers[1][7:])
		if err := v.parseSetup(r.Headers[2]); err != nil {
			return err
		}
		r.vorbis = v
		r.st.prevBlock = v.blocksize[0]
		r.SampleRate, r.Channels, r.Bitrate = v.rate, v.channels, max(v.bitrateNom, 0)
	}
	return nil
}

// nextOwnPage returns the next page of the stream, skipping others.
func (r *Reader) nextOwnPage() (page, bool) {
	for {
		p, ok := r.src.nextPage(r.st.pos, r.src.size)
		if !ok {
			r.st.pos = r.src.size
			return page{}, false
		}
		r.st.pos = p.next
		if p.serial == r.Serial {
			return p, true
		}
	}
}

// assemble feeds a page of the stream to the packet reassembler and
// returns the packets it completes.
func (r *Reader) assemble(p page) [][]byte {
	s := &r.st
	if s.haveSeq && p.seq != s.lastSeq+1 {
		// A lost page: whatever was being assembled is incomplete.
		s.partial, s.inPartial, s.drop = nil, false, false
		s.granKnown = false
	}
	s.lastSeq, s.haveSeq = p.seq, true
	cont := p.flags&flagContinued != 0
	if !cont {
		s.skipCont = false
		if s.inPartial {
			s.partial, s.inPartial, s.drop = nil, false, false
		}
	} else if !s.inPartial {
		s.skipCont = true
	}
	var out [][]byte
	off := 0
	for _, l := range p.lacing {
		seg := p.body[off : off+int(l)]
		off += int(l)
		if s.skipCont {
			if l < 255 {
				s.skipCont = false
				s.granKnown = false // the skipped packet's length is unknown
			}
			continue
		}
		if !s.drop {
			if len(s.partial)+len(seg) > maxPacket {
				s.drop, s.partial = true, nil
			} else {
				s.partial = append(s.partial, seg...)
			}
		}
		s.inPartial = true
		if l < 255 {
			if !s.drop {
				pkt := s.partial
				if pkt == nil {
					pkt = []byte{}
				}
				out = append(out, pkt)
			}
			s.partial, s.inPartial, s.drop = nil, false, false
		}
	}
	return out
}

func (r *Reader) newPacket(b []byte) Packet {
	n := 0
	switch r.Codec {
	case "opus":
		n = OpusPacketSamples(b)
	case "vorbis":
		v := r.vorbis
		v.prevBlock = r.st.prevBlock
		n = v.packetSamples(b)
		r.st.prevBlock = v.prevBlock
	}
	return Packet{Data: b, Samples: n}
}

// time assigns timestamps to held packets using page p's granule.
func (r *Reader) time(p page) {
	s := &r.st
	if len(s.held) == 0 {
		if p.granule >= 0 {
			s.granPos, s.granKnown = p.granule, true
		}
		return
	}
	var total int64
	for _, h := range s.held {
		total += int64(h.Samples)
	}
	eos := p.flags&flagEOS != 0
	var start int64
	switch {
	case p.granule >= 0 && eos && s.granKnown:
		start = s.granPos // trim to the final granule below
	case p.granule >= 0:
		start = p.granule - total
	case s.granKnown:
		start = s.granPos
	case len(s.held) > maxHeld:
		start = 0
	default:
		return // wait for a page with a granule position
	}
	for _, h := range s.held {
		end := start + int64(h.Samples)
		dur := int64(h.Samples)
		if eos && p.granule >= 0 && end > p.granule {
			dur = max(p.granule-start, 0)
		}
		h.PTS = r.toDur(start - int64(r.PreSkip))
		h.Duration = r.toDur(dur)
		h.Granule = end
		s.queue = append(s.queue, h)
		start = end
	}
	s.held = s.held[:0]
	s.granPos, s.granKnown = start, true
	if p.granule >= 0 && (!eos || start < p.granule) {
		s.granPos = p.granule
	}
}

func (r *Reader) toDur(samples int64) time.Duration {
	rate := int64(r.SampleRate)
	if rate <= 0 {
		return 0
	}
	return time.Duration(samples/rate*int64(time.Second) + samples%rate*int64(time.Second)/rate)
}

func (r *Reader) samples(d time.Duration) int64 {
	rate := int64(r.SampleRate)
	return int64(d)/int64(time.Second)*rate + int64(d)%int64(time.Second)*rate/int64(time.Second)
}

// ReadPacket returns the next audio packet, or io.EOF after the last.
func (r *Reader) ReadPacket() (Packet, error) {
	s := &r.st
	for len(s.queue) == 0 {
		if s.eos {
			return Packet{}, io.EOF
		}
		p, ok := r.nextOwnPage()
		if !ok {
			s.eos = true
			// Packets that never got a granule position: time them
			// forward if possible.
			if len(s.held) > 0 && s.granKnown {
				r.time(page{granule: -1})
			}
			if r.src.err != nil {
				return Packet{}, r.src.err
			}
			continue
		}
		for _, b := range r.assemble(p) {
			s.held = append(s.held, r.newPacket(b))
		}
		r.time(p)
		if p.flags&flagEOS != 0 {
			s.eos = true
		}
	}
	pk := s.queue[0]
	s.queue[0] = Packet{}
	s.queue = s.queue[1:]
	return pk, nil
}

// StartTime is the presentation time of the first decoded sample after
// the pre-skip (normally 0).
func (r *Reader) StartTime() time.Duration {
	return r.toDur(max(r.firstTS-int64(r.PreSkip), 0))
}

// Duration is the stream's playable duration, from the final page's
// granule position (0 when no granule position was found).
func (r *Reader) Duration() time.Duration {
	if r.lastGr < 0 {
		return 0
	}
	end := r.lastGr - int64(r.PreSkip)
	return r.toDur(max(end-max(r.firstTS-int64(r.PreSkip), 0), 0))
}

// findLastGranule scans backwards from the end for the stream's last
// page with a granule position.
func (r *Reader) findLastGranule() int64 {
	limit := r.src.size
	chunk := int64(64 << 10)
	for limit > r.dataOff {
		from := max(r.dataOff, limit-chunk)
		last := int64(-1)
		off := from
		for {
			p, ok := r.src.nextPage(off, limit)
			if !ok {
				break
			}
			if p.serial == r.Serial && p.granule >= 0 {
				last = p.granule
			}
			off = p.next
		}
		if last >= 0 {
			return last
		}
		limit = from
		chunk *= 2
	}
	return -1
}

// Seek positions the reader so that the next packet starts at or before
// t (as close as the page structure allows), by bisecting on granule
// positions. It returns that packet's PTS. For Opus, callers should seek
// SeekPreRoll before the wanted time and discard decoded audio up to it.
func (r *Reader) Seek(t time.Duration) (time.Duration, error) {
	target := r.samples(t) + int64(r.PreSkip)
	if target <= r.firstTS || r.lastGr < 0 {
		return r.rewind(), r.src.err
	}
	type anchor struct {
		p  page
		ok bool
	}
	var best, any anchor
	consider := func(p page) {
		any = anchor{p, true}
		if n := len(p.lacing); n > 0 && p.lacing[n-1] != 255 {
			best = anchor{p, true}
		}
	}
	isAnchor := func(p page) bool { return p.serial == r.Serial && p.granule >= 0 }
	lo, hi := r.dataOff, r.src.size
	for hi-lo > bisectMin {
		mid := lo + (hi-lo)/2
		var p page
		found := false
		for off := mid; ; {
			q, ok := r.src.nextPage(off, hi)
			if !ok {
				break
			}
			if isAnchor(q) {
				p, found = q, true
				break
			}
			off = q.next
		}
		if !found {
			hi = mid
			continue
		}
		if p.granule <= target {
			consider(p)
			lo = p.next
		} else {
			hi = mid
		}
	}
	for off := lo; ; {
		p, ok := r.src.nextPage(off, r.src.size)
		if !ok {
			break
		}
		off = p.next
		if !isAnchor(p) {
			continue
		}
		if p.granule > target {
			break
		}
		consider(p)
	}
	a := best
	if !a.ok {
		a = any
	}
	if !a.ok {
		return r.rewind(), r.src.err
	}
	r.st = streamState{
		pos:       a.p.next,
		lastSeq:   a.p.seq,
		haveSeq:   true,
		granPos:   a.p.granule,
		granKnown: true,
		eos:       a.p.flags&flagEOS != 0,
		prevBlock: r.vorbis.blocksize[0],
	}
	return r.toDur(a.p.granule - int64(r.PreSkip)), r.src.err
}

func (r *Reader) rewind() time.Duration {
	r.st = r.start.clone()
	return r.toDur(r.firstTS - int64(r.PreSkip))
}
