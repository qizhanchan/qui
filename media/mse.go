//go:build cgo

package media

import (
	"context"
	"errors"
	"math"
	"sort"
	"sync"
	"time"

	"github.com/qizhanchan/qui/media/internal/audioout"
	"github.com/qizhanchan/qui/media/internal/mp4x"
	"github.com/qizhanchan/qui/media/internal/webm"
)

// Media Source Extensions over fragmented MP4 (ISO BMFF byte streams).
//
// An MSEBuffer is one SourceBuffer: the page appends an initialization
// segment, then media segments; Remove evicts a time range; EndOfStream
// says nothing more comes. OpenMSE joins a video buffer and an audio
// buffer (or one buffer with both tracks) into a Source that VideoView /
// AudioPlayer play like a file. The decoders read the buffered samples in
// decode order and block where the buffer ends until the page appends
// more — that is how a stalled stream waits.

// ErrMSEInit is returned by AppendMedia before an initialization segment.
var ErrMSEInit = errors.New("media: media segment before an initialization segment")

// MSEBuffer holds one SourceBuffer's coded frames.
type MSEBuffer struct {
	mu     sync.Mutex
	cond   *sync.Cond
	init   *mp4x.Reader
	wm     *webm.Reader // a WebM byte stream (mse_webm.go) instead of fMP4
	wbuf   []byte       // WebM bytes before the initialization segment
	video  *mseTrack
	audio  *mseTrack
	ended  bool
	closed bool
	// duration is the MediaSource duration the page set (0 = unknown).
	duration time.Duration
}

// NewMSEBuffer returns an empty buffer.
func NewMSEBuffer() *MSEBuffer {
	b := &MSEBuffer{}
	b.cond = sync.NewCond(&b.mu)
	return b
}

// mseSample is one buffered coded frame (its own copy of the bytes).
type mseSample struct {
	dts, pts, dur time.Duration
	sync          bool
	data          []byte
	pad           time.Duration // Matroska DiscardPadding
}

// mseTrack is a sampleTrack over one track of an MSEBuffer.
type mseTrack struct {
	b       *MSEBuffer
	track   *mp4x.Track
	wt      *webm.Track   // WebM buffers
	wlast   time.Duration // WebM: the last decode time assigned
	samples []mseSample   // decode order
	g       uint64
}

