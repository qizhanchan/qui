// Package mp4x is the ISO-BMFF (MP4) container layer for qui's pure-Go
// media backend. It wraps github.com/abema/go-mp4 — which parses boxes
// and assembles the sample table but stops short of the player-shaped
// API — with the pieces a decoder/player actually needs:
//
//   - track enumeration with codec/dimension/rate metadata
//   - a sample timeline (decode timestamps)
//   - sample bytes by index
//   - keyframe (IDR) indexing and keyframe-accurate seek
//   - codec configuration (avcC SPS/PPS, AudioSpecificConfig)
//
// The package is pure Go (no cgo) and has no dependency on root qui, so
// it can back a cross-platform media backend. Progressive (non-fragmented)
// MP4 is supported today; fragmented MP4 (moof/traf) is detected and
// reported via ErrFragmented for a follow-up.
package mp4x

import (
	"errors"
	"fmt"
	"os"
	"time"

	mp4 "github.com/abema/go-mp4"
)

// ErrFragmented is returned when a file uses fragmented MP4 (moof/traf),
// which this version does not yet read.
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

	dts     []time.Duration // decode timestamps, lazily built
	keyfr   []int           // keyframe sample indices (video), lazily built
	keyfrOK bool
}

// SampleCount is the number of samples in the track.
func (t *Track) SampleCount() int {
	if t == nil || t.raw == nil {
		return 0
	}
	return len(t.raw.Samples)
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
	for _, raw := range info.Tracks {
		t := newTrack(raw)
		switch t.Kind {
		case KindVideo:
			if r.video == nil {
				r.video = t
			}
		case KindAudio:
			if r.audio == nil {
				r.audio = t
			}
		}
	}
	return r, nil
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

// Fragmented reports whether the file uses fragmented MP4, which this
// version cannot read yet.
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
	if t == nil || t.raw == nil {
		return nil, ErrNoTrack
	}
	if len(t.raw.Samples) == 0 {
		return nil, ErrFragmented
	}
	off, size, err := sampleOffset(t.raw, i)
	if err != nil {
		return nil, err
	}
	buf := make([]byte, size)
	if _, err := r.f.ReadAt(buf, int64(off)); err != nil {
		return nil, err
	}
	return buf, nil
}

// DTS returns the decode timestamp of sample i within the track. It is
// the running sum of the sample durations from the stts table.
func (r *Reader) DTS(t *Track, i int) time.Duration {
	if t == nil || t.raw == nil {
		return 0
	}
	t.buildDTS()
	if i < 0 || i >= len(t.dts) {
		return 0
	}
	return t.dts[i]
}

// Keyframes returns the sample indices that are sync samples (IDR frames)
// for a video track. Returns nil for non-video tracks.
func (r *Reader) Keyframes(t *Track) []int {
	if t == nil || t.raw == nil || t.Kind != KindVideo {
		return nil
	}
	if t.keyfrOK {
		return t.keyfr
	}
	t.keyfrOK = true
	idxs, err := mp4.FindIDRFrames(r.f, &mp4.TrackInfo{
		AVC:     t.raw.AVC,
		Chunks:  t.raw.Chunks,
		Samples: t.raw.Samples,
	})
	if err != nil {
		return nil
	}
	t.keyfr = idxs
	return idxs
}

// SeekKeyframe returns the sample index of the last keyframe whose decode
// timestamp is <= target. It returns 0 when target precedes the first
// keyframe. The returned index is suitable for feeding a decoder from a
// self-contained point.
func (r *Reader) SeekKeyframe(t *Track, target time.Duration) int {
	ks := r.Keyframes(t)
	if len(ks) == 0 {
		return 0
	}
	best := ks[0]
	for _, k := range ks {
		if r.DTS(t, k) <= target {
			best = k
		} else {
			break
		}
	}
	return best
}

