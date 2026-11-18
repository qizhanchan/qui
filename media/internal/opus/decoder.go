package opus

import (
	"errors"
	"fmt"
	"math"
)

// Top-level Opus decoder (RFC 6716 section 4), a port of src/opus_decoder.c:
// TOC parsing, frame packing, SILK/CELT/hybrid dispatch, redundancy frames
// and mode transitions.

const (
	modeSILK   = 1000
	modeHybrid = 1001
	modeCELT   = 1002

	bwNarrow    = 0 // 4 kHz
	bwMedium    = 1 // 6 kHz
	bwWide      = 2 // 8 kHz
	bwSuperWide = 3 // 12 kHz
	bwFull      = 4 // 20 kHz
)

// noRecover disables the panic safety net in the decode entry points so
// tests can detect latent bugs: a malformed packet must be rejected by the
// decoder's own checks, not merely recovered from.
var noRecover = false

var (
	// ErrInvalidPacket reports a packet that violates RFC 6716 framing.
	ErrInvalidPacket = errors.New("opus: invalid packet")
	// ErrBadArg reports an unsupported decoder configuration.
	ErrBadArg = errors.New("opus: bad argument")
)

// Decoder decodes a single Opus stream (mono or stereo).
type Decoder struct {
	fs       int
	channels int
	celt     *celtDecoder
	silk     *silkDecoder
	silkCtl  silkDecControl
	gain     float32 // linear output gain (1 = none)

	streamChannels int
	bandwidth      int
	mode           int
	prevMode       int
	frameSize      int
	prevRedundancy bool
	rangeFinal     uint32
	lastDuration   int // samples per channel of the last packet (output rate)

	pkt           packetInfo
	pcmSilk       []int16
	pcmTransition []float32
	redundant     []float32
	frameBuf      []float32
}

// NewDecoder returns a decoder producing interleaved float32 PCM at
// sampleRate (48000, 24000, 16000, 12000 or 8000) with 1 or 2 channels.
func NewDecoder(sampleRate, channels int) (*Decoder, error) {
	switch sampleRate {
	case 48000, 24000, 16000, 12000, 8000:
	default:
		return nil, fmt.Errorf("opus: unsupported sample rate %d: %w", sampleRate, ErrBadArg)
	}
	if channels != 1 && channels != 2 {
		return nil, fmt.Errorf("opus: unsupported channel count %d: %w", channels, ErrBadArg)
	}
	d := &Decoder{fs: sampleRate, channels: channels, gain: 1}
	d.celt = newCELTDecoder(sampleRate, channels)
	d.silk = newSILKDecoder()
	d.silkCtl.nChannelsAPI = channels
	d.silkCtl.apiSampleRate = sampleRate
	d.Reset()
	return d, nil
}

// SampleRate is the output sample rate.
func (d *Decoder) SampleRate() int { return d.fs }

// Channels is the number of output channels.
func (d *Decoder) Channels() int { return d.channels }

// SetGain sets the output gain in Q8 dB (the OpusHead output gain field).
func (d *Decoder) SetGain(q8dB int) {
	d.gain = float32(math.Pow(10, float64(q8dB)/(20*256)))
}

// FinalRange returns the range coder state after the last decoded frame
// (for conformance checks against the encoder's value).
func (d *Decoder) FinalRange() uint32 { return d.rangeFinal }

// Reset clears all decoding state, e.g. after a seek.
func (d *Decoder) Reset() {
	d.celt.reset()
	d.silk.reset()
	d.streamChannels = d.channels
	d.bandwidth = 0
	d.mode = 0
	d.prevMode = 0
	d.frameSize = d.fs / 400
	d.prevRedundancy = false
	d.rangeFinal = 0
	d.lastDuration = d.fs / 50
}

// Decode decodes one Opus packet and appends its interleaved samples to
// out. An empty packet is treated as lost (see DecodePLC).
func (d *Decoder) Decode(packet []byte, out []float32) ([]float32, error) {
	if len(packet) == 0 {
		return d.DecodePLC(out)
	}
	start := len(out)
	out = growFloat(out, d.fs*120/1000*d.channels)
	n, _, err := d.decodeNative(packet, false, out[start:])
	if err != nil {
		return out[:start], err
	}
	d.applyGain(out[start : start+n*d.channels])
	return out[:start+n*d.channels], nil
}

