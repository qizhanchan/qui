//go:build cgo

package media

import (
	"context"
	"errors"
	"io"
	"sync"
	"time"

	"github.com/qizhanchan/qui/media/internal/av1"
	"github.com/qizhanchan/qui/media/internal/h264"
	"github.com/qizhanchan/qui/media/internal/hevc"
	"github.com/qizhanchan/qui/media/internal/mp4x"
)

// The pure-Go pipeline backend: MP4 demux (internal/mp4x) + the self-written
// H.264, HEVC and AV1 decoders (internal/h264, internal/hevc,
// internal/av1) + GPU YUV→RGB
// (gl_yuv.go), with no ffmpeg
// and no platform framework. Audio (AAC-LC) is decoded by internal/aac and
// played through internal/audioout when available.
//
// It registers itself as "golang" and is selected by QUI_MEDIA_BACKEND=golang
// (or by "auto" when no native backend is registered).
func init() { setBackend("golang", golangBackend{}) }

type golangBackend struct{}

func (golangBackend) Capabilities() Capabilities {
	c := Capabilities{
		VideoCodecs: []string{"h264", "hevc", "av1"},
		Containers:  []string{"mp4", "mov", "m4v", "m4a"},
	}
	if golangAudioAvailable() {
		c.AudioCodecs = []string{"aac"}
	}
	return c
}

func (golangBackend) OpenSource(path string) (Source, error) {
	r, err := mp4x.Open(path)
	if err != nil {
		return nil, err
	}
	s := &golangSource{r: r}
	if vt := r.VideoTrack(); vt != nil && (vt.Codec == "h264" || vt.Codec == "hevc" || vt.Codec == "av1") {
		if err := s.openVideo(vt); err != nil {
			_ = r.Close()
			return nil, err
		}
	}
	if at := r.AudioTrack(); at != nil && at.Codec == "aac" && golangAudioAvailable() {
		if a, err := newGolangAudio(r, at); err == nil {
			s.audio = a
		}
	}
	if s.video == nil && s.audio == nil {
		_ = r.Close()
		return nil, ErrCodecUnsupported
	}
	return s, nil
}

// golangSource is an open MP4 file.
type golangSource struct {
	r *mp4x.Reader

	vt        *mp4x.Track
	video     *VideoTrackInfo
	hevc      bool     // HEVC rather than H.264
	av1       bool     // AV1: samples are temporal units, not NAL units
	params    [][]byte // parameter sets (AV1: configOBUs) fed before the first sample
	lenSize   int
	audio     *golangAudio
	scrub     *golangScrub
	mu        sync.Mutex // guards play, ahead and closed
	play      *videoDecoder
	ahead     *decodeAhead
	closed    bool
	closeOnce sync.Once
}

func (s *golangSource) openVideo(vt *mp4x.Track) error {
	if vt.SampleCount() == 0 {
		return ErrCodecUnsupported
	}
	var w, h int
	var fpsVUI float64
	switch vt.Codec {
	case "av1":
		cfg, err := s.r.AV1ConfigOBUs()
		if err != nil {
			return err
		}
		// SequenceHeaderOf rejects what the decoder cannot handle
		// (4:2:2 / 4:4:4, 12-bit); 10-bit output is rounded to 8 bits.
		seq, err := av1.SequenceHeaderOf(cfg)
		if err != nil {
			return ErrCodecUnsupported
		}
		w, h = seq.MaxWidth, seq.MaxHeight
		fpsVUI = seq.FPS()
		s.av1 = true
		s.params = [][]byte{cfg}
	case "hevc":
		vps, sps, pps, ls, err := s.r.HVCCParameterSets()
		if err != nil {
			return err
		}
		if len(vps) == 0 || len(sps) == 0 || len(pps) == 0 || ls < 1 || ls > 4 {
			return ErrCodecUnsupported
		}
		// ParseSPS rejects what the decoder cannot handle (Main 10,
		// 4:2:2 / 4:4:4).
		p, err := hevc.ParseSPS(sps[0])
		if err != nil {
			return ErrCodecUnsupported
		}
		w, h = p.DisplaySize()
		fpsVUI = p.FPS()
		s.hevc = true
		s.params = append(append(append([][]byte{}, vps...), sps...), pps...)
		s.lenSize = ls
	default:
		sps, pps, ls, err := s.r.AVCCParameterSets()
		if err != nil {
			return err
		}
		if len(sps) == 0 || len(pps) == 0 || ls < 1 || ls > 4 {
			return ErrCodecUnsupported
		}
		p, err := h264.ParseSPS(sps[0])
		if err != nil {
			return ErrCodecUnsupported
		}
		if !p.FrameMbsOnly || p.ChromaFormatIDC != 1 || p.BitDepthLuma != 8 || p.BitDepthChroma != 8 {
			// Interlaced / 4:2:2 / 4:4:4 / >8-bit streams are not decoded.
			return ErrCodecUnsupported
		}
		w, h = p.Width, p.Height
		fpsVUI = p.FPS()
		s.params = append(append([][]byte{}, sps...), pps...)
		s.lenSize = ls
	}
	s.vt = vt
	s.r.Keyframes(vt) // build the index before any concurrent use
	dur := vt.Duration
	if n := vt.SampleCount(); n > 0 {
		end := s.r.PTS(vt, n-1) + s.r.SampleDuration(vt, n-1)
		if end > dur {
			dur = end
		}
	}
	fps := vt.FPS
	if fps == 0 {
		fps = float32(fpsVUI)
	}
	s.video = &VideoTrackInfo{
		Codec:       vt.Codec,
		Width:       w,
		Height:      h,
		FPS:         fps,
		Duration:    dur,
		PixelFormat: "yuv420p",
	}
	s.play = s.newVideoDecoder()
	s.play.start(0, 0, false)
	s.ahead = startDecodeAhead(s.play)
	return nil
}

