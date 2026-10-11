// Package mp4x is the ISO-BMFF (MP4) container layer for qui's pure-Go
// media backend. It wraps github.com/abema/go-mp4 — which parses boxes
// and assembles the sample table but stops short of the player-shaped
// API — with the pieces a decoder/player actually needs:
//
//   - track enumeration with codec/dimension/rate metadata
//   - a per-sample table (file offset, size, decode and presentation time,
//     sync flag) built once at open
//   - sample bytes by index
//   - keyframe indexing (stss) and keyframe-accurate seek
//   - codec configuration (avcC SPS/PPS, AudioSpecificConfig)
//
// Presentation timestamps apply the composition offsets (ctts / trun) and
// the first edit-list entry, so they start at (about) zero for files whose
// encoder delays the first frame (B-frame reordering, AAC priming).
//
// The package is pure Go (no cgo) and has no dependency on root qui, so
// it can back a cross-platform media backend. Both progressive and
// fragmented MP4 are supported: for a fragmented file Open walks the
// moof/traf/trun boxes and assembles the same per-sample table a
// progressive file gets from stbl, so the read API is identical.
package mp4x

import (
	"errors"
	"fmt"
	"os"
	"sort"
	"time"

	mp4 "github.com/abema/go-mp4"
)

// ErrFragmented is kept for API compatibility; fragmented files are read.
var ErrFragmented = errors.New("mp4x: fragmented MP4 not supported yet")

// ErrNoTrack is returned when the requested track kind is absent.
var ErrNoTrack = errors.New("mp4x: track not present")

// Kind classifies a track.
type Kind int

const (
	KindUnknown Kind = iota
	KindVideo
	KindAudio
)

// sample is one entry of a track's sample table.
type sample struct {
	off  uint64 // absolute file offset
	size uint32
	dur  uint32 // duration, track timescale units
	dts  uint64 // decode time, track timescale units
	cto  int64  // composition time offset, track timescale units
	sync bool
}

// fragSample is the fragment-table entry (same layout as sample).
type fragSample = sample

// Track describes one media track and exposes its sample timeline.
type Track struct {
	ID        uint32
	Kind      Kind
	Codec     string // "h264" | "aac"
	Timescale uint32
	Duration  time.Duration

	// Video.
	Width  int
	Height int
	FPS    float32

	// Audio.
	SampleRate int
	Channels   int

	raw *mp4.Track

	// samples is the full table (progressive: from stbl; fragmented: from
	// the movie fragments). frag aliases it while fragments are appended.
	samples []sample
	frag    []fragSample

	// shift is subtracted from dts+cto to get the presentation time (the
	// first edit's media_time, minus any leading empty edit).
	shift int64

	stss    []uint32 // 1-based sync sample numbers; nil = all sync
	hasStss bool

	keyfr   []int
	keyfrOK bool

	avcC *mp4.AVCDecoderConfiguration
	asc  []byte
}

// SampleCount is the number of samples in the track.
func (t *Track) SampleCount() int {
	if t == nil {
		return 0
	}
	return len(t.samples)
}

// Reader is an open MP4 file.
type Reader struct {
	f    *os.File
	info *mp4.ProbeInfo

	video *Track
	audio *Track
}

// Open parses path and enumerates its tracks.
func Open(path string) (*Reader, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	info, err := mp4.Probe(f)
	if err != nil {
		_ = f.Close()
		return nil, fmt.Errorf("mp4x: probe %s: %w", path, err)
	}
	r := &Reader{f: f, info: info}
	byID := map[uint32]*Track{}
	for _, raw := range info.Tracks {
		t := newTrack(raw)
		byID[t.ID] = t
		switch t.Kind {
		case KindVideo:
			if r.video == nil && !raw.Encrypted {
				r.video = t
			}
		case KindAudio:
			if r.audio == nil && !raw.Encrypted {
				r.audio = t
			}
		}
	}
	if err := r.readTrakExtras(byID, info.Timescale); err != nil {
		_ = f.Close()
		return nil, err
	}
	for _, t := range []*Track{r.video, r.audio} {
		if t != nil && len(t.raw.Samples) > 0 {
			if err := t.buildProgressive(); err != nil {
				_ = f.Close()
				return nil, err
			}
		}
	}
	if len(info.Segments) > 0 {
		if err := r.buildFragments(); err != nil {
			_ = f.Close()
			return nil, err
		}
		for _, t := range []*Track{r.video, r.audio} {
			if t != nil && len(t.samples) == 0 && len(t.frag) > 0 {
				t.samples = t.frag
			}
		}
	}
	return r, nil
}

