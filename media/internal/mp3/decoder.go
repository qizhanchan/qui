package mp3

import (
	"errors"
	"fmt"
	"math"
)

var (
	// ErrSync reports a buffer that does not start with a valid MPEG audio
	// frame header.
	ErrSync = errors.New("mp3: invalid frame header")
	// ErrTruncated reports a frame shorter than its header says.
	ErrTruncated = errors.New("mp3: truncated frame")
	errCorrupt   = errors.New("mp3: corrupt frame")
)

// testNoRecover makes Decode re-raise internal panics (robustness tests
// must see them instead of the recovered error).
var testNoRecover = false

// Decoder decodes MPEG-1/2/2.5 Layer I, II and III frames. It keeps the
// Layer III bit reservoir and filterbank state across frames, so frames
// must be fed in stream order; call Reset after a seek.
type Decoder struct {
	channels   int
	sampleRate int

	gr      [2][2]granule
	res     []byte // bit reservoir: main data of previous frames
	scratch []byte
	overlap [2][32][18]float64
	synth   [2]synthState
	pcm     [2][1152]float64
}

// NewDecoder returns a decoder; the channel count and sample rate are
// known after the first decoded frame.
func NewDecoder() *Decoder {
	return &Decoder{res: make([]byte, 0, 4096)}
}

// Channels is the channel count of the last decoded frame (1 or 2).
func (d *Decoder) Channels() int { return d.channels }

// SampleRate is the sample rate of the last decoded frame.
func (d *Decoder) SampleRate() int { return d.sampleRate }

// Reset discards the bit reservoir and filterbank history, as needed after
// a seek. Layer III frames that reference reservoir bytes from before the
// reset then produce no output (see Decode).
func (d *Decoder) Reset() {
	d.res = d.res[:0]
	d.overlap = [2][32][18]float64{}
	d.synth[0].reset()
	d.synth[1].reset()
	for ch := range d.gr {
		for gr := range d.gr[ch] {
			d.gr[ch][gr].sf = [40]int{}
		}
	}
}

// Decode decodes one frame (header included, as returned by
// Stream.ReadFrame) and appends SamplesPerFrame interleaved float32 frames
// (full scale ±1) to out.
//
// A Layer III frame whose main_data_begin points before the start of the
// reservoir (the first frame after Reset, or a stream cut mid-reservoir)
// is consumed to refill the reservoir and warm the filterbank but appends
// nothing (its output would be partly silent). A frame with
// a valid header but a corrupt body still appends its (partly silent)
// samples, so timing is preserved, and returns a non-nil error. A buffer
// without a valid header appends nothing and returns ErrSync.
func (d *Decoder) Decode(frame []byte, out []float32) ([]float32, error) {
	n, err := d.decode(frame)
	nch := d.channels
	for i := 0; i < n; i++ {
		for c := 0; c < nch; c++ {
			out = append(out, float32(d.pcm[c][i]))
		}
	}
	return out, err
}

// DecodeInt16 is Decode producing interleaved signed 16-bit samples.
func (d *Decoder) DecodeInt16(frame []byte, out []int16) ([]int16, error) {
	n, err := d.decode(frame)
	nch := d.channels
	for i := 0; i < n; i++ {
		for c := 0; c < nch; c++ {
			v := math.Round(d.pcm[c][i] * 32768)
			if v > 32767 {
				v = 32767
			} else if v < -32768 {
				v = -32768
			}
			out = append(out, int16(v))
		}
	}
	return out, err
}

// decode returns the number of samples per channel left in d.pcm.
func (d *Decoder) decode(frame []byte) (n int, err error) {
	h, ok := ParseHeader(frame)
	if !ok {
		return 0, ErrSync
	}
	size := h.FrameSize()
	if len(frame) < size {
		return 0, ErrTruncated
	}
	spf := h.SamplesPerFrame()
	nch := h.Channels()
	if nch != d.channels && d.channels != 0 {
		// A channel-count switch invalidates the per-channel history.
		d.Reset()
	}
	d.channels = nch
	d.sampleRate = h.SampleRate
	d.silence(spf) // corrupt frames bail out early; never emit stale PCM
	defer func() {
		// Malformed input must never take the host process down.
		if r := recover(); r != nil {
			if testNoRecover {
				panic(r)
			}
			d.Reset()
			d.silence(spf)
			n, err = spf, fmt.Errorf("%w: %v", errCorrupt, r)
		}
	}()
	switch h.Layer {
	case 1:
		err = d.layer1(h, frame)
	case 2:
		err = d.layer2(h, frame)
	default:
		var skipped bool
		skipped, err = d.layer3(h, frame)
		if skipped {
			return 0, err
		}
	}
	return spf, err
}

func (d *Decoder) silence(n int) {
	for c := range d.pcm {
		for i := 0; i < n; i++ {
			d.pcm[c][i] = 0
		}
	}
}