// decodeAheadFrames is the depth of the decode ring between the decoder
// goroutine and NextVideoFrame (~130 ms at 30 fps).
const decodeAheadFrames = 4

// decodeAhead runs one videoDecoder on its own goroutine, keeping up to
// decodeAheadFrames presentation-ordered frames ready, so decode time
// overlaps display instead of stalling the pump. A seek replaces it.
type decodeAhead struct {
	ring   chan *golangVideoFrame
	cancel context.CancelFunc
	done   chan struct{}
	err    error // terminal error; written before ring is closed
}

func startDecodeAhead(v *videoDecoder) *decodeAhead {
	ctx, cancel := context.WithCancel(context.Background())
	a := &decodeAhead{ring: make(chan *golangVideoFrame, decodeAheadFrames), cancel: cancel, done: make(chan struct{})}
	go func() {
		defer close(a.done)
		defer close(a.ring)
		for {
			f, pts, err := v.nextFrame(ctx)
			if err != nil {
				a.err = err
				return
			}
			vf := newGolangVideoFrame(pts, f)
			select {
			case a.ring <- vf:
			case <-ctx.Done():
				vf.Release()
				a.err = ctx.Err()
				return
			}
		}
	}()
	return a
}

// stop ends the goroutine and releases every frame still in the ring.
func (a *decodeAhead) stop() {
	a.cancel()
	<-a.done
	for f := range a.ring {
		f.Release()
	}
}

func (s *golangSource) VideoTrack() *VideoTrackInfo { return s.video }

func (s *golangSource) AudioTrack() *AudioTrackInfo {
	if s.audio == nil {
		return nil
	}
	return s.audio.info
}

func (s *golangSource) AudioRenderer() AudioRenderer {
	if s.audio == nil {
		return nil
	}
	return s.audio
}

func (s *golangSource) NextVideoFrame(ctx context.Context) (VideoFrame, error) {
	for {
		s.mu.Lock()
		a, closed := s.ahead, s.closed
		s.mu.Unlock()
		if closed || a == nil {
			return nil, io.EOF
		}
		select {
		case f, ok := <-a.ring:
			if ok {
				return f, nil
			}
			// The ring closed: either decoding ended (report it) or a seek
			// replaced the ring (read the new one).
			s.mu.Lock()
			same := s.ahead == a
			s.mu.Unlock()
			if same {
				if a.err == nil {
					return nil, io.EOF
				}
				return nil, a.err
			}
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
}

// SeekVideo restarts decoding at the keyframe at or before pts. With
// precise, frames before pts are decoded but not returned.
func (s *golangSource) SeekVideo(pts time.Duration, precise bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return io.EOF
	}
	if s.play == nil {
		return nil
	}
	s.ahead.stop()
	idx := s.r.SeekKeyframe(s.vt, pts)
	s.play.start(idx, pts, precise)
	s.ahead = startDecodeAhead(s.play)
	return nil
}

func (s *golangSource) OpenScrubDecoder() (ScrubDecoder, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil, io.EOF
	}
	if s.vt == nil {
		return nil, ErrNoTrack
	}
	if s.scrub == nil {
		s.scrub = &golangScrub{dec: s.newVideoDecoder()}
	}
	return s.scrub, nil
}

func (s *golangSource) Close() error {
	var err error
	s.closeOnce.Do(func() {
		if s.audio != nil {
			_ = s.audio.Close()
		}
		s.mu.Lock()
		s.closed = true
		a := s.ahead
		s.mu.Unlock()
		if a != nil {
			a.stop()
		}
		if s.scrub != nil {
			s.scrub.mu.Lock()
			s.scrub.closed = true
			s.scrub.mu.Unlock()
		}
		err = s.r.Close()
	})
	return err
}

// videoCodec is the decoder interface shared by h264.Decoder,
// hevc.Decoder and av1Codec (all output h264.Frame pictures from one frame
// pool).
type videoCodec interface {
	SetTag(tag int64)
	DecodeNAL(nal []byte) error
	DecodeNALs(nals [][]byte) error
	Next() *h264.Frame
	Flush()
}

// av1Codec adapts av1.Decoder to videoCodec: each "NAL" is a whole
// temporal unit.
type av1Codec struct{ *av1.Decoder }

func (c av1Codec) DecodeNAL(tu []byte) error { return c.Decode(tu) }