// decodeNative decodes a (possibly self-delimited) packet into pcm and
// returns the samples per channel and the bytes consumed.
func (d *Decoder) decodeNative(packet []byte, selfDelimited bool, pcm []float32) (n, used int, err error) {
	defer func() {
		if noRecover {
			return
		}
		if r := recover(); r != nil {
			n, used = 0, 0
			err = fmt.Errorf("%w (%v)", ErrInvalidPacket, r)
			d.Reset()
		}
	}()
	pk, used, err := parsePacket(packet, selfDelimited, &d.pkt)
	if err != nil {
		return 0, 0, err
	}
	toc := packet[0]
	d.mode = tocMode(toc)
	d.bandwidth = tocBandwidth(toc)
	d.frameSize = tocSamplesPerFrame(toc, d.fs)
	d.streamChannels = 1 + int(toc>>2&1)
	if pk.count*d.frameSize*d.channels > len(pcm) {
		return 0, 0, ErrInvalidPacket
	}
	for i := 0; i < pk.count; i++ {
		got, err := d.decodeFrame(pk.frames[i], pcm[n*d.channels:], d.frameSize)
		if err != nil {
			return 0, 0, err
		}
		n += got
	}
	d.lastDuration = n
	return n, used, nil
}

// DecodePLC conceals one lost packet of the same duration as the last one
// and appends the concealment audio to out.
func (d *Decoder) DecodePLC(out []float32) (res []float32, err error) {
	defer func() {
		if noRecover {
			return
		}
		if r := recover(); r != nil {
			res = out
			err = fmt.Errorf("opus: concealment failed (%v)", r)
			d.Reset()
		}
	}()
	n := d.lastDuration
	start := len(out)
	out = growFloat(out, n*d.channels)
	pcm := out[start:]
	done := 0
	for done < n {
		// Conceal at most what the previous packet's frames covered.
		chunk := imin(n-done, imax(d.frameSize, d.fs/400))
		got, err := d.decodeFrame(nil, pcm[done*d.channels:], chunk)
		if err != nil {
			return out[:start], err
		}
		if got <= 0 {
			break
		}
		done += got
	}
	d.applyGain(pcm[:done*d.channels])
	return out[:start+done*d.channels], nil
}

func (d *Decoder) applyGain(pcm []float32) {
	if d.gain == 1 {
		return
	}
	for i := range pcm {
		pcm[i] *= d.gain
	}
}

func growFloat(s []float32, n int) []float32 {
	if cap(s)-len(s) >= n {
		s = s[:len(s)+n]
		clear(s[len(s)-n:])
		return s
	}
	ns := make([]float32, len(s)+n, len(s)+n+n/2)
	copy(ns, s)
	return ns
}

func smoothFade(in1, in2, out []float32, overlap, channels, fs int) {
	inc := 48000 / fs
	for c := 0; c < channels; c++ {
		for i := 0; i < overlap; i++ {
			w := window120[i*inc] * window120[i*inc]
			out[i*channels+c] = w*in2[i*channels+c] + (1-w)*in1[i*channels+c]
		}
	}
}

