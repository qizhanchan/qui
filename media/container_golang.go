//go:build cgo

package media

import (
	"context"
	"io"
	"math"
	"time"

	"github.com/qizhanchan/qui/media/internal/audioout"
	"github.com/qizhanchan/qui/media/internal/av1"
	"github.com/qizhanchan/qui/media/internal/mp3"
	"github.com/qizhanchan/qui/media/internal/ogg"
	"github.com/qizhanchan/qui/media/internal/opus"
	"github.com/qizhanchan/qui/media/internal/vp9"
	"github.com/qizhanchan/qui/media/internal/webm"
)

// Containers other than MP4 for the pure-Go pipeline: WebM / Matroska
// (internal/webm: AV1, H.264, HEVC, VP9 profile 0 video; Opus, AAC
// audio), Ogg (internal/ogg: Opus) and raw MPEG audio (internal/mp3).
// VP8, VP9 profiles 1–3 and Vorbis have no decoder: such tracks are
// ignored (a VP9 4:4:4 + Opus WebM plays its audio), and a file left with
// nothing decodable fails with ErrCodecUnsupported.

// sniffHead is how much of the file sniffContainer looks at.
const sniffHead = 16 << 10

// sniffContainer names the container of the bytes in ra: "mp4", "webm"
// (WebM and Matroska), "ogg", "mp3", or "" when unrecognized.
func sniffContainer(ra io.ReaderAt, size int64) string {
	head := make([]byte, min(size, sniffHead))
	n, _ := ra.ReadAt(head, 0)
	head = head[:n]
	switch {
	case webm.Sniff(head):
		return "webm"
	case ogg.Sniff(head):
		return "ogg"
	case looksMP4(head):
		return "mp4"
	case mp3.Sniff(head):
		return "mp3"
	}
	return ""
}

// looksMP4 reports whether head starts with an ISO BMFF box.
func looksMP4(head []byte) bool {
	if len(head) < 8 {
		return false
	}
	switch string(head[4:8]) {
	case "ftyp", "moov", "mdat", "free", "skip", "wide", "pnot", "styp", "moof", "sidx", "uuid":
		return true
	}
	return false
}

// openContainer opens a non-MP4 container read through ra. closer is
// what the Source releases on Close (also on failure here).
func openContainer(kind string, ra io.ReaderAt, size int64, closer io.Closer) (Source, error) {
	var s Source
	var err error
	switch kind {
	case "webm":
		s, err = openWebM(ra, size, closer, audioout.Open)
	case "ogg":
		s, err = openOgg(ra, size, closer, audioout.Open)
	case "mp3":
		s, err = openMP3(ra, size, closer, audioout.Open)
	default:
		err = ErrNotSupported
	}
	if err != nil {
		_ = closer.Close()
		return nil, err
	}
	return s, nil
}

type sinkOpener = func(audioout.Format) (audioout.Sink, error)

// --- WebM / Matroska ---

func openWebM(ra io.ReaderAt, size int64, closer io.Closer, open sinkOpener) (Source, error) {
	r, err := webm.Open(ra, size)
	if err != nil {
		return nil, err
	}
	s := &golangSource{closer: closer}
	dur := r.Duration()
	if vt := r.VideoTrack(); vt != nil && (vt.Codec == "h264" || vt.Codec == "hevc" || vt.Codec == "av1" || vt.Codec == "vp9" && webmVP9Playable(r, vt)) {
		cfg, err := webmVideoConfig(r, vt, dur)
		if err == nil {
			err = s.openVideo(cfg, webmTrack{r, vt})
		}
		if err != nil {
			s.stopVideo()
			return nil, err
		}
	}
	if at := r.AudioTrack(); at != nil && golangAudioAvailable() {
		if a, err := newWebMAudio(r, at, dur, open); err == nil {
			s.audio = a
		}
	}
	if s.video == nil && s.audio == nil {
		return nil, ErrCodecUnsupported
	}
	main := r.VideoTrack()
	if s.video == nil {
		main = r.AudioTrack()
	}
	s.buffered = func() []TimeRange { return webmBuffered(r, main, ra, dur) }
	return s, nil
}

// stopVideo ends the decode-ahead goroutine of a source that failed to
// open.
func (s *golangSource) stopVideo() {
	if s.ahead != nil {
		s.ahead.stop()
		s.ahead = nil
	}
}

// webmVP9Playable reports whether a VP9 track is profile 0 (8-bit 4:2:0),
// judged by its first frame; other profiles are skipped so the audio
// still plays.
func webmVP9Playable(r *webm.Reader, vt *webm.Track) bool {
	b, err := r.ReadSample(vt, 0)
	return err != nil || vp9.ProfileOf(b) <= 0
}