// readTrakExtras reads per-trak boxes go-mp4's probe does not expose: the
// sync sample table and the codec configuration.
func (r *Reader) readTrakExtras(byID map[uint32]*Track, movieTimescale uint32) error {
	traks, err := mp4.ExtractBox(r.f, nil, mp4.BoxPath{mp4.BoxTypeMoov(), mp4.BoxTypeTrak()})
	if err != nil {
		return err
	}
	for _, trak := range traks {
		bips, err := mp4.ExtractBoxesWithPayload(r.f, trak, []mp4.BoxPath{
			{mp4.BoxTypeTkhd()},
			{mp4.BoxTypeMdia(), mp4.BoxTypeMinf(), mp4.BoxTypeStbl(), mp4.BoxTypeStss()},
			{mp4.BoxTypeMdia(), mp4.BoxTypeMinf(), mp4.BoxTypeStbl(), mp4.BoxTypeStsd(), mp4.BoxTypeAvc1(), mp4.BoxTypeAvcC()},
			{mp4.BoxTypeMdia(), mp4.BoxTypeMinf(), mp4.BoxTypeStbl(), mp4.BoxTypeStsd(), mp4.BoxTypeMp4a(), mp4.BoxTypeEsds()},
		})
		if err != nil {
			return err
		}
		var t *Track
		for _, bip := range bips {
			switch b := bip.Payload.(type) {
			case *mp4.Tkhd:
				t = byID[b.TrackID]
			case *mp4.Stss:
				if t != nil {
					t.hasStss = true
					t.stss = b.SampleNumber
				}
			case *mp4.AVCDecoderConfiguration:
				if t != nil && t.avcC == nil {
					t.avcC = b
				}
			case *mp4.Esds:
				if t != nil && t.asc == nil {
					for _, d := range b.Descriptors {
						if d.Tag == mp4.DecSpecificInfoTag {
							t.asc = d.Data
						}
					}
				}
			}
		}
		if t == nil {
			continue
		}
		// Edit list: a leading empty edit delays the track; the first
		// non-empty edit's media_time is where presentation starts.
		var empty uint64
		for _, e := range t.raw.EditList {
			if e.MediaTime == -1 {
				empty += e.SegmentDuration
				continue
			}
			t.shift = e.MediaTime
			break
		}
		if empty > 0 && movieTimescale > 0 {
			t.shift -= int64(empty * uint64(t.Timescale) / uint64(movieTimescale))
		}
	}
	return nil
}

// buildProgressive builds the sample table from stbl (chunks, sizes,
// stts deltas, ctts offsets) and the sync table.
func (t *Track) buildProgressive() error {
	raw := t.raw
	t.samples = make([]sample, len(raw.Samples))
	i := 0
	for _, ch := range raw.Chunks {
		off := ch.DataOffset
		for k := uint32(0); k < ch.SamplesPerChunk && i < len(raw.Samples); k++ {
			t.samples[i].off = off
			off += uint64(raw.Samples[i].Size)
			i++
		}
	}
	if i < len(raw.Samples) {
		return fmt.Errorf("mp4x: chunk table covers %d of %d samples", i, len(raw.Samples))
	}
	var dts uint64
	for k, s := range raw.Samples {
		e := &t.samples[k]
		e.size = s.Size
		e.dur = s.TimeDelta
		e.dts = dts
		e.cto = s.CompositionTimeOffset
		e.sync = !t.hasStss
		dts += uint64(s.TimeDelta)
	}
	for _, n := range t.stss {
		if n >= 1 && int(n) <= len(t.samples) {
			t.samples[n-1].sync = true
		}
	}
	return nil
}

