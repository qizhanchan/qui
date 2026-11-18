//go:build cgo

package media

import (
	"encoding/binary"
	"errors"
	"io"
	"math"
	"sync"
	"time"

	"github.com/qizhanchan/qui/media/internal/aac"
	"github.com/qizhanchan/qui/media/internal/audioout"
	"github.com/qizhanchan/qui/media/internal/mp4x"
)

// golangAudio is the pure-Go backend's AudioRenderer: compressed packets
// (AAC from MP4/Matroska, Opus from WebM/Ogg, MP3) are decoded on a
// producer goroutine by an audioDecoder and fed to the platform sink
// (internal/audioout). The sink's sample clock drives HostTime, which
// VideoView uses as the master clock.
type golangAudio struct {
	src  audioStream
	info *AudioTrackInfo
	sink audioout.Sink
	rate int // output sample rate
	chs  int

	mu   sync.Mutex
	dec  audioDecoder
	base time.Duration // content time of the sink's zero
	// seekTo is the content time the output must start at; the first
	// packet that decodes to audio sets trim (or pads) from its PTS.
	seekTo  time.Duration
	trimSet bool
	trim    int    // frames still to drop (seek / pre-skip / priming)
	pending []byte // decoded PCM not yet accepted by the sink
	eof     bool
	playing bool
	ended   bool
	onEnded func() // replaces any previous registration, like the interface says
	// onErr reports streaming decode failures (see errorSink); errReported
	// latches so a damaged stretch does not spam one callback per frame.
	onErr       func(error)
	errReported bool
	closed      bool
	// After the last sample has been handed to the device the clock keeps
	// running on wall time, so a video slightly longer than its audio
	// still reaches its end.
	drained   bool
	drainPos  time.Duration
	drainWall time.Time
	drainAcc  time.Duration
	wake      chan struct{}
	done      chan struct{}

	buf  []byte
	f32  []float32
	pcmB []byte
}

// audioDecoder is a codec as the renderer drives it: Decode appends one
// packet's interleaved float32 PCM (full scale ±1) to out; Reset drops
// all state (after a seek). aac.Decoder satisfies it directly.
type audioDecoder interface {
	Decode(pkt []byte, out []float32) ([]float32, error)
	Reset()
	SampleRate() int
	Channels() int
}

// audioPacket is one compressed packet with its timing. dropEnd is the
// number of decoded frames to discard from its end (Opus/Vorbis final
// granule, Matroska DiscardPadding, MP3 gapless padding).
type audioPacket struct {
	data    []byte
	pts     time.Duration
	dropEnd int
}

// errAudioWait: no packet yet, but more may come (an MSE buffer that has
// not been appended to): try again later.
var errAudioWait = errors.New("media: audio packet not yet available")

// audioStream is the packet source of the renderer, over any container.
type audioStream interface {
	// seek positions the stream so that the next packet starts at or
	// before t minus the codec's pre-roll.
	seek(t time.Duration)
	// next returns the next packet, reading into buf's storage; io.EOF
	// after the last, errAudioWait when more may come later.
	next(buf []byte) (audioPacket, error)
}

// trackStream is an audioStream over a sampleTrack (an MP4 or Matroska
// track, or an MSE buffer).
type trackStream struct {
	tr   sampleTrack
	rate int
	// preRollPackets packets (AAC: 1, so the overlap-add of the first
	// audible frame is complete) and preRoll of time (Opus: 80 ms) are
	// decoded before a seek target and discarded.
	preRollPackets int
	preRoll        time.Duration
	// padding, when set, is the DiscardPadding of sample i.
	padding func(i int) time.Duration

	i       int
	trkGen  uint64
	lastPTS time.Duration
}

func (s *trackStream) seek(t time.Duration) {
	idx := s.tr.sampleAt(t - s.preRoll)
	if n := s.tr.count(); idx >= n {
		s.i = n
		return
	}
	s.i = max(idx-s.preRollPackets, 0)
	s.trkGen = s.tr.gen()
}

func (s *trackStream) next(buf []byte) (audioPacket, error) {
	if g := s.tr.gen(); g != s.trkGen {
		s.trkGen = g
		s.i = s.tr.sampleAt(s.lastPTS) + 1
	}
	if s.i >= s.tr.count() {
		if s.tr.more(nil, s.i) && s.i >= s.tr.count() {
			return audioPacket{}, errAudioWait // more is on its way (MSE)
		}
		if s.i >= s.tr.count() {
			return audioPacket{}, io.EOF
		}
	}
	i := s.i
	s.i++
	s.lastPTS = s.tr.pts(i)
	b, err := s.tr.read(buf, i)
	if err != nil {
		return audioPacket{}, err
	}
	p := audioPacket{data: b, pts: s.lastPTS}
	if s.padding != nil {
		if pad := s.padding(i); pad > 0 {
			p.dropEnd = int(math.Round(pad.Seconds() * float64(s.rate)))
		}
	}
	return p, nil
}