// decodeFrame decodes one Opus frame (data==nil: lost) of up to frameSize
// samples into pcm, returning the number of samples per channel produced.
func (d *Decoder) decodeFrame(data []byte, pcm []float32, frameSize int) (int, error) {
	F20 := d.fs / 50
	F10 := F20 >> 1
	F5 := F10 >> 1
	F2_5 := F5 >> 1
	C := d.channels
	if frameSize < F2_5 {
		return 0, ErrBadArg
	}
	if len(data) <= 1 {
		data = nil
		frameSize = imin(frameSize, d.frameSize)
	}
	var dec rangeDecoder
	var audiosize, mode, bandwidth int
	bandwidth = -1
	if data != nil {
		audiosize = d.frameSize
		mode = d.mode
		bandwidth = d.bandwidth
		dec.init(data)
	} else {
		audiosize = frameSize
		// Conceal with the last mode (CELT if we ended on CELT redundancy).
		mode = d.prevMode
		if d.prevRedundancy {
			mode = modeCELT
		}
		if mode == 0 {
			clear(pcm[:audiosize*C])
			return audiosize, nil
		}
		if audiosize > F20 {
			n := 0
			for audiosize > 0 {
				got, err := d.decodeFrame(nil, pcm[n*C:], imin(audiosize, F20))
				if err != nil || got <= 0 {
					return 0, errCorrupt
				}
				n += got
				audiosize -= got
			}
			return frameSize, nil
		} else if audiosize < F20 {
			if audiosize > F10 {
				audiosize = F10
			} else if mode != modeSILK && audiosize > F5 && audiosize < F10 {
				audiosize = F5
			}
		}
	}
	if cap(d.pcmTransition) < F5*C {
		d.pcmTransition = make([]float32, F5*C)
		d.redundant = make([]float32, F5*C)
	}
	pcmTransition := d.pcmTransition[:F5*C]
	redundantAudio := d.redundant[:F5*C]
	transition := false
	if data != nil && d.prevMode > 0 && ((mode == modeCELT && d.prevMode != modeCELT && !d.prevRedundancy) ||
		(mode != modeCELT && d.prevMode == modeCELT)) {
		transition = true
		if mode == modeCELT {
			if _, err := d.decodeFrame(nil, pcmTransition, imin(F5, audiosize)); err != nil {
				return 0, err
			}
		}
	}
	if audiosize > frameSize {
		return 0, ErrBadArg
	}
	frameSize = audiosize

	n := imax(F10, frameSize) * C
	if cap(d.pcmSilk) < n {
		d.pcmSilk = make([]int16, n)
	}
	pcmSilk := d.pcmSilk[:n]

	if mode != modeCELT {
		if d.prevMode == modeCELT {
			d.silk.reset()
		}
		d.silkCtl.payloadSizeMs = imax(10, 1000*audiosize/d.fs)
		if data != nil {
			d.silkCtl.nChannelsInternal = d.streamChannels
			if mode == modeSILK {
				switch d.bandwidth {
				case bwNarrow:
					d.silkCtl.internalSampleRate = 8000
				case bwMedium:
					d.silkCtl.internalSampleRate = 12000
				default:
					d.silkCtl.internalSampleRate = 16000
				}
			} else {
				d.silkCtl.internalSampleRate = 16000
			}
		}
		lostFlag := 0
		if data == nil {
			lostFlag = 1
		}
		decoded := 0
		for decoded < frameSize {
			ptr := pcmSilk[decoded*C:]
			nOut, err := d.silk.decode(&d.silkCtl, lostFlag, decoded == 0, &dec, ptr)
			if err != nil {
				if lostFlag != 0 {
					nOut = frameSize
					clear(ptr[:imin(len(ptr), frameSize*C)])
				} else {
					return 0, ErrInvalidPacket
				}
			}
			if nOut <= 0 {
				return 0, ErrInvalidPacket
			}
			decoded += nOut
		}
	}

	startBand := 0
	redundancy := false
	celtToSilk := false
	redundancyBytes := 0
	length := len(data)
	if mode != modeCELT && data != nil {
		hy := 0
		if mode == modeHybrid {
			hy = 20
		}
		if dec.tell()+17+hy <= 8*length {
			if mode == modeHybrid {
				redundancy = dec.bitLogp(12) != 0
			} else {
				redundancy = true
			}
			if redundancy {
				celtToSilk = dec.bitLogp(1) != 0
				if mode == modeHybrid {
					redundancyBytes = int(dec.uint(256)) + 2
				} else {
					redundancyBytes = length - ((dec.tell() + 7) >> 3)
				}
				length -= redundancyBytes
				if length*8 < dec.tell() || length < 0 {
					length = 0
					redundancyBytes = 0
					redundancy = false
				}
				dec.storage -= uint32(redundancyBytes)
			}
		}
	}
	if mode != modeCELT {
		startBand = 17
	}
	if bandwidth >= 0 {
		endBand := nbEBands
		switch bandwidth {
		case bwNarrow:
			endBand = 13
		case bwMedium, bwWide:
			endBand = 17
		case bwSuperWide:
			endBand = 19
		}
		d.celt.end = endBand
	}
	d.celt.streamChannels = d.streamChannels

	if redundancy {
		transition = false
	}
	if transition && mode != modeCELT {
		if _, err := d.decodeFrame(nil, pcmTransition, imin(F5, audiosize)); err != nil {
			return 0, err
		}
	}
	var redundantRng uint32
	if redundancy && celtToSilk {
		d.celt.start = 0
		d.celt.decode(data[length:length+redundancyBytes], redundantAudio, F5, nil, false)
		redundantRng = d.celt.rng
	}
	d.celt.start = startBand

	if mode != modeSILK {
		celtFrameSize := imin(F20, frameSize)
		if mode != d.prevMode && d.prevMode > 0 && !d.prevRedundancy {
			d.celt.reset()
		}
		var cdata []byte
		if data != nil {
			cdata = data[:length]
		}
		accum := mode != modeCELT
		if accum {
			clear(pcm[:frameSize*C])
		}
		if _, err := d.celt.decode(cdata, pcm, celtFrameSize, &dec, accum); err != nil && data != nil {
			return 0, err
		}
	} else {
		clear(pcm[:frameSize*C])
		if d.prevMode == modeHybrid && !(redundancy && celtToSilk && d.prevRedundancy) {
			d.celt.start = 0
			d.celt.decode([]byte{0xFF, 0xFF}, pcm, F2_5, nil, false)
		}
	}
	if mode != modeCELT {
		for i := 0; i < frameSize*C; i++ {
			pcm[i] += (1.0 / 32768) * float32(pcmSilk[i])
		}
	}
	if redundancy && !celtToSilk {
		d.celt.reset()
		d.celt.start = 0
		d.celt.decode(data[length:length+redundancyBytes], redundantAudio, F5, nil, false)
		redundantRng = d.celt.rng
		o := C * (frameSize - F2_5)
		smoothFade(pcm[o:], redundantAudio[C*F2_5:], pcm[o:], F2_5, C, d.fs)
	}
	if redundancy && celtToSilk && (d.prevMode != modeSILK || d.prevRedundancy) {
		copy(pcm[:F2_5*C], redundantAudio[:F2_5*C])
		smoothFade(redundantAudio[C*F2_5:], pcm[C*F2_5:], pcm[C*F2_5:], F2_5, C, d.fs)
	}
	if transition {
		if audiosize >= F5 {
			copy(pcm[:F2_5*C], pcmTransition[:F2_5*C])
			smoothFade(pcmTransition[C*F2_5:], pcm[C*F2_5:], pcm[C*F2_5:], F2_5, C, d.fs)
		} else {
			smoothFade(pcmTransition, pcm, pcm, F2_5, C, d.fs)
		}
	}
	if length <= 1 {
		d.rangeFinal = 0
	} else {
		d.rangeFinal = dec.rng ^ redundantRng
	}
	d.prevMode = mode
	d.prevRedundancy = redundancy && !celtToSilk
	return audiosize, nil
}