// Close releases the file.
func (r *Reader) Close() error {
	if r == nil || r.f == nil {
		return nil
	}
	err := r.f.Close()
	r.f = nil
	return err
}

// Fragmented reports whether the file uses fragmented MP4 (moof/traf). Such
// files are fully readable: Open assembles the sample table from the movie
// fragments.
func (r *Reader) Fragmented() bool {
	for _, t := range r.info.Tracks {
		if len(t.Samples) == 0 {
			return true
		}
	}
	return len(r.info.Segments) > 0
}

// VideoTrack returns the first video track, or nil.
func (r *Reader) VideoTrack() *Track { return r.video }

// AudioTrack returns the first audio track, or nil.
func (r *Reader) AudioTrack() *Track { return r.audio }

// Track returns the first track of the given kind, or nil.
func (r *Reader) Track(k Kind) *Track {
	switch k {
	case KindVideo:
		return r.video
	case KindAudio:
		return r.audio
	default:
		return nil
	}
}

// ReadSample returns the encoded bytes of sample i of t.
func (r *Reader) ReadSample(t *Track, i int) ([]byte, error) {
	return r.ReadSampleInto(nil, t, i)
}

// ReadSampleInto is ReadSample reusing buf's storage when it is large
// enough.
func (r *Reader) ReadSampleInto(buf []byte, t *Track, i int) ([]byte, error) {
	if t == nil {
		return nil, ErrNoTrack
	}
	if i < 0 || i >= len(t.samples) {
		return nil, fmt.Errorf("mp4x: sample %d out of range (0..%d)", i, len(t.samples)-1)
	}
	s := &t.samples[i]
	if cap(buf) < int(s.size) {
		buf = make([]byte, s.size)
	}
	buf = buf[:s.size]
	if _, err := r.f.ReadAt(buf, int64(s.off)); err != nil {
		return nil, err
	}
	return buf, nil
}

func (t *Track) toDuration(v int64) time.Duration {
	if t.Timescale == 0 {
		return 0
	}
	return time.Duration(float64(v) / float64(t.Timescale) * float64(time.Second))
}

// DTS returns the decode timestamp of sample i within the track.
func (r *Reader) DTS(t *Track, i int) time.Duration {
	if t == nil || i < 0 || i >= len(t.samples) {
		return 0
	}
	return t.toDuration(int64(t.samples[i].dts))
}

// PTS returns the presentation timestamp of sample i (composition offset
// and edit list applied). It can be negative for samples that precede the
// presentation start (e.g. AAC priming).
func (r *Reader) PTS(t *Track, i int) time.Duration {
	if t == nil || i < 0 || i >= len(t.samples) {
		return 0
	}
	s := &t.samples[i]
	return t.toDuration(int64(s.dts) + s.cto - t.shift)
}

// SampleDuration returns the duration of sample i.
func (r *Reader) SampleDuration(t *Track, i int) time.Duration {
	if t == nil || i < 0 || i >= len(t.samples) {
		return 0
	}
	return t.toDuration(int64(t.samples[i].dur))
}

// IsSync reports whether sample i is a sync sample (keyframe).
func (r *Reader) IsSync(t *Track, i int) bool {
	if t == nil || i < 0 || i >= len(t.samples) {
		return false
	}
	return t.samples[i].sync
}

