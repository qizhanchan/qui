// Package mp4util holds the small ISO-BMFF helpers the media foundation
// POCs share: track selection, sample-table lookup, AVCC <-> Annex-B
// conversion, and codec-config extraction.
//
// These are exactly the pieces github.com/abema/go-mp4 gives you the
// ingredients for but deliberately leaves you to assemble — the
// container layer of the planned media foundation.
package mp4util

import (
	"fmt"
	"io"
	"os"

	mp4 "github.com/abema/go-mp4"
)

// VideoTrack returns the first H.264 (avc1) video track, or nil.
func VideoTrack(info *mp4.ProbeInfo) *mp4.Track {
	for _, t := range info.Tracks {
		if t.Codec == mp4.CodecAVC1 {
			return t
		}
	}
	return nil
}

// AudioTrack returns the first AAC (mp4a) track, or nil.
func AudioTrack(info *mp4.ProbeInfo) *mp4.Track {
	for _, t := range info.Tracks {
		if t.Codec == mp4.CodecMP4A {
			return t
		}
	}
	return nil
}

// SampleOffset walks the chunk table (stsc + stco/co64 + stsz) to find
// the absolute file offset and size of sample idx.
func SampleOffset(t *mp4.Track, idx int) (uint64, uint32, error) {
	if idx < 0 || idx >= len(t.Samples) {
		return 0, 0, fmt.Errorf("sample %d out of range (0..%d)", idx, len(t.Samples)-1)
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
	return 0, 0, fmt.Errorf("sample %d not covered by any chunk", idx)
}

// ReadSample reads sample idx of track t from f.
func ReadSample(f *os.File, t *mp4.Track, idx int) ([]byte, error) {
	off, size, err := SampleOffset(t, idx)
	if err != nil {
		return nil, err
	}
	buf := make([]byte, size)
	if _, err := f.ReadAt(buf, int64(off)); err != nil {
		return nil, err
	}
	return buf, nil
}

// SplitAVCC splits a length-prefixed (ISO-BMFF / AVC) sample into raw
// NAL units. lengthSize is 1..4 bytes, big-endian.
func SplitAVCC(data []byte, lengthSize int) ([][]byte, error) {
	if lengthSize < 1 || lengthSize > 4 {
		return nil, fmt.Errorf("bad lengthSize %d", lengthSize)
	}
	var out [][]byte
	for p := 0; p+lengthSize <= len(data); {
		n := 0
		for i := 0; i < lengthSize; i++ {
			n = n<<8 | int(data[p+i])
		}
		p += lengthSize
		if n <= 0 || p+n > len(data) {
			break
		}
		out = append(out, data[p:p+n])
		p += n
	}
	return out, nil
}

// AnnexB assembles one Annex-B access unit from raw NAL units, injecting
// SPS/PPS first when withPS is true.
func AnnexB(sps, pps, nalus [][]byte, withPS bool) []byte {
	sc := []byte{0x00, 0x00, 0x00, 0x01}
	var b []byte
	add := func(n []byte) { b = append(b, sc...); b = append(b, n...) }
	if withPS {
		for _, s := range sps {
			add(s)
		}
		for _, p := range pps {
			add(p)
		}
	}
	for _, n := range nalus {
		add(n)
	}
	return b
}

// AVCCParameterSets extracts the SPS/PPS NAL units from the video
// track's avcC box.
func AVCCParameterSets(r io.ReadSeeker) (sps, pps [][]byte, err error) {
	boxes, err := mp4.ExtractBoxWithPayload(r, nil, mp4.BoxPath{
		mp4.BoxTypeMoov(), mp4.BoxTypeTrak(), mp4.BoxTypeMdia(),
		mp4.BoxTypeMinf(), mp4.BoxTypeStbl(), mp4.BoxTypeStsd(),
		mp4.BoxTypeAvc1(), mp4.BoxTypeAvcC(),
	})
	if err != nil {
		return nil, nil, err
	}
	if len(boxes) == 0 {
		return nil, nil, fmt.Errorf("avc1/avcC box not found")
	}
	avcC, ok := boxes[0].Payload.(*mp4.AVCDecoderConfiguration)
	if !ok {
		return nil, nil, fmt.Errorf("unexpected avcC payload %T", boxes[0].Payload)
	}
	for _, ps := range avcC.SequenceParameterSets {
		sps = append(sps, ps.NALUnit)
	}
	for _, ps := range avcC.PictureParameterSets {
		pps = append(pps, ps.NALUnit)
	}
	return sps, pps, nil
}

// AudioSpecificConfig extracts the AAC AudioSpecificConfig bytes from the
// audio track's esds box.
func AudioSpecificConfig(r io.ReadSeeker) ([]byte, error) {
	boxes, err := mp4.ExtractBoxWithPayload(r, nil, mp4.BoxPath{
		mp4.BoxTypeMoov(), mp4.BoxTypeTrak(), mp4.BoxTypeMdia(),
		mp4.BoxTypeMinf(), mp4.BoxTypeStbl(), mp4.BoxTypeStsd(),
		mp4.BoxTypeMp4a(), mp4.BoxTypeEsds(),
	})
	if err != nil {
		return nil, err
	}
	if len(boxes) == 0 {
		return nil, fmt.Errorf("mp4a/esds box not found")
	}
	esds, ok := boxes[0].Payload.(*mp4.Esds)
	if !ok {
		return nil, fmt.Errorf("unexpected esds payload %T", boxes[0].Payload)
	}
	for _, d := range esds.Descriptors {
		if d.Tag == mp4.DecSpecificInfoTag {
			return d.Data, nil
		}
	}
	return nil, fmt.Errorf("DecoderSpecificInfo (0x05) descriptor not found")
}

// Seconds converts a media duration + timescale to seconds.
func Seconds(dur uint64, timescale uint32) float64 {
	if timescale == 0 {
		return 0
	}
	return float64(dur) / float64(timescale)
}

// FirstN returns at most n leading elements of s.
func FirstN(s []int, n int) []int {
	if len(s) > n {
		return s[:n]
	}
	return s
}