func (c av1Codec) DecodeNALs(tus [][]byte) error {
	var first error
	for _, tu := range tus {
		if err := c.Decode(tu); err != nil && first == nil {
			first = err
		}
	}
	return first
}

// videoDecoder feeds samples from the video track into the H.264, HEVC or
// AV1 decoder and yields pictures in display order with their presentation
// times.
type videoDecoder struct {
	s       *golangSource
	dec     videoCodec
	next    int  // next sample to feed
	flushed bool // end of track reached and decoder flushed
	// Frames before minPTS are dropped: leading pictures of an open GOP
	// after a seek (which reference pictures from before the keyframe)
	// and, for a precise seek, everything before the target.
	minPTS  time.Duration
	target  time.Duration
	precise bool
	buf     []byte
	errs    int
}

func (s *golangSource) newVideoDecoder() *videoDecoder {
	return &videoDecoder{s: s}
}

// start restarts decoding at sample idx.
func (v *videoDecoder) start(idx int, target time.Duration, precise bool) {
	s := v.s
	switch {
	case s.av1:
		v.dec = av1Codec{av1.New()}
	case s.hevc:
		v.dec = hevc.New()
	default:
		v.dec = h264.New()
	}
	for _, n := range s.params {
		_ = v.dec.DecodeNAL(n)
	}
	v.next = idx
	v.flushed = false
	v.minPTS = s.r.PTS(s.vt, idx)
	if idx == 0 {
		v.minPTS = -1 << 62
	}
	v.target, v.precise = target, precise
	v.errs = 0
}

// nextFrame returns the next picture in display order.
func (v *videoDecoder) nextFrame(ctx context.Context) (*h264.Frame, time.Duration, error) {
	s := v.s
	n := s.vt.SampleCount()
	for {
		for f := v.dec.Next(); f != nil; f = v.dec.Next() {
			pts := s.r.PTS(s.vt, int(f.Tag))
			if pts < v.minPTS || v.precise && pts+s.r.SampleDuration(s.vt, int(f.Tag)) <= v.target {
				h264.Recycle(f)
				continue
			}
			return f, pts, nil
		}
		if v.next >= n {
			if v.flushed {
				return nil, 0, io.EOF
			}
			v.dec.Flush()
			v.flushed = true
			continue
		}
		if err := ctx.Err(); err != nil {
			return nil, 0, err
		}
		if err := v.feed(v.next); err != nil {
			return nil, 0, err
		}
		v.next++
	}
}

var errVideoCorrupt = errors.New("media: video stream could not be decoded")

func (v *videoDecoder) feed(i int) error {
	s := v.s
	b, err := s.r.ReadSampleInto(v.buf, s.vt, i)
	if err != nil {
		return err
	}
	v.buf = b
	var nals [][]byte
	switch {
	case s.av1:
		nals = [][]byte{b}
	case s.hevc:
		nals, err = hevc.SplitLengthPrefixed(b, s.lenSize)
	default:
		nals, err = h264.SplitLengthPrefixed(b, s.lenSize)
	}
	if err != nil && len(nals) == 0 {
		v.errs++
	}
	v.dec.SetTag(int64(i))
	if err := v.dec.DecodeNALs(nals); err != nil {
		if errors.Is(err, h264.ErrUnsupported) || errors.Is(err, hevc.ErrUnsupported) || errors.Is(err, av1.ErrUnsupported) {
			return ErrCodecUnsupported
		}
		v.errs++
	} else {
		v.errs = 0
	}
	if v.errs > 300 {
		return errVideoCorrupt
	}
	return nil
}

// golangScrub is an independent single-frame fetcher with its own decoder,
// so scrubbing never disturbs playback.
type golangScrub struct {
	mu      sync.Mutex
	dec     *videoDecoder
	started bool
	lastPTS time.Duration
	// lastEnd is lastPTS plus that frame's duration: the frame already
	// handed out covers [lastPTS, lastEnd), so only a pts at or past
	// lastEnd can continue forward from the decoder's position.
	lastEnd time.Duration
	closed  bool
}

func (d *golangScrub) DecodeAt(pts time.Duration) (VideoFrame, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.closed {
		return nil, io.EOF
	}
	v := d.dec
	s := v.s
	idx := s.r.SeekKeyframe(s.vt, pts)
	// Continue forward within the same GOP instead of restarting.
	if !d.started || pts < d.lastEnd || idx >= v.next || s.r.SeekKeyframe(s.vt, d.lastPTS) != idx {
		v.start(idx, 0, false)
		d.started = true
	}
	var best *h264.Frame
	var bestPTS time.Duration
	for {
		f, fpts, err := v.nextFrame(context.Background())
		if err != nil {
			if best != nil {
				break
			}
			d.started = false
			return nil, err
		}
		h264.Recycle(best)
		best, bestPTS = f, fpts
		end := fpts + s.r.SampleDuration(s.vt, int(f.Tag))
		d.lastPTS, d.lastEnd = fpts, end
		if end > pts {
			break
		}
	}
	return newGolangVideoFrame(bestPTS, best), nil
}

func (d *golangScrub) Close() error {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.closed = true
	return nil
}