// webmVideoConfig reads the decoder configuration from CodecPrivate.
func webmVideoConfig(r *webm.Reader, vt *webm.Track, dur time.Duration) (videoConfig, error) {
	cfg := videoConfig{codec: vt.Codec, fps: float32(vt.FPS), duration: dur}
	cp := vt.CodecPrivate
	var err error
	switch vt.Codec {
	case "av1":
		// AV1CodecConfigurationRecord: 4 bytes, then configOBUs (which
		// may be empty: the sequence header is then in the first
		// keyframe).
		obus := cp
		if len(cp) >= 4 && cp[0]&0x80 != 0 {
			obus = cp[4:]
		}
		if av1.SplitSequenceHeader(obus) == nil {
			tu, rerr := r.ReadSample(vt, 0)
			if rerr != nil {
				return cfg, rerr
			}
			obus = av1.SplitSequenceHeader(tu)
		}
		cfg.av1OBUs = obus
	case "vp9":
		// No configuration record is needed: every keyframe carries its
		// header. The size comes from the track, else the first frame.
		cfg.width, cfg.height = vt.Width, vt.Height
		if b, rerr := r.ReadSample(vt, 0); rerr == nil {
			if vp9.ProfileOf(b) > 0 {
				return cfg, ErrCodecUnsupported
			}
			if w, h, ok := vp9.KeyframeSize(b); ok && (cfg.width == 0 || cfg.height == 0) {
				cfg.width, cfg.height = w, h
			}
		}
	case "hevc":
		cfg.vps, cfg.sps, cfg.pps, cfg.lenSize, err = webm.HVCC(cp)
	case "h264":
		cfg.sps, cfg.pps, cfg.lenSize, err = webm.AVCC(cp)
	default:
		err = ErrCodecUnsupported
	}
	if err != nil {
		return cfg, ErrCodecUnsupported
	}
	return cfg, nil
}

// newWebMAudio starts the renderer for an Opus or AAC track.
func newWebMAudio(r *webm.Reader, at *webm.Track, dur time.Duration, open sinkOpener) (*golangAudio, error) {
	var dec audioDecoder
	var err error
	st := &trackStream{tr: newWebMAudioTrack(r, at)}
	switch at.Codec {
	case "opus":
		dec, err = newOpusDecoder(at.CodecPrivate)
		st.preRoll = at.SeekPreRoll
		if st.preRoll == 0 {
			st.preRoll = opusSeekPreRoll
		}
	case "aac":
		dec, err = newAACDecoder(at.CodecPrivate)
		st.preRollPackets = 1
	default:
		return nil, ErrCodecUnsupported // Vorbis, …
	}
	if err != nil {
		return nil, err
	}
	st.rate = dec.SampleRate()
	st.padding = func(i int) time.Duration {
		smp, _ := r.Sample(at, i)
		return smp.DiscardPadding
	}
	return newGolangAudioStream(at.Codec, dec, st, dur, open)
}

// webmTrack is a sampleTrack over a Matroska track. Its index grows
// lazily: count is what has been indexed so far, and more scans further
// (blocking on a ByteSource still downloading).
type webmTrack struct {
	r *webm.Reader
	t *webm.Track
}

func (w webmTrack) count() int                             { return w.r.IndexedCount(w.t) }
func (w webmTrack) pts(i int) time.Duration                { return w.r.PTS(w.t, i) }
func (w webmTrack) dur(i int) time.Duration                { return w.r.SampleDuration(w.t, i) }
func (w webmTrack) read(buf []byte, i int) ([]byte, error) { return w.r.ReadSampleInto(buf, w.t, i) }
func (w webmTrack) seekKeyframe(t time.Duration) int       { return w.r.SeekKeyframe(w.t, t) }
func (w webmTrack) gen() uint64                            { return 0 }

func (w webmTrack) sampleAt(t time.Duration) int {
	i := w.r.SampleAtTime(w.t, t)
	if n := w.count(); i >= n && n > 0 && t < w.pts(n-1)+w.dur(n-1) {
		i = n - 1
	}
	return i
}

func (w webmTrack) more(_ context.Context, n int) bool {
	_, err := w.r.Sample(w.t, n)
	return err == nil
}

// webmAudioTrack refines Matroska's block timestamps (usually whole
// milliseconds) to the sample grid: when frame i starts within a
// millisecond of first + i·step (constant-size frames, as AAC
// always and Opus usually has), that exact time is used, so a seek trims
// to the right sample.
type webmAudioTrack struct {
	webmTrack
	first time.Duration // PTS of frame 0 (−CodecDelay)
	step  time.Duration // DefaultDuration, else frame 0's duration
}

