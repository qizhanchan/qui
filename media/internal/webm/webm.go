// Package webm is a pure-Go Matroska / WebM demuxer.
//
// It mirrors media/internal/mp4x: a Reader over an io.ReaderAt exposes
// tracks with qui codec names ("av1", "vp9", "vp8", "h264", "hevc",
// "opus", "vorbis", "aac") and an index-based sample API (Sample,
// ReadSample, SeekKeyframe, …). Unlike an MP4 sample table, Matroska
// has no up-front index of every frame, so the index is built lazily:
// clusters are scanned (block headers only, payloads are skipped) in
// file order as far as a request needs, and Cues are used where a
// request can be answered without frame indices (SeekTo + ReadFrame).
//
// Timestamps are presentation timestamps (Matroska stores PTS; blocks
// are in decode order) with the track's CodecDelay already subtracted,
// as the Matroska specification requires.
//
// For Media Source Extensions, OpenInit parses an initialization segment
// (EBML header + Segment with Info and Tracks) and ParseCluster /
// AppendBytes pull frames out of appended Clusters.
package webm

import (
	"bytes"
	"fmt"
	"io"
	"sort"
	"sync"
	"time"
)

// Kind classifies a track.
type Kind int

const (
	KindUnknown Kind = iota
	KindVideo
	KindAudio
	KindSubtitle
)

func (k Kind) String() string {
	switch k {
	case KindVideo:
		return "video"
	case KindAudio:
		return "audio"
	case KindSubtitle:
		return "subtitle"
	}
	return "unknown"
}

// Track describes one Matroska track.
type Track struct {
	Number  uint64 // TrackNumber, as referenced by blocks
	UID     uint64
	Type    uint64 // raw TrackType (1 video, 2 audio, 0x11 subtitle, …)
	Kind    Kind
	CodecID string // raw CodecID, e.g. "V_VP9"
	// Codec is the qui codec name: "av1", "vp9", "vp8", "h264", "hevc",
	// "opus", "vorbis", "aac"; "" for anything else.
	Codec string
	// CodecPrivate is the codec configuration: the AV1CodecConfigurationRecord,
	// avcC, hvcC, OpusHead, Xiph-laced Vorbis headers or AudioSpecificConfig.
	CodecPrivate []byte
	Name         string
	Language     string
	Default      bool
	Enabled      bool

	// DefaultDuration is the duration of each frame when blocks carry
	// none (0 = unset).
	DefaultDuration time.Duration
	// CodecDelay has already been subtracted from every timestamp; it is
	// reported so an Opus decoder can account for its pre-skip.
	CodecDelay time.Duration
	// SeekPreRoll is how much earlier than a seek target decoding must
	// start (80 ms for Opus).
	SeekPreRoll time.Duration

	// Video.
	Width, Height               int
	DisplayWidth, DisplayHeight int
	FPS                         float64 // from DefaultDuration or FrameRate; 0 if unknown

	// Audio.
	SampleRate       float64
	OutputSampleRate float64 // SBR; equals SampleRate when unset
	Channels         int
	BitDepth         int

	// Encrypted marks a track with ContentEncryption (frames are returned
	// as stored). Compressed marks zlib/bzlib/lzo compression, which is
	// not supported: reading its frames fails with ErrUnsupported.
	Encrypted  bool
	Compressed bool

	stripPrefix []byte // ContentCompAlgo 3 (header stripping)

	// Lazy index (guarded by Reader.mu).
	frames   []entry
	pending  int // frames[pending:] await the next block's timestamp
	keyfr    []int
	keyfrLen int // len(frames) when keyfr was last extended
}

// entry is one frame of a track's index.
type entry struct {
	off     int64 // absolute offset of the frame payload
	size    int32
	cluster int32 // index into Reader.clusters
	ts      int64 // ns, CodecDelay applied
	dur     int64 // ns; -1 = unknown yet
	pad     int64 // DiscardPadding, ns
	flags   uint8
}

const (
	flagKey uint8 = 1 << iota
	flagDiscardable
	flagInvisible
)