// --- packet parsing ---

type packetInfo struct {
	count  int
	frames [48][]byte
}

func tocMode(toc byte) int {
	switch {
	case toc&0x80 != 0:
		return modeCELT
	case toc&0x60 == 0x60:
		return modeHybrid
	}
	return modeSILK
}

func tocBandwidth(toc byte) int {
	switch {
	case toc&0x80 != 0:
		bw := bwMedium + int(toc>>5&3)
		if bw == bwMedium {
			bw = bwNarrow
		}
		return bw
	case toc&0x60 == 0x60:
		if toc&0x10 != 0 {
			return bwFull
		}
		return bwSuperWide
	}
	return bwNarrow + int(toc>>5&3)
}

func tocSamplesPerFrame(toc byte, fs int) int {
	switch {
	case toc&0x80 != 0:
		return (fs << (toc >> 3 & 3)) / 400
	case toc&0x60 == 0x60:
		if toc&0x08 != 0 {
			return fs / 50
		}
		return fs / 100
	}
	a := int(toc >> 3 & 3)
	if a == 3 {
		return fs * 60 / 1000
	}
	return (fs << uint(a)) / 100
}

func parseSize(data []byte) (size, n int) {
	if len(data) < 1 {
		return -1, -1
	}
	if data[0] < 252 {
		return int(data[0]), 1
	}
	if len(data) < 2 {
		return -1, -1
	}
	return 4*int(data[1]) + int(data[0]), 2
}

