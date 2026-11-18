package opus

import "fmt"

// MultistreamDecoder decodes Opus multistream packets (RFC 7845 section
// 5.1.1, channel mapping families 0, 1 and 255): one packet carries
// Streams elementary streams, the first CoupledStreams of them stereo, all
// but the last self-delimited. Output channels follow Head.Mapping.
type MultistreamDecoder struct {
	head     Head
	fs       int
	decs     []*Decoder
	gain     float32
	scratch  [][]float32
	lastSize int
}

// NewMultistreamDecoder returns a 48 kHz decoder for the stream described
// by head. Output gain from the header is applied.
func NewMultistreamDecoder(head Head) (*MultistreamDecoder, error) {
	return NewMultistreamDecoderRate(head, 48000)
}

// NewMultistreamDecoderRate is NewMultistreamDecoder at another output
// sample rate (48000, 24000, 16000, 12000 or 8000).
func NewMultistreamDecoderRate(head Head, sampleRate int) (*MultistreamDecoder, error) {
	if head.Channels < 1 || head.Channels > 255 || head.Streams < 1 || head.CoupledStreams > head.Streams ||
		len(head.Mapping) != head.Channels {
		return nil, fmt.Errorf("opus: bad multistream head: %w", ErrBadArg)
	}
	m := &MultistreamDecoder{head: head, fs: sampleRate}
	for s := 0; s < head.Streams; s++ {
		ch := 1
		if s < head.CoupledStreams {
			ch = 2
		}
		d, err := NewDecoder(sampleRate, ch)
		if err != nil {
			return nil, err
		}
		m.decs = append(m.decs, d)
		m.scratch = append(m.scratch, make([]float32, sampleRate*120/1000*ch))
	}
	m.lastSize = sampleRate / 50
	m.gain = 1
	if head.OutputGain != 0 {
		m.decs[0].SetGain(head.OutputGain)
		m.gain = m.decs[0].gain
		m.decs[0].gain = 1
	}
	return m, nil
}

// Channels is the number of output channels.
func (m *MultistreamDecoder) Channels() int { return m.head.Channels }

// SampleRate is the output sample rate.
func (m *MultistreamDecoder) SampleRate() int { return m.fs }

// Reset clears the state of every stream decoder.
func (m *MultistreamDecoder) Reset() {
	for _, d := range m.decs {
		d.Reset()
	}
}

// Decode decodes one multistream packet and appends the interleaved
// output channels to out. An empty packet is concealed.
func (m *MultistreamDecoder) Decode(packet []byte, out []float32) ([]float32, error) {
	n := -1
	data := packet
	for s, d := range m.decs {
		var got int
		if len(packet) == 0 {
			var err error
			res, err := d.DecodePLC(m.scratch[s][:0])
			if err != nil {
				return out, err
			}
			got = len(res) / d.channels
		} else {
			if len(data) == 0 {
				return out, ErrInvalidPacket
			}
			last := s == len(m.decs)-1
			var used int
			var err error
			got, used, err = d.decodeNative(data, !last, m.scratch[s])
			if err != nil {
				return out, err
			}
			data = data[used:]
		}
		if n >= 0 && got != n {
			return out, ErrInvalidPacket
		}
		n = got
	}
	C := m.head.Channels
	start := len(out)
	out = growFloat(out, n*C)
	pcm := out[start:]
	for c := 0; c < C; c++ {
		idx := int(m.head.Mapping[c])
		if idx == 255 {
			continue
		}
		var src []float32
		var ch, stride int
		if idx < 2*m.head.CoupledStreams {
			src, ch, stride = m.scratch[idx/2], idx&1, 2
		} else {
			src, ch, stride = m.scratch[idx-m.head.CoupledStreams], 0, 1
		}
		for i := 0; i < n; i++ {
			pcm[i*C+c] = src[i*stride+ch] * m.gain
		}
	}
	return out, nil
}

// DecodePLC conceals one lost packet.
func (m *MultistreamDecoder) DecodePLC(out []float32) ([]float32, error) {
	return m.Decode(nil, out)
}
