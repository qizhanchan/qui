package mp3

import (
	"io"
	"time"
)

// seekPreroll is how many frames before a seek target are decoded and
// discarded so that the bit reservoir (up to 511 bytes back, 255 for LSF) and the
// filterbank overlap are fully rebuilt; the samples after a seek then
// equal those of a continuous decode.
func (s *Stream) seekPreroll() int64 {
	h := s.first
	if h.Layer != 3 {
		return 2
	}
	// The smallest possible frame at this rate (lowest bitrate) bounds how
	// many frames the reservoir can span.
	h.Padding = false
	lsf, maxBack := 0, int64(511)
	if h.LSF() {
		lsf, maxBack = 1, 255
	}
	h.Bitrate = bitrateTable[lsf][h.Layer-1][1] * 1000
	minFrame := int64(h.FrameSize() - 4 - h.sideInfoSize())
	if minFrame < 8 {
		minFrame = 8
	}
	p := 2 + (maxBack+minFrame-1)/minFrame
	if p > 40 {
		p = 40
	}
	return p
}

// PCMReader decodes a Stream to interleaved float32 PCM, applying gapless
// trimming (LAME/Lavc encoder delay + decoder delay at the start, encoder
// padding at the end) and sample-accurate seeking.
type PCMReader struct {
	s   *Stream
	d   *Decoder
	pcm []float32

	start int64 // first absolute (untrimmed) sample to emit
	end   int64 // absolute end, -1 unknown
}

// NewPCMReader returns a reader positioned at the start of s.
func NewPCMReader(s *Stream) *PCMReader {
	r := &PCMReader{s: s, d: NewDecoder()}
	r.start = s.trimStart()
	r.end = -1
	if n := s.validSamples(); n >= 0 {
		r.end = r.start + n
	}
	return r
}

// Decoder returns the underlying frame decoder.
func (r *PCMReader) Decoder() *Decoder { return r.d }

// Read decodes the next frame and appends its trimmed interleaved samples
// to out (possibly none, e.g. inside the gapless lead-in). It returns
// io.EOF once the stream is exhausted. Corrupt frames are decoded as well
// as possible (silence where unreadable) and do not stop the reader.
func (r *PCMReader) Read(out []float32) ([]float32, error) {
	nch := int64(r.s.Channels())
	spf := int64(r.s.SamplesPerFrame())
	for {
		k := r.s.FrameIndex()
		if r.end >= 0 && k*spf >= r.end {
			return out, io.EOF
		}
		frame, err := r.s.ReadFrame()
		if err != nil {
			return out, err
		}
		r.pcm, _ = r.d.Decode(frame, r.pcm[:0])
		if r.d.Channels() != int(nch) || len(r.pcm) == 0 {
			continue
		}
		n := int64(len(r.pcm)) / nch
		a, b := k*spf, k*spf+n
		lo, hi := a, b
		if lo < r.start {
			lo = r.start
		}
		if r.end >= 0 && hi > r.end {
			hi = r.end
		}
		if lo >= hi {
			continue
		}
		return append(out, r.pcm[(lo-a)*nch:(hi-a)*nch]...), nil
	}
}

// Seek positions the reader so that the next Read starts at time t of the
// trimmed timeline; it returns the actual position (exact with an index or
// a CBR stream's exact frame grid, otherwise an estimate).
func (r *PCMReader) Seek(t time.Duration) (time.Duration, error) {
	if t < 0 {
		t = 0
	}
	sr := int64(r.s.SampleRate())
	spf := int64(r.s.SamplesPerFrame())
	target := r.s.trimStart() + int64(t)*sr/int64(time.Second)
	k := target/spf - r.s.seekPreroll()
	if err := r.s.seekToFrame(k); err != nil {
		return 0, err
	}
	r.d.Reset()
	r.start = target
	if r.start < r.s.trimStart() {
		r.start = r.s.trimStart()
	}
	return samplesToDuration(r.start-r.s.trimStart(), sr), nil
}

// samplesToDuration converts a sample count to a duration, rounding up so
// that converting back with floor(d*sr/1s) yields n again.
func samplesToDuration(n, sr int64) time.Duration {
	d := n * int64(time.Second)
	q := d / sr
	if d > 0 && d%sr != 0 {
		q++
	}
	return time.Duration(q)
}