// Sample describes frame i of a track.
type Sample struct {
	PTS      time.Duration
	Duration time.Duration
	Keyframe bool
	// Discardable and Invisible mirror the SimpleBlock flags.
	Discardable bool
	Invisible   bool
	// DiscardPadding is the duration of audio to drop from the end of
	// the decoded frame (Opus end trimming).
	DiscardPadding time.Duration
	Offset         int64 // payload offset in the file (after any stripped header)
	Size           int   // payload size, including a stripped header
}

// Frame is one decoded-order frame returned by ReadFrame or the MSE API.
type Frame struct {
	Track          *Track
	PTS            time.Duration
	Duration       time.Duration
	Keyframe       bool
	Discardable    bool
	Invisible      bool
	DiscardPadding time.Duration
	Data           []byte
}

type clusterRec struct {
	pos int64 // offset of the Cluster element
	ts  int64 // cluster timestamp, ns
}

type cuePoint struct {
	t     int64 // ns
	track uint64
	pos   int64 // absolute Cluster offset
}

// Reader is an open Matroska stream.
type Reader struct {
	mu  sync.Mutex
	src *source
	ra  io.ReaderAt

	DocType string

	scale    int64 // TimecodeScale, ns per tick
	duration time.Duration

	segStart, segEnd int64
	tracks           []*Track
	byNum            map[uint64]*Track
	cues             []cuePoint

	// Lazy cluster index: clusters [0, len(clusters)) are fully scanned;
	// frontier is where the next unscanned top-level element starts.
	clusters []clusterRec
	frontier int64
	start    int64 // first Cluster found at Open
	done     bool
	scanErr  error

	cur cursor // ReadFrame state

	// MSE state.
	mse mseState
}

// Sniff reports whether head starts with an EBML header whose DocType is
// "webm" or "matroska" (head should hold at least the first 64 bytes).
func Sniff(head []byte) bool {
	if len(head) < 4 || head[0] != 0x1A || head[1] != 0x45 || head[2] != 0xDF || head[3] != 0xA3 {
		return false
	}
	return bytes.Contains(head[:min(len(head), 64)], []byte("webm")) ||
		bytes.Contains(head[:min(len(head), 64)], []byte("matroska"))
}

// Open parses the EBML header and the Segment's metadata (SeekHead,
// Info, Tracks, Cues). Clusters are scanned lazily.
func Open(r io.ReaderAt, size int64) (*Reader, error) {
	if r == nil || size <= 0 {
		return nil, ErrInvalid
	}
	return open(&source{ra: r, size: size}, r)
}

func open(src *source, ra io.ReaderAt) (*Reader, error) {
	rd := &Reader{src: src, ra: ra, scale: 1000000, byNum: map[uint64]*Track{}}
	if err := rd.parseHeader(); err != nil {
		return nil, err
	}
	if err := rd.parseSegment(); err != nil {
		return nil, err
	}
	if len(rd.tracks) == 0 {
		return nil, fmt.Errorf("webm: no tracks: %w", ErrInvalid)
	}
	rd.start = rd.frontier
	rd.cur.pos = rd.frontier
	return rd, nil
}

func (r *Reader) parseHeader() error {
	e, err := r.src.header(0)
	if err != nil {
		return err
	}
	if e.id != idEBML || e.size < 0 {
		return fmt.Errorf("webm: missing EBML header: %w", ErrInvalid)
	}
	end := e.end(r.src.size)
	var cerr error
	err = r.src.children(e.dataOff, end, func(c elem) bool {
		b, perr := r.src.payload(c, end, 1024)
		if perr != nil {
			cerr = perr
			return false
		}
		switch c.id {
		case idDocType:
			r.DocType = readString(b)
		case idEBMLMaxIDLength:
			if v := readUint(b); v > 4 {
				cerr = fmt.Errorf("webm: EBMLMaxIDLength %d: %w", v, ErrUnsupported)
			}
		case idEBMLMaxSizeLength:
			if v := readUint(b); v > 8 {
				cerr = fmt.Errorf("webm: EBMLMaxSizeLength %d: %w", v, ErrUnsupported)
			}
		}
		return cerr == nil
	})
	if cerr != nil {
		return cerr
	}
	if err != nil {
		return err
	}
	if r.DocType != "webm" && r.DocType != "matroska" {
		return fmt.Errorf("webm: DocType %q: %w", r.DocType, ErrInvalid)
	}
	// Find the Segment (skipping Void and the like).
	off := end
	for off < r.src.size {
		s, err := r.src.header(off)
		if err != nil {
			return err
		}
		if s.id == idSegment {
			r.segStart = s.dataOff
			r.segEnd = s.end(r.src.size)
			return nil
		}
		if s.size < 0 {
			break
		}
		off = s.dataOff + s.size
	}
	return fmt.Errorf("webm: no Segment: %w", ErrInvalid)
}

