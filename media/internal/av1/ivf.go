package av1

import (
	"encoding/binary"
	"fmt"
)

// ReadIVF splits an IVF file into its frames (temporal units).
func ReadIVF(b []byte) (frames [][]byte, w, h int, err error) {
	if len(b) < 32 || string(b[:4]) != "DKIF" {
		return nil, 0, 0, fmt.Errorf("%w: not an IVF file", ErrInvalid)
	}
	hl := int(binary.LittleEndian.Uint16(b[6:]))
	w, h = int(binary.LittleEndian.Uint16(b[12:])), int(binary.LittleEndian.Uint16(b[14:]))
	if hl < 32 || hl > len(b) {
		return nil, 0, 0, fmt.Errorf("%w: bad IVF header", ErrInvalid)
	}
	for p := hl; p+12 <= len(b); {
		n := int(binary.LittleEndian.Uint32(b[p:]))
		p += 12
		if n > len(b)-p {
			return frames, w, h, fmt.Errorf("%w: truncated IVF frame", ErrInvalid)
		}
		frames = append(frames, b[p:p+n])
		p += n
	}
	return frames, w, h, nil
}