func golangAudioAvailable() bool { return audioout.Available() }

func newGolangAudio(r *mp4x.Reader, t *mp4x.Track) (*golangAudio, error) {
	return newGolangAudioSink(r, t, audioout.Open)
}

// newGolangAudioSink is newGolangAudio with an injectable sink (tests).
func newGolangAudioSink(r *mp4x.Reader, t *mp4x.Track, open func(audioout.Format) (audioout.Sink, error)) (*golangAudio, error) {
	return newGolangAudioTrack(r, t, fileTrack{r, t}, open)
}

// newGolangAudioTrack is the AAC renderer over any sample source; r
// supplies the AudioSpecificConfig (a file, or an MSE initialization
// segment).
func newGolangAudioTrack(r *mp4x.Reader, t *mp4x.Track, tr sampleTrack, open func(audioout.Format) (audioout.Sink, error)) (*golangAudio, error) {
	asc, err := r.AudioSpecificConfig()
	if err != nil {
		return nil, err
	}
	if _, mse := tr.(*mseTrack); t.SampleCount() == 0 && !mse {
		return nil, ErrNoTrack
	}
	dec, err := newAACDecoder(asc)
	if err != nil {
		return nil, err
	}
	dur := t.Duration
	if n := tr.count(); n > 0 {
		if end := tr.pts(n-1) + tr.dur(n-1); end > 0 && (dur == 0 || end < dur+time.Second) {
			dur = end
		}
	}
	st := &trackStream{tr: tr, rate: dec.SampleRate(), preRollPackets: 1}
	return newGolangAudioStream("aac", dec, st, dur, open)
}

// newAACDecoder builds an AAC decoder from an AudioSpecificConfig.
func newAACDecoder(asc []byte) (audioDecoder, error) {
	cfg, err := aac.ParseASC(asc)
	if err != nil {
		return nil, err
	}
	dec, err := aac.NewDecoder(cfg)
	if err != nil {
		return nil, ErrCodecUnsupported
	}
	return dec, nil
}

// newGolangAudioStream starts the renderer: codec names the track, dec
// decodes src's packets, dur is the track duration.
func newGolangAudioStream(codec string, dec audioDecoder, src audioStream, dur time.Duration, open func(audioout.Format) (audioout.Sink, error)) (*golangAudio, error) {
	a := &golangAudio{
		src: src, dec: dec, rate: dec.SampleRate(), chs: dec.Channels(),
		wake: make(chan struct{}, 1), done: make(chan struct{}),
	}
	if a.rate <= 0 || a.chs <= 0 {
		return nil, ErrCodecUnsupported
	}
	sink, err := open(audioout.Format{SampleRate: a.rate, Channels: a.chs})
	if err != nil {
		return nil, err
	}
	a.sink = sink
	a.info = &AudioTrackInfo{Codec: codec, SampleRate: a.rate, Channels: a.chs, Duration: dur}
	a.seekLocked(0)
	go a.produce()
	return a, nil
}

// seekLocked positions decoding so that content time t is the next frame
// sent to the sink. Caller holds a.mu.
func (a *golangAudio) seekLocked(t time.Duration) {
	a.src.seek(t)
	a.dec.Reset()
	a.pending = a.pending[:0]
	a.base = t
	a.seekTo = t
	a.trimSet = false
	a.trim = 0
	a.eof = false
	a.ended = false
	a.drained = false
	a.errReported = false
}

func (a *golangAudio) poke() {
	select {
	case a.wake <- struct{}{}:
	default:
	}
}

// produce keeps the sink's buffer topped up.
func (a *golangAudio) produce() {
	defer close(a.done)
	const ahead = 300 * time.Millisecond
	idle := time.NewTimer(time.Hour)
	defer idle.Stop()
	for {
		a.mu.Lock()
		if a.closed {
			a.mu.Unlock()
			return
		}
		work := false
		if len(a.pending) > 0 {
			n, _ := a.sink.Write(a.pending)
			a.pending = a.pending[n:]
			work = n > 0
		}
		var decodeErr error
		if len(a.pending) == 0 && !a.eof && a.sink.Buffered() < ahead {
			decodeErr = a.decodeOneLocked()
			work = true
		}
		if a.eof && len(a.pending) == 0 && !a.drained && a.sink.Buffered() == 0 {
			a.drained = true
			a.drainPos = a.base + a.sink.HostTime()
			a.drainWall = time.Now()
			a.drainAcc = 0
		}
		// Sleep until the sink has drained to the refill threshold rather
		// than polling: each wakeup costs a goroutine handoff.
		wait := 10 * time.Millisecond
		if !a.eof && len(a.pending) == 0 {
			wait = min(max(a.sink.Buffered()-ahead, 10*time.Millisecond), 100*time.Millisecond)
		}
		var fire func()
		if a.eof && len(a.pending) == 0 && !a.ended && a.playing && a.sink.Buffered() == 0 &&
			a.base+a.sink.HostTime() >= a.info.Duration-50*time.Millisecond {
			a.ended = true
			fire = a.onEnded
		}
		var report func(error)
		if decodeErr != nil && !a.errReported {
			a.errReported = true
			report = a.onErr
		} else if decodeErr == nil {
			a.errReported = false
		}
		a.mu.Unlock()
		if fire != nil {
			fire()
		}
		if report != nil {
			report(decodeErr)
		}
		if !work {
			idle.Reset(wait)
			select {
			case <-a.wake:
			case <-idle.C: // Reset (Go 1.23+) never delivers a stale tick
			}
		}
	}
}