const maxMeta = 64 << 20 // cap for one metadata element (Tracks, Cues, …)

func (r *Reader) parseSegment() error {
	seek := map[uint32]int64{}
	seen := map[uint32]bool{}
	r.frontier = -1
	off := r.segStart
	for off < r.segEnd {
		e, err := r.src.header(off)
		if err != nil {
			// Damaged metadata: look for the first Cluster.
			if p := r.src.find(idCluster, off+1, r.segEnd); p >= 0 {
				r.frontier = p
			}
			break
		}
		if e.id == idCluster {
			r.frontier = off
			break
		}
		if e.size < 0 {
			break
		}
		if err := r.parseTopLevel(e, seek, seen); err != nil {
			return err
		}
		off = e.dataOff + e.size
	}
	// Metadata stored after the clusters (typically Cues), via SeekHead.
	for _, id := range []uint32{idInfo, idTracks, idCues} {
		pos, ok := seek[id]
		if !ok || seen[id] {
			continue
		}
		e, err := r.src.header(r.segStart + pos)
		if err != nil || e.id != id {
			continue
		}
		if err := r.parseTopLevel(e, seek, seen); err != nil && id != idCues {
			return err
		}
	}
	if r.frontier < 0 {
		r.frontier = r.segEnd
		r.done = true
	}
	if r.duration == 0 {
		r.duration = -1 // unknown until indexed
	}
	return nil
}

func (r *Reader) parseTopLevel(e elem, seek map[uint32]int64, seen map[uint32]bool) error {
	end := e.end(r.segEnd)
	switch e.id {
	case idSeekHead:
		seen[e.id] = true
		return r.parseSeekHead(e.dataOff, end, seek)
	case idInfo:
		seen[e.id] = true
		return r.parseInfo(e.dataOff, end)
	case idTracks:
		seen[e.id] = true
		return r.parseTracks(e.dataOff, end)
	case idCues:
		seen[e.id] = true
		r.parseCues(e.dataOff, end) // best effort: Cues are an optimisation
	}
	return nil
}

func (r *Reader) parseSeekHead(off, end int64, seek map[uint32]int64) error {
	return r.src.children(off, end, func(c elem) bool {
		if c.id != idSeek {
			return true
		}
		var id uint32
		pos := int64(-1)
		_ = r.src.children(c.dataOff, c.end(end), func(s elem) bool {
			b, err := r.src.payload(s, end, 16)
			if err != nil {
				return false
			}
			switch s.id {
			case idSeekID:
				id = uint32(readUint(b))
			case idSeekPosition:
				pos = int64(readUint(b) & (1<<62 - 1))
			}
			return true
		})
		if id != 0 && pos >= 0 {
			if _, dup := seek[id]; !dup {
				seek[id] = pos
			}
		}
		return true
	})
}

func (r *Reader) parseInfo(off, end int64) error {
	var dur float64
	var cerr error
	err := r.src.children(off, end, func(c elem) bool {
		switch c.id {
		case idTimecodeScale, idDuration:
		default:
			return true
		}
		b, err := r.src.payload(c, end, 16)
		if err != nil {
			cerr = err
			return false
		}
		if c.id == idTimecodeScale {
			if v := readUint(b); v > 0 && v < 1<<40 {
				r.scale = int64(v)
			}
		} else {
			dur = readFloat(b)
		}
		return true
	})
	if cerr != nil {
		return cerr
	}
	if dur > 0 && dur < 1e15 {
		r.duration = time.Duration(dur * float64(r.scale))
	}
	return err
}