// parsePacket splits a packet into its frames (RFC 6716 section 3.2 and,
// for selfDelimited, Appendix B), including the RFC 8251 padding fix. It
// also returns the number of bytes the packet occupies.
func parsePacket(data []byte, selfDelimited bool, p *packetInfo) (*packetInfo, int, error) {
	if len(data) < 1 {
		return nil, 0, ErrInvalidPacket
	}
	data0 := len(data)
	framesize := tocSamplesPerFrame(data[0], 48000)
	toc := data[0]
	data = data[1:]
	var size [48]int
	length := len(data)
	lastSize := length
	count := 0
	cbr := false
	switch toc & 3 {
	case 0:
		count = 1
	case 1:
		count = 2
		cbr = true
		if !selfDelimited {
			if length&1 != 0 {
				return nil, 0, ErrInvalidPacket
			}
			lastSize = length / 2
			size[0] = lastSize
		}
	case 2:
		count = 2
		s, n := parseSize(data)
		if n < 0 {
			return nil, 0, ErrInvalidPacket
		}
		length -= n
		if s < 0 || s > length {
			return nil, 0, ErrInvalidPacket
		}
		data = data[n:]
		size[0] = s
		lastSize = length - s
	default:
		if length < 1 {
			return nil, 0, ErrInvalidPacket
		}
		ch := data[0]
		data = data[1:]
		length--
		count = int(ch & 0x3F)
		if count <= 0 || framesize*count > 5760 {
			return nil, 0, ErrInvalidPacket
		}
		if ch&0x40 != 0 {
			for {
				if length <= 0 {
					return nil, 0, ErrInvalidPacket
				}
				pb := int(data[0])
				data = data[1:]
				length--
				if pb == 255 {
					length -= 254
				} else {
					length -= pb
				}
				if pb != 255 {
					break
				}
			}
		}
		if length < 0 {
			return nil, 0, ErrInvalidPacket
		}
		cbr = ch&0x80 == 0
		if !cbr {
			lastSize = length
			for i := 0; i < count-1; i++ {
				s, n := parseSize(data[:imin(len(data), length)])
				if n < 0 {
					return nil, 0, ErrInvalidPacket
				}
				length -= n
				if s < 0 || s > length {
					return nil, 0, ErrInvalidPacket
				}
				data = data[n:]
				size[i] = s
				lastSize -= n + s
			}
			if lastSize < 0 {
				return nil, 0, ErrInvalidPacket
			}
		} else if !selfDelimited {
			lastSize = length / count
			if lastSize*count != length {
				return nil, 0, ErrInvalidPacket
			}
			for i := 0; i < count-1; i++ {
				size[i] = lastSize
			}
		}
	}
	if selfDelimited {
		s, n := parseSize(data[:imin(len(data), imax(length, 0))])
		if n < 0 {
			return nil, 0, ErrInvalidPacket
		}
		length -= n
		if s < 0 || s > length {
			return nil, 0, ErrInvalidPacket
		}
		data = data[n:]
		if cbr {
			if s*count > length {
				return nil, 0, ErrInvalidPacket
			}
			for i := 0; i < count-1; i++ {
				size[i] = s
			}
		} else if s > lastSize {
			return nil, 0, ErrInvalidPacket
		}
		size[count-1] = s
	} else {
		if lastSize > 1275 {
			return nil, 0, ErrInvalidPacket
		}
		size[count-1] = lastSize
	}
	p.count = count
	off := 0
	for i := 0; i < count; i++ {
		if size[i] > 1275 || off+size[i] > len(data) {
			return nil, 0, ErrInvalidPacket
		}
		p.frames[i] = data[off : off+size[i]]
		off += size[i]
	}
	used := data0 - len(data) + off
	return p, used, nil
}

// PacketDuration returns the number of samples per channel at 48 kHz that
// the packet decodes to.
func PacketDuration(packet []byte) (int, error) {
	if len(packet) < 1 {
		return 0, ErrInvalidPacket
	}
	var count int
	switch packet[0] & 3 {
	case 0:
		count = 1
	case 1, 2:
		count = 2
	default:
		if len(packet) < 2 {
			return 0, ErrInvalidPacket
		}
		count = int(packet[1] & 0x3F)
	}
	n := count * tocSamplesPerFrame(packet[0], 48000)
	if count == 0 || n > 5760 {
		return 0, ErrInvalidPacket
	}
	return n, nil
}
