package av1

import "fmt"

// ConfigRecord builds the AV1CodecConfigurationRecord (the av1C box
// payload, AV1-ISOBMFF 2.3) for a sequence header OBU (with its OBU
// header and size field), which becomes the record's configOBUs.
func ConfigRecord(seqOBU []byte) ([]byte, error) {
	obus, err := splitOBUs(seqOBU)
	if err != nil || len(obus) == 0 || obus[0].typ != obuSequenceHeader {
		return nil, fmt.Errorf("%w: not a sequence header OBU", ErrInvalid)
	}
	s, err := ParseSequenceHeader(obus[0].payload)
	if err != nil {
		return nil, err
	}
	b2 := byte(s.Profile<<5 | s.opLevel[0])
	var b3 byte
	if s.opTier[0] != 0 {
		b3 |= 0x80
	}
	if s.BitDepth > 8 {
		b3 |= 0x40
	}
	if s.BitDepth == 12 {
		b3 |= 0x20
	}
	if s.MonoChrome {
		b3 |= 0x10
	}
	b3 |= byte(s.SubX<<3 | s.SubY<<2 | s.ChromaSamplePos)
	rec := []byte{0x81, b2, b3, 0}
	return append(rec, seqOBU...), nil
}

// SplitSequenceHeader returns the first sequence header OBU (with header)
// in a run of OBUs, or nil.
func SplitSequenceHeader(b []byte) []byte {
	obus, err := splitOBUs(b)
	if err != nil {
		return nil
	}
	off := 0
	for _, o := range obus {
		n := o.hdrLen + len(o.payload)
		if o.typ == obuSequenceHeader {
			return b[off : off+n]
		}
		off += n
	}
	return nil
}

// StripTemporalDelimiters returns the OBUs of b without temporal
// delimiters (the form MP4 samples carry).
func StripTemporalDelimiters(b []byte) []byte {
	obus, err := splitOBUs(b)
	if err != nil {
		return b
	}
	out := make([]byte, 0, len(b))
	off := 0
	for _, o := range obus {
		n := o.hdrLen + len(o.payload)
		if o.typ != obuTemporalDelimiter {
			out = append(out, b[off:off+n]...)
		}
		off += n
	}
	return out
}