func (r *Reader) parseTracks(off, end int64) error {
	var cerr error
	err := r.src.children(off, end, func(c elem) bool {
		if c.id != idTrackEntry {
			return true
		}
		t, err := r.parseTrackEntry(c.dataOff, c.end(end))
		if err != nil {
			cerr = err
			return false
		}
		if t.Number == 0 || r.byNum[t.Number] != nil {
			return true // invalid or duplicate: ignore
		}
		r.tracks = append(r.tracks, t)
		r.byNum[t.Number] = t
		return true
	})
	if cerr != nil {
		return cerr
	}
	return err
}

func (r *Reader) parseTrackEntry(off, end int64) (*Track, error) {
	t := &Track{Enabled: true, Default: true}
	var codecDelay, preRoll, defDur uint64
	var frameRate float64
	var cerr error
	leaf := func(c elem, max int) []byte {
		b, err := r.src.payload(c, end, max)
		if err != nil {
			cerr = err
		}
		return b
	}
	err := r.src.children(off, end, func(c elem) bool {
		switch c.id {
		case idTrackNumber:
			t.Number = readUint(leaf(c, 8))
		case idTrackUID:
			t.UID = readUint(leaf(c, 8))
		case idTrackType:
			t.Type = readUint(leaf(c, 8))
		case idFlagEnabled:
			t.Enabled = readUint(leaf(c, 8)) != 0
		case idFlagDefault:
			t.Default = readUint(leaf(c, 8)) != 0
		case idDefaultDuration:
			defDur = readUint(leaf(c, 8))
		case idName:
			t.Name = readString(leaf(c, 4096))
		case idLanguage:
			t.Language = readString(leaf(c, 64))
		case idCodecID:
			t.CodecID = readString(leaf(c, 256))
		case idCodecPrivate:
			t.CodecPrivate = leaf(c, maxMeta)
		case idCodecDelay:
			codecDelay = readUint(leaf(c, 8))
		case idSeekPreRoll:
			preRoll = readUint(leaf(c, 8))
		case idVideo:
			cerr = r.src.children(c.dataOff, c.end(end), func(v elem) bool {
				var b []byte
				switch v.id {
				case idPixelWidth, idPixelHeight, idDisplayWidth, idDisplayHeight, idFrameRate:
					b = leaf(v, 8)
				default:
					return true
				}
				switch v.id {
				case idPixelWidth:
					t.Width = clampInt(readUint(b))
				case idPixelHeight:
					t.Height = clampInt(readUint(b))
				case idDisplayWidth:
					t.DisplayWidth = clampInt(readUint(b))
				case idDisplayHeight:
					t.DisplayHeight = clampInt(readUint(b))
				case idFrameRate:
					frameRate = readFloat(b)
				}
				return cerr == nil
			})
		case idAudio:
			cerr = r.src.children(c.dataOff, c.end(end), func(a elem) bool {
				var b []byte
				switch a.id {
				case idSamplingFrequency, idOutputSamplingFrequency, idChannels, idBitDepth:
					b = leaf(a, 8)
				default:
					return true
				}
				switch a.id {
				case idSamplingFrequency:
					t.SampleRate = readFloat(b)
				case idOutputSamplingFrequency:
					t.OutputSampleRate = readFloat(b)
				case idChannels:
					t.Channels = clampInt(readUint(b))
				case idBitDepth:
					t.BitDepth = clampInt(readUint(b))
				}
				return cerr == nil
			})
		case idContentEncodings:
			cerr = r.parseEncodings(t, c.dataOff, c.end(end))
		}
		return cerr == nil
	})
	if cerr != nil {
		return nil, cerr
	}
	if err != nil {
		return nil, err
	}
	switch t.Type {
	case 1:
		t.Kind = KindVideo
		if t.DisplayWidth == 0 {
			t.DisplayWidth = t.Width
		}
		if t.DisplayHeight == 0 {
			t.DisplayHeight = t.Height
		}
	case 2:
		t.Kind = KindAudio
		if t.SampleRate == 0 {
			t.SampleRate = 8000 // the spec's default
		}
		if t.OutputSampleRate == 0 {
			t.OutputSampleRate = t.SampleRate
		}
		if t.Channels == 0 {
			t.Channels = 1
		}
	case 0x11:
		t.Kind = KindSubtitle
	}
	if defDur > 0 && defDur < 1<<40 {
		t.DefaultDuration = time.Duration(defDur)
	}
	if codecDelay < 1<<40 {
		t.CodecDelay = time.Duration(codecDelay)
	}
	if preRoll < 1<<40 {
		t.SeekPreRoll = time.Duration(preRoll)
	}
	if t.Kind == KindVideo {
		if t.DefaultDuration > 0 {
			t.FPS = float64(time.Second) / float64(t.DefaultDuration)
		} else if frameRate > 0 && frameRate < 1e6 {
			t.FPS = frameRate
		}
	}
	t.Codec = codecName(t.CodecID)
	return t, nil
}