// Keyframes returns the sample indices that are sync samples for a video
// track. Returns nil for non-video tracks.
func (r *Reader) Keyframes(t *Track) []int {
	if t == nil || t.Kind != KindVideo {
		return nil
	}
	if t.keyfrOK {
		return t.keyfr
	}
	t.keyfrOK = true
	if !t.hasStss && len(t.frag) == 0 && t.avcC != nil {
		// No stss means "all samples are sync" — but some muxers omit it
		// for ordinary IDR/P streams, so classify by NAL unit type.
		ls := int(t.avcC.LengthSizeMinusOne) + 1
		var buf []byte
		for i := range t.samples {
			b, err := r.ReadSampleInto(buf, t, i)
			if err != nil {
				break
			}
			buf = b
			t.samples[i].sync = hasIDR(b, ls)
		}
	}
	for i := range t.samples {
		if t.samples[i].sync {
			t.keyfr = append(t.keyfr, i)
		}
	}
	return t.keyfr
}

// SeekKeyframe returns the index of the last keyframe whose presentation
// time is <= target (0 when target precedes the first keyframe). Decoding
// from that sample in decode order reaches target.
func (r *Reader) SeekKeyframe(t *Track, target time.Duration) int {
	ks := r.Keyframes(t)
	if len(ks) == 0 {
		return 0
	}
	j := sort.Search(len(ks), func(j int) bool { return r.PTS(t, ks[j]) > target })
	if j == 0 {
		return ks[0]
	}
	return ks[j-1]
}

// SampleAtTime returns the index of the first sample (decode order) whose
// presentation time is >= target, for tracks without reordering (audio).
func (r *Reader) SampleAtTime(t *Track, target time.Duration) int {
	n := len(t.samples)
	return sort.Search(n, func(i int) bool { return r.PTS(t, i)+r.SampleDuration(t, i) > target })
}

// AVCCParameterSets returns the SPS/PPS NAL units and the NAL length prefix
// size for the video track.
func (r *Reader) AVCCParameterSets() (sps, pps [][]byte, lengthSize int, err error) {
	t := r.video
	if t == nil || t.avcC == nil {
		return nil, nil, 0, fmt.Errorf("mp4x: avcC not found")
	}
	for _, ps := range t.avcC.SequenceParameterSets {
		sps = append(sps, ps.NALUnit)
	}
	for _, ps := range t.avcC.PictureParameterSets {
		pps = append(pps, ps.NALUnit)
	}
	return sps, pps, int(t.avcC.LengthSizeMinusOne) + 1, nil
}

// AudioSpecificConfig returns the AAC AudioSpecificConfig bytes for the
// audio track.
func (r *Reader) AudioSpecificConfig() ([]byte, error) {
	if r.audio == nil || r.audio.asc == nil {
		return nil, fmt.Errorf("mp4x: DecoderSpecificInfo not found")
	}
	return r.audio.asc, nil
}

// newTrack converts go-mp4's Track into our Track.
func newTrack(raw *mp4.Track) *Track {
	t := &Track{ID: raw.TrackID, Timescale: raw.Timescale, raw: raw}
	if raw.Timescale > 0 {
		t.Duration = time.Duration(float64(raw.Duration) / float64(raw.Timescale) * float64(time.Second))
	}
	switch raw.Codec {
	case mp4.CodecAVC1:
		t.Kind = KindVideo
		t.Codec = "h264"
		if raw.AVC != nil {
			t.Width = int(raw.AVC.Width)
			t.Height = int(raw.AVC.Height)
		}
		if n := len(raw.Samples); n > 0 && t.Duration > 0 {
			t.FPS = float32(float64(n) / t.Duration.Seconds())
		}
	case mp4.CodecMP4A:
		t.Kind = KindAudio
		t.Codec = "aac"
		t.SampleRate = int(raw.Timescale)
		if raw.MP4A != nil {
			t.Channels = int(raw.MP4A.ChannelCount)
		}
	}
	return t
}

// hasIDR reports whether a length-prefixed AVC sample contains an IDR slice.
func hasIDR(b []byte, ls int) bool {
	for len(b) >= ls {
		n := 0
		for k := 0; k < ls; k++ {
			n = n<<8 | int(b[k])
		}
		b = b[ls:]
		if n <= 0 || n > len(b) {
			return false
		}
		if b[0]&0x1f == 5 {
			return true
		}
		b = b[n:]
	}
	return false
}