// AVCCParameterSets returns the SPS/PPS NAL units and the NAL length prefix
// size for the video track.
func (r *Reader) AVCCParameterSets() (sps, pps [][]byte, lengthSize int, err error) {
	boxes, err := mp4.ExtractBoxWithPayload(r.f, nil, mp4.BoxPath{
		mp4.BoxTypeMoov(), mp4.BoxTypeTrak(), mp4.BoxTypeMdia(),
		mp4.BoxTypeMinf(), mp4.BoxTypeStbl(), mp4.BoxTypeStsd(),
		mp4.BoxTypeAvc1(), mp4.BoxTypeAvcC(),
	})
	if err != nil {
		return nil, nil, 0, err
	}
	if len(boxes) == 0 {
		return nil, nil, 0, fmt.Errorf("mp4x: avcC not found")
	}
	avcC, ok := boxes[0].Payload.(*mp4.AVCDecoderConfiguration)
	if !ok {
		return nil, nil, 0, fmt.Errorf("mp4x: unexpected avcC payload %T", boxes[0].Payload)
	}
	for _, ps := range avcC.SequenceParameterSets {
		sps = append(sps, ps.NALUnit)
	}
	for _, ps := range avcC.PictureParameterSets {
		pps = append(pps, ps.NALUnit)
	}
	return sps, pps, int(avcC.LengthSizeMinusOne) + 1, nil
}

// AudioSpecificConfig returns the AAC AudioSpecificConfig bytes for the
// audio track.
func (r *Reader) AudioSpecificConfig() ([]byte, error) {
	boxes, err := mp4.ExtractBoxWithPayload(r.f, nil, mp4.BoxPath{
		mp4.BoxTypeMoov(), mp4.BoxTypeTrak(), mp4.BoxTypeMdia(),
		mp4.BoxTypeMinf(), mp4.BoxTypeStbl(), mp4.BoxTypeStsd(),
		mp4.BoxTypeMp4a(), mp4.BoxTypeEsds(),
	})
	if err != nil {
		return nil, err
	}
	if len(boxes) == 0 {
		return nil, fmt.Errorf("mp4x: esds not found")
	}
	esds, ok := boxes[0].Payload.(*mp4.Esds)
	if !ok {
		return nil, fmt.Errorf("mp4x: unexpected esds payload %T", boxes[0].Payload)
	}
	for _, d := range esds.Descriptors {
		if d.Tag == mp4.DecSpecificInfoTag {
			return d.Data, nil
		}
	}
	return nil, fmt.Errorf("mp4x: DecoderSpecificInfo not found")
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

// buildDTS lazily accumulates decode timestamps from the stts deltas.
func (t *Track) buildDTS() {
	if t.dts != nil {
		return
	}
	t.dts = make([]time.Duration, len(t.raw.Samples))
	if t.Timescale == 0 {
		return
	}
	var acc uint64
	for i, s := range t.raw.Samples {
		t.dts[i] = time.Duration(float64(acc) / float64(t.Timescale) * float64(time.Second))
		acc += uint64(s.TimeDelta)
	}
}

// sampleOffset walks the chunk table (stsc + stco/co64 + stsz) to find the
// absolute file offset and size of sample idx.
func sampleOffset(t *mp4.Track, idx int) (uint64, uint32, error) {
	if idx < 0 || idx >= len(t.Samples) {
		return 0, 0, fmt.Errorf("mp4x: sample %d out of range (0..%d)", idx, len(t.Samples)-1)
	}
	si := 0
	for _, ch := range t.Chunks {
		spc := int(ch.SamplesPerChunk)
		if spc == 0 {
			continue
		}
		if idx < si+spc {
			off := ch.DataOffset
			for k := si; k < idx; k++ {
				off += uint64(t.Samples[k].Size)
			}
			return off, t.Samples[idx].Size, nil
		}
		si += spc
	}
	return 0, 0, fmt.Errorf("mp4x: sample %d not covered by any chunk", idx)
}