func (r *Reader) parseEncodings(t *Track, off, end int64) error {
	return r.src.children(off, end, func(c elem) bool {
		if c.id != idContentEncoding {
			return true
		}
		cend := c.end(end)
		_ = r.src.children(c.dataOff, cend, func(e elem) bool {
			switch e.id {
			case idContentEncryption:
				t.Encrypted = true
			case idContentCompression:
				algo := uint64(0) // zlib is the default
				var settings []byte
				_ = r.src.children(e.dataOff, e.end(cend), func(s elem) bool {
					b, err := r.src.payload(s, cend, 1<<16)
					if err != nil {
						return false
					}
					switch s.id {
					case idContentCompAlgo:
						algo = readUint(b)
					case idContentCompSettings:
						settings = b
					}
					return true
				})
				if algo == 3 {
					t.stripPrefix = settings
				} else {
					t.Compressed = true
				}
			}
			return true
		})
		return true
	})
}

func (r *Reader) parseCues(off, end int64) {
	_ = r.src.children(off, end, func(c elem) bool {
		if c.id != idCuePoint {
			return true
		}
		cend := c.end(end)
		t := int64(-1)
		var tps []cuePoint
		_ = r.src.children(c.dataOff, cend, func(e elem) bool {
			switch e.id {
			case idCueTime:
				b, err := r.src.payload(e, cend, 8)
				if err != nil {
					return false
				}
				t = int64(readUint(b) & (1<<62 - 1))
			case idCueTrackPositions:
				cp := cuePoint{pos: -1}
				_ = r.src.children(e.dataOff, e.end(cend), func(p elem) bool {
					b, err := r.src.payload(p, cend, 8)
					if err != nil {
						return false
					}
					switch p.id {
					case idCueTrack:
						cp.track = readUint(b)
					case idCueClusterPosition:
						cp.pos = int64(readUint(b) & (1<<62 - 1))
					}
					return true
				})
				if cp.pos >= 0 {
					tps = append(tps, cp)
				}
			}
			return true
		})
		if t < 0 || t > (1<<62)/r.scale {
			return true
		}
		for _, cp := range tps {
			cp.t = t * r.scale
			cp.pos += r.segStart
			if cp.pos < r.segEnd {
				r.cues = append(r.cues, cp)
			}
		}
		return true
	})
	sort.SliceStable(r.cues, func(i, j int) bool { return r.cues[i].t < r.cues[j].t })
}

func clampInt(v uint64) int {
	if v > 1<<30 {
		return 0
	}
	return int(v)
}

func codecName(id string) string {
	switch id {
	case "V_AV1":
		return "av1"
	case "V_VP9":
		return "vp9"
	case "V_VP8":
		return "vp8"
	case "V_MPEG4/ISO/AVC":
		return "h264"
	case "V_MPEGH/ISO/HEVC":
		return "hevc"
	case "A_OPUS":
		return "opus"
	case "A_VORBIS":
		return "vorbis"
	case "A_AAC", "A_AAC/MPEG4/LC", "A_AAC/MPEG4/LC/SBR", "A_AAC/MPEG4/MAIN", "A_AAC/MPEG2/LC", "A_AAC/MPEG2/LC/SBR":
		return "aac"
	}
	return ""
}

// Tracks returns the tracks in file order.
func (r *Reader) Tracks() []*Track { return r.tracks }

// TrackByNumber returns the track with the given TrackNumber, or nil.
func (r *Reader) TrackByNumber(n uint64) *Track { return r.byNum[n] }