func newWebMAudioTrack(r *webm.Reader, t *webm.Track) sampleTrack {
	w := webmTrack{r, t}
	smp, err := r.Sample(t, 0)
	if err != nil {
		return w
	}
	step := t.DefaultDuration
	if step <= 0 {
		step = smp.Duration // the first frame's (Opus: from its TOC)
	}
	if step <= 0 {
		return w
	}
	return webmAudioTrack{w, smp.PTS, step}
}

func (w webmAudioTrack) pts(i int) time.Duration {
	p := w.webmTrack.pts(i)
	if g := w.first + time.Duration(i)*w.step; g-p <= time.Millisecond && p-g <= time.Millisecond {
		return g
	}
	return p
}

// webmBuffered maps downloaded bytes onto time through the frames
// indexed so far (a file is fully buffered).
func webmBuffered(r *webm.Reader, t *webm.Track, ra io.ReaderAt, dur time.Duration) []TimeRange {
	br, ok := ra.(ByteRanger)
	if !ok {
		return []TimeRange{{0, dur.Seconds()}}
	}
	ranges := br.BufferedBytes()
	var out []TimeRange
	in := false
	var cur TimeRange
	n := r.IndexedCount(t)
	for i := 0; i < n; i++ {
		smp, err := r.Sample(t, i)
		if err != nil {
			break
		}
		pts, end := smp.PTS.Seconds(), (smp.PTS + smp.Duration).Seconds()
		if coveredBy(ranges, smp.Offset, int64(smp.Size)) {
			if !in {
				cur, in = TimeRange{max(pts, 0), end}, true
			} else {
				cur.Start = min(cur.Start, max(pts, 0))
				cur.End = max(cur.End, end)
			}
			continue
		}
		if in {
			out = append(out, cur)
			in = false
		}
	}
	if in {
		out = append(out, cur)
	}
	return out
}

// --- Opus ---

// opusSeekPreRoll is the decoder convergence time RFC 7845 §4.6 asks for.
const opusSeekPreRoll = 80 * time.Millisecond

// newOpusDecoder builds a 48 kHz decoder from an OpusHead (Ogg header
// packet or Matroska CodecPrivate), applying its output gain. The
// pre-skip is not applied here: containers put it in the timestamps
// (negative PTS for the first packets), which the renderer trims.
func newOpusDecoder(head []byte) (audioDecoder, error) {
	h, err := opus.ParseOpusHead(head)
	if err != nil {
		return nil, ErrCodecUnsupported
	}
	if h.MappingFamily == 0 && h.Channels <= 2 {
		d, err := opus.NewDecoder(48000, h.Channels)
		if err != nil {
			return nil, ErrCodecUnsupported
		}
		if h.OutputGain != 0 {
			d.SetGain(h.OutputGain)
		}
		return d, nil
	}
	m, err := opus.NewMultistreamDecoder(h)
	if err != nil {
		return nil, ErrCodecUnsupported
	}
	return m, nil
}

// --- Ogg ---

func openOgg(ra io.ReaderAt, size int64, closer io.Closer, open sinkOpener) (Source, error) {
	r, err := ogg.Open(ra, size)
	if err != nil {
		return nil, err
	}
	if r.Codec != "opus" || len(r.Headers) == 0 {
		return nil, ErrCodecUnsupported // Vorbis has no decoder
	}
	if !golangAudioAvailable() {
		return nil, ErrCodecUnsupported
	}
	dec, err := newOpusDecoder(r.Headers[0])
	if err != nil {
		return nil, err
	}
	preRoll := r.SeekPreRoll
	if preRoll == 0 {
		preRoll = opusSeekPreRoll
	}
	dur := r.Duration()
	a, err := newGolangAudioStream("opus", dec, &oggStream{r: r, rate: r.SampleRate, preRoll: preRoll}, dur, open)
	if err != nil {
		return nil, err
	}
	s := &golangSource{closer: closer, audio: a}
	s.buffered = func() []TimeRange { return linearBuffered(ra, size, dur) }
	return s, nil
}

// oggStream is an audioStream over an Ogg logical stream.
type oggStream struct {
	r       *ogg.Reader
	rate    int
	preRoll time.Duration
}

func (o *oggStream) seek(t time.Duration) { _, _ = o.r.Seek(max(t-o.preRoll, 0)) }

func (o *oggStream) next(buf []byte) (audioPacket, error) {
	pk, err := o.r.ReadPacket()
	if err != nil {
		return audioPacket{}, err
	}
	p := audioPacket{data: append(buf[:0], pk.Data...), pts: pk.PTS}
	if keep := int(math.Round(pk.Duration.Seconds() * float64(o.rate))); keep < pk.Samples {
		p.dropEnd = pk.Samples - max(keep, 0)
	}
	return p, nil
}