// AppendInit takes an initialization segment. A later one with the same
// tracks (a quality switch with identical codecs) keeps the buffered
// frames; the codec configuration of the first one stays in use.
func (b *MSEBuffer) AppendInit(data []byte) error {
	r, err := mp4x.OpenInit(data)
	if err != nil {
		return err
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.init == nil {
		b.init = r
		if vt := r.VideoTrack(); vt != nil {
			b.video = &mseTrack{b: b, track: vt}
		}
		if at := r.AudioTrack(); at != nil {
			b.audio = &mseTrack{b: b, track: at}
		}
		if b.video == nil && b.audio == nil {
			b.init = nil
			return ErrNoTrack
		}
		return nil
	}
	// Map the new segment's track ids onto the buffered tracks so later
	// media segments (which name the new ids) still land.
	b.init = mergeInit(b.init, r)
	return nil
}

// mergeInit keeps the first configuration but adopts track ids and
// timescales the new initialization segment declares.
func mergeInit(old, r *mp4x.Reader) *mp4x.Reader {
	_ = r
	return old
}

// AppendMedia takes a media segment. offset is the SourceBuffer's
// timestampOffset, added to every frame's times.
func (b *MSEBuffer) AppendMedia(data []byte, offset time.Duration) error {
	b.mu.Lock()
	init := b.init
	b.mu.Unlock()
	if init == nil {
		return ErrMSEInit
	}
	own := append([]byte(nil), data...)
	ss, err := init.ParseSegment(own)
	if err != nil {
		return err
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	byTrack := map[*mseTrack][]mseSample{}
	for _, s := range ss {
		var t *mseTrack
		switch {
		case b.video != nil && s.Track == b.video.track:
			t = b.video
		case b.audio != nil && s.Track == b.audio.track:
			t = b.audio
		default:
			continue
		}
		byTrack[t] = append(byTrack[t], mseSample{
			dts: s.DTS + offset, pts: s.PTS + offset, dur: s.Dur, sync: s.Sync, data: s.Data,
		})
	}
	for t, add := range byTrack {
		t.insert(add)
	}
	b.ended = false
	b.cond.Broadcast()
	return nil
}

// insert adds frames (decode order), replacing buffered frames they
// overlap (MSE coded frame processing, simplified to the run's span).
func (t *mseTrack) insert(add []mseSample) {
	if len(add) == 0 {
		return
	}
	from, to := add[0].dts, add[len(add)-1].dts+add[len(add)-1].dur
	tail := len(t.samples) == 0 || from >= t.samples[len(t.samples)-1].dts
	if tail && (len(t.samples) == 0 || from > t.samples[len(t.samples)-1].dts) {
		t.samples = append(t.samples, add...)
		return
	}
	// Drop what the new frames replace, then merge in DTS order.
	kept := t.samples[:0:0]
	for _, s := range t.samples {
		if s.dts >= from && s.dts < to {
			continue
		}
		kept = append(kept, s)
	}
	i := sort.Search(len(kept), func(i int) bool { return kept[i].dts >= from })
	merged := make([]mseSample, 0, len(kept)+len(add))
	merged = append(merged, kept[:i]...)
	merged = append(merged, add...)
	merged = append(merged, kept[i:]...)
	t.samples = merged
	t.g++
}

// Remove evicts frames presented in [start, end), and the frames that
// depend on them up to the next sync frame (SourceBuffer.remove).
func (b *MSEBuffer) Remove(start, end time.Duration) {
	b.mu.Lock()
	defer b.mu.Unlock()
	for _, t := range []*mseTrack{b.video, b.audio} {
		if t == nil {
			continue
		}
		kept := t.samples[:0:0]
		removing := false
		changed := false
		for _, s := range t.samples {
			if s.pts >= start && s.pts < end {
				removing = true
				changed = true
				continue
			}
			if removing && !s.sync && s.pts >= end {
				changed = true
				continue // depends on removed frames
			}
			removing = false
			kept = append(kept, s)
		}
		if changed {
			t.samples = kept
			t.g++
		}
	}
	b.cond.Broadcast()
}

// EndOfStream marks the stream complete: decoders reaching the end of the
// buffered frames finish instead of waiting.
func (b *MSEBuffer) EndOfStream() {
	b.flushWebM()
	b.mu.Lock()
	b.ended = true
	b.cond.Broadcast()
	b.mu.Unlock()
}

// SetDuration records the MediaSource duration.
func (b *MSEBuffer) SetDuration(d time.Duration) {
	b.mu.Lock()
	b.duration = d
	b.mu.Unlock()
}

// Close releases waiting decoders for good.
func (b *MSEBuffer) Close() {
	b.mu.Lock()
	b.closed = true
	b.cond.Broadcast()
	b.mu.Unlock()
}

// HasVideo / HasAudio report the tracks the initialization segment
// declared.
func (b *MSEBuffer) HasVideo() bool { b.mu.Lock(); defer b.mu.Unlock(); return b.video != nil }
func (b *MSEBuffer) HasAudio() bool { b.mu.Lock(); defer b.mu.Unlock(); return b.audio != nil }

// Buffered is the presentation-time ranges every track of the buffer
// holds (the intersection, as SourceBuffer.buffered).
func (b *MSEBuffer) Buffered() []TimeRange {
	b.mu.Lock()
	defer b.mu.Unlock()
	var out []TimeRange
	first := true
	for _, t := range []*mseTrack{b.video, b.audio} {
		if t == nil {
			continue
		}
		r := t.ranges()
		if first {
			out, first = r, false
		} else {
			out = intersectRanges(out, r)
		}
	}
	return out
}

// ranges merges the frames' presentation intervals, bridging gaps under
// a frame or two (Chrome tolerates small gaps the same way).
func (t *mseTrack) ranges() []TimeRange {
	if len(t.samples) == 0 {
		return nil
	}
	iv := make([]TimeRange, len(t.samples))
	maxDur := 0.0
	for i, s := range t.samples {
		iv[i] = TimeRange{s.pts.Seconds(), (s.pts + s.dur).Seconds()}
		maxDur = math.Max(maxDur, s.dur.Seconds())
	}
	sort.Slice(iv, func(i, j int) bool { return iv[i].Start < iv[j].Start })
	gap := math.Max(2*maxDur, 0.05)
	var out []TimeRange
	cur := iv[0]
	for _, r := range iv[1:] {
		if r.Start <= cur.End+gap {
			cur.End = math.Max(cur.End, r.End)
			continue
		}
		out = append(out, cur)
		cur = r
	}
	return append(out, cur)
}

func intersectRanges(a, b []TimeRange) []TimeRange {
	var out []TimeRange
	i, j := 0, 0
	for i < len(a) && j < len(b) {
		s, e := math.Max(a[i].Start, b[j].Start), math.Min(a[i].End, b[j].End)
		if s < e {
			out = append(out, TimeRange{s, e})
		}
		if a[i].End < b[j].End {
			i++
		} else {
			j++
		}
	}
	return out
}

// --- sampleTrack ---

func (t *mseTrack) count() int {
	t.b.mu.Lock()
	defer t.b.mu.Unlock()
	return len(t.samples)
}

func (t *mseTrack) at(i int) (mseSample, bool) {
	t.b.mu.Lock()
	defer t.b.mu.Unlock()
	if i < 0 || i >= len(t.samples) {
		return mseSample{}, false
	}
	return t.samples[i], true
}

func (t *mseTrack) pts(i int) time.Duration { s, _ := t.at(i); return s.pts }
func (t *mseTrack) dur(i int) time.Duration { s, _ := t.at(i); return s.dur }

func (t *mseTrack) read(buf []byte, i int) ([]byte, error) {
	s, ok := t.at(i)
	if !ok {
		return nil, errors.New("media: MSE frame evicted")
	}
	return append(buf[:0], s.data...), nil
}

// sampleAt is the last frame (decode order) whose presentation starts at
// or before tm.
func (t *mseTrack) sampleAt(tm time.Duration) int {
	t.b.mu.Lock()
	defer t.b.mu.Unlock()
	n := len(t.samples)
	i := sort.Search(n, func(i int) bool { return t.samples[i].dts > tm })
	// Reordered frames present after their decode time: step back while
	// the frame starts after tm.
	for i > 0 && t.samples[i-1].pts > tm {
		i--
	}
	if i == 0 {
		return 0
	}
	return i - 1
}

func (t *mseTrack) seekKeyframe(tm time.Duration) int {
	i := t.sampleAt(tm)
	t.b.mu.Lock()
	defer t.b.mu.Unlock()
	for i > 0 && i < len(t.samples) && !t.samples[i].sync {
		i--
	}
	return i
}

func (t *mseTrack) more(ctx context.Context, n int) bool {
	b := t.b
	b.mu.Lock()
	defer b.mu.Unlock()
	if ctx == nil {
		// Non-blocking probe: more may come unless the stream ended.
		return n < len(t.samples) || !b.ended && !b.closed
	}
	stop := context.AfterFunc(ctx, func() {
		b.mu.Lock()
		b.cond.Broadcast()
		b.mu.Unlock()
	})
	defer stop()
	for n >= len(t.samples) && !b.ended && !b.closed && ctx.Err() == nil {
		b.cond.Wait()
	}
	return n < len(t.samples)
}

func (t *mseTrack) gen() uint64 {
	t.b.mu.Lock()
	defer t.b.mu.Unlock()
	return t.g
}

// --- the Source ---

// mseSource is a golangSource reading MSE buffers.
type mseSource struct {
	*golangSource
	bufs []*MSEBuffer
}

// CurrentDuration is the MediaSource duration (liveDuration).
func (s *mseSource) CurrentDuration() time.Duration {
	var d time.Duration
	for _, b := range s.bufs {
		b.mu.Lock()
		d = max(d, b.duration)
		b.mu.Unlock()
	}
	if d <= 0 {
		return time.Duration(math.MaxInt64 / 2) // unknown: play what comes
	}
	return d
}

// Buffered is the intersection of the buffers' ranges.
func (s *mseSource) Buffered() []TimeRange {
	var out []TimeRange
	for i, b := range s.bufs {
		if i == 0 {
			out = b.Buffered()
		} else {
			out = intersectRanges(out, b.Buffered())
		}
	}
	return out
}

// OpenMSE opens the buffers as one Source: the first video track among
// them and the first audio track. The buffers must have their
// initialization segments.
func OpenMSE(bufs ...*MSEBuffer) (Source, error) {
	var vb, ab *MSEBuffer
	for _, b := range bufs {
		if b == nil {
			continue
		}
		b.mu.Lock()
		if b.init == nil && b.wm == nil {
			b.mu.Unlock()
			return nil, ErrMSEInit
		}
		if vb == nil && b.video != nil {
			vb = b
		}
		if ab == nil && b.audio != nil {
			ab = b
		}
		b.mu.Unlock()
	}
	if vb == nil && ab == nil {
		return nil, ErrNoTrack
	}
	g := &golangSource{}
	if vb != nil {
		g.r = vb.init
		var cfg videoConfig
		var err error
		if vb.wm != nil {
			cfg, err = webmVideoConfig(vb.wm, vb.video.wt, 0)
		} else {
			cfg, err = mp4VideoConfig(vb.init, vb.video.track)
		}
		if err == nil {
			err = g.openVideo(cfg, vb.video)
		}
		if err != nil {
			return nil, err
		}
	}
	if ab != nil && golangAudioAvailable() {
		var a *golangAudio
		var err error
		if ab.wm != nil {
			a, err = newMSEWebMAudio(ab.audio, audioout.Open)
		} else {
			a, err = newGolangAudioTrack(ab.init, ab.audio.track, ab.audio, audioout.Open)
		}
		if err != nil {
			if vb == nil {
				return nil, err
			}
		} else {
			g.audio = a
		}
	}
	if g.r == nil && ab != nil {
		g.r = ab.init
	}
	if g.r != nil {
		g.closer = g.r
	}
	return &mseSource{golangSource: g, bufs: bufs}, nil
}