// Track returns the first track of kind k with a known codec (falling
// back to the first track of that kind), or nil.
func (r *Reader) Track(k Kind) *Track {
	var first *Track
	for _, t := range r.tracks {
		if t.Kind != k {
			continue
		}
		if t.Codec != "" {
			return t
		}
		if first == nil {
			first = t
		}
	}
	return first
}

// VideoTrack is Track(KindVideo).
func (r *Reader) VideoTrack() *Track { return r.Track(KindVideo) }

// AudioTrack is Track(KindAudio).
func (r *Reader) AudioTrack() *Track { return r.Track(KindAudio) }

// TimecodeScale is the segment's tick in nanoseconds.
func (r *Reader) TimecodeScale() int64 { return r.scale }

// HasCues reports whether the file has a usable Cues index.
func (r *Reader) HasCues() bool { return len(r.cues) > 0 }

// Duration is the segment duration from Info, or — when Info has none
// (live recordings) — the end of the last frame once the clusters have
// been indexed (it then indexes the whole file).
func (r *Reader) Duration() time.Duration {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.duration >= 0 {
		return r.duration
	}
	r.indexUntil(func() bool { return false })
	var end int64
	for _, t := range r.tracks {
		if n := len(t.frames); n > 0 {
			f := t.frames[n-1]
			if e := f.ts + max(f.dur, 0); e > end {
				end = e
			}
		}
	}
	return time.Duration(end)
}

// AVCC parses an avcC record (h264 CodecPrivate) into its parameter sets
// and NAL length size.
func AVCC(b []byte) (sps, pps [][]byte, lengthSize int, err error) {
	if len(b) < 7 || b[0] != 1 {
		return nil, nil, 0, fmt.Errorf("webm: bad avcC: %w", ErrInvalid)
	}
	lengthSize = int(b[4]&3) + 1
	p := 6
	read := func(n int) ([][]byte, bool) {
		var out [][]byte
		for i := 0; i < n; i++ {
			if p+2 > len(b) {
				return nil, false
			}
			l := int(b[p])<<8 | int(b[p+1])
			p += 2
			if p+l > len(b) {
				return nil, false
			}
			out = append(out, b[p:p+l])
			p += l
		}
		return out, true
	}
	var ok bool
	if sps, ok = read(int(b[5] & 31)); !ok {
		return nil, nil, 0, fmt.Errorf("webm: truncated avcC: %w", ErrInvalid)
	}
	if p >= len(b) {
		return nil, nil, 0, fmt.Errorf("webm: truncated avcC: %w", ErrInvalid)
	}
	p++
	if pps, ok = read(int(b[p-1])); !ok {
		return nil, nil, 0, fmt.Errorf("webm: truncated avcC: %w", ErrInvalid)
	}
	return sps, pps, lengthSize, nil
}

// HVCC parses an hvcC record (hevc CodecPrivate) into its VPS/SPS/PPS
// NAL units and NAL length size.
func HVCC(b []byte) (vps, sps, pps [][]byte, lengthSize int, err error) {
	if len(b) < 23 {
		return nil, nil, nil, 0, fmt.Errorf("webm: bad hvcC: %w", ErrInvalid)
	}
	lengthSize = int(b[21]&3) + 1
	n := int(b[22])
	p := 23
	for i := 0; i < n; i++ {
		if p+3 > len(b) {
			return nil, nil, nil, 0, fmt.Errorf("webm: truncated hvcC: %w", ErrInvalid)
		}
		typ := b[p] & 0x3F
		cnt := int(b[p+1])<<8 | int(b[p+2])
		p += 3
		for j := 0; j < cnt; j++ {
			if p+2 > len(b) {
				return nil, nil, nil, 0, fmt.Errorf("webm: truncated hvcC: %w", ErrInvalid)
			}
			l := int(b[p])<<8 | int(b[p+1])
			p += 2
			if p+l > len(b) {
				return nil, nil, nil, 0, fmt.Errorf("webm: truncated hvcC: %w", ErrInvalid)
			}
			nal := b[p : p+l]
			p += l
			switch typ {
			case 32:
				vps = append(vps, nal)
			case 33:
				sps = append(sps, nal)
			case 34:
				pps = append(pps, nal)
			}
		}
	}
	return vps, sps, pps, lengthSize, nil
}