func (a *golangAudio) decodeOneLocked() error {
	p, err := a.src.next(a.buf)
	if err == errAudioWait {
		return nil // try again
	}
	if err != nil {
		// The end, or a truncated/unreadable packet, ends the stream.
		a.eof = true
		return nil
	}
	b := p.data
	a.buf = b
	// Decode errors are concealed as silence; they are also reported to the
	// optional handler so callers can log or retry the file.
	var decErr error
	a.f32, decErr = a.dec.Decode(b, a.f32[:0])
	frames := len(a.f32) / a.chs
	if p.dropEnd > 0 {
		frames = max(frames-p.dropEnd, 0)
		a.f32 = a.f32[:frames*a.chs]
	}
	if frames > 0 && !a.trimSet {
		// The first packet that yields audio after a seek: drop what
		// precedes the target (pre-roll, pre-skip, priming), or pad when
		// the content starts after it.
		a.trimSet = true
		a.trim = int(math.Round((a.seekTo - p.pts).Seconds() * float64(a.rate)))
		if a.trim < 0 {
			a.pending = append(a.pending, make([]byte, -a.trim*2*a.chs)...)
			a.trim = 0
		}
	}
	skip := min(a.trim, frames)
	a.trim -= skip
	pcm := a.f32[skip*a.chs:]
	need := len(pcm) * 2
	if cap(a.pcmB) < need {
		a.pcmB = make([]byte, need)
	}
	out := a.pcmB[:need]
	for i, v := range pcm {
		x := math.Round(float64(v) * 32768)
		if x > 32767 {
			x = 32767
		} else if x < -32768 {
			x = -32768
		}
		binary.LittleEndian.PutUint16(out[2*i:], uint16(int16(x)))
	}
	a.pending = append(a.pending, out...)
	return decErr
}
func (a *golangAudio) Play() error {
	a.mu.Lock()
	if !a.playing {
		a.drainWall = time.Now()
	}
	a.playing = true
	a.mu.Unlock()
	a.poke()
	return a.sink.Play()
}

func (a *golangAudio) Pause() error {
	a.mu.Lock()
	if a.playing && a.drained {
		a.drainAcc += time.Since(a.drainWall)
	}
	a.playing = false
	a.mu.Unlock()
	return a.sink.Pause()
}

func (a *golangAudio) Stop() error {
	_ = a.Pause()
	return a.SeekTo(0)
}

func (a *golangAudio) SeekTo(t time.Duration) error {
	if t < 0 {
		t = 0
	}
	if d := a.info.Duration; d > 0 && t > d {
		t = d
	}
	a.mu.Lock()
	a.sink.Flush()
	a.seekLocked(t)
	a.mu.Unlock()
	a.poke()
	return nil
}

func (a *golangAudio) SetVolume(v float32) { a.sink.SetVolume(v) }
func (a *golangAudio) SetRate(r float32)   { a.sink.SetRate(r) }

func (a *golangAudio) HostTime() time.Duration {
	a.mu.Lock()
	base := a.base
	var virt time.Duration = -1
	if a.drained {
		virt = a.drainPos + a.drainAcc
		if a.playing {
			virt += time.Since(a.drainWall)
		}
	}
	a.mu.Unlock()
	t := base + a.sink.HostTime()
	return max(t, virt)
}

func (a *golangAudio) OnEnded(fn func()) {
	a.mu.Lock()
	a.onEnded = fn
	a.mu.Unlock()
}

// SetErrorHandler implements errorSink: the handler receives the first
// decode failure of a run (consecutive failures are coalesced).
func (a *golangAudio) SetErrorHandler(fn func(error)) {
	a.mu.Lock()
	a.onErr = fn
	a.mu.Unlock()
}

func (a *golangAudio) Close() error {
	a.mu.Lock()
	if a.closed {
		a.mu.Unlock()
		return nil
	}
	a.closed = true
	a.mu.Unlock()
	a.poke()
	<-a.done
	return a.sink.Close()
}