// --- MP3 ---

// mp3PreRollFrames frames are decoded before a seek target so the bit
// reservoir (up to 511 bytes back, several frames at low bitrates) and
// the filterbank overlap are primed.
const mp3PreRollFrames = 12

// mp3DecoderDelay is the decoder's own delay in samples (filterbank 528 +
// synthesis 1), which a gapless (LAME-tagged) timeline removes with the
// encoder delay, as internal/mp3 and FFmpeg do.
const mp3DecoderDelay = 529

func openMP3(ra io.ReaderAt, size int64, closer io.Closer, open sinkOpener) (Source, error) {
	st, err := mp3.Open(ra, size)
	if err != nil {
		return nil, err
	}
	if !golangAudioAvailable() {
		return nil, ErrCodecUnsupported
	}
	if _, progressive := ra.(ByteRanger); !progressive {
		// A local file: index every frame for exact seeks and duration.
		_ = st.BuildIndex()
	}
	rate, chs, spf := st.SampleRate(), st.Channels(), st.SamplesPerFrame()
	if rate <= 0 || chs <= 0 || spf <= 0 {
		return nil, ErrCodecUnsupported
	}
	dur := st.Duration()
	ms := &mp3Stream{s: st, rate: rate, spf: spf, start: *st}
	if delay, _, ok := st.Gapless(); ok {
		ms.startPTS = -time.Duration(int64(delay+mp3DecoderDelay) * int64(time.Second) / int64(rate))
	}
	if _, ok := st.NumFrames(); ok {
		ms.end = dur
	}
	a, err := newGolangAudioStream("mp3", &mp3Decoder{Decoder: mp3.NewDecoder(), rate: rate, chs: chs}, ms, dur, open)
	if err != nil {
		return nil, err
	}
	s := &golangSource{closer: closer, audio: a}
	s.buffered = func() []TimeRange { return linearBuffered(ra, size, dur) }
	return s, nil
}

// mp3Decoder reports the stream's format before the first frame is
// decoded.
type mp3Decoder struct {
	*mp3.Decoder
	rate, chs int
}

func (d *mp3Decoder) SampleRate() int { return d.rate }
func (d *mp3Decoder) Channels() int   { return d.chs }

// mp3Stream is an audioStream over an MPEG audio elementary stream, on
// the gapless timeline (encoder delay and padding removed).
type mp3Stream struct {
	s         *mp3.Stream
	rate, spf int
	end       time.Duration // gapless end; 0 when the frame count is unknown
	k0        int64         // frame index at p0
	p0        time.Duration
	// start is the stream as opened (positioned on the first frame),
	// whose time is startPTS (negative: encoder + decoder delay).
	start    mp3.Stream
	startPTS time.Duration
}

func (m *mp3Stream) frameDur(k int64) time.Duration {
	return time.Duration(k * int64(m.spf) * int64(time.Second) / int64(m.rate))
}

func (m *mp3Stream) seek(t time.Duration) {
	if t -= m.frameDur(mp3PreRollFrames); t <= 0 {
		// From the first frame: SeekFrame(0) lands on the frame holding
		// time 0, after the priming frames the decoder needs.
		*m.s = m.start
		m.p0, m.k0 = m.startPTS, m.s.FrameIndex()
		return
	}
	p, _ := m.s.SeekFrame(t)
	m.p0, m.k0 = p, m.s.FrameIndex()
}

func (m *mp3Stream) next(buf []byte) (audioPacket, error) {
	pts := m.p0 + m.frameDur(m.s.FrameIndex()-m.k0)
	b, err := m.s.ReadFrame()
	if err != nil {
		return audioPacket{}, err
	}
	p := audioPacket{data: append(buf[:0], b...), pts: pts}
	if m.end > 0 {
		keep := int(math.Round((m.end - pts).Seconds() * float64(m.rate)))
		if keep < m.spf {
			p.dropEnd = m.spf - max(keep, 0)
		}
	}
	return p, nil
}

// linearBuffered maps downloaded byte ranges proportionally onto the
// duration (containers without a byte-addressed index; a file is fully
// buffered).
func linearBuffered(ra io.ReaderAt, size int64, dur time.Duration) []TimeRange {
	br, ok := ra.(ByteRanger)
	if !ok || size <= 0 {
		return []TimeRange{{0, dur.Seconds()}}
	}
	var out []TimeRange
	d := dur.Seconds()
	for _, rg := range br.BufferedBytes() {
		out = append(out, TimeRange{float64(rg[0]) / float64(size) * d, float64(rg[1]) / float64(size) * d})
	}
	return out
}
