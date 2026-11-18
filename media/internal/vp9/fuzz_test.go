package vp9

import (
	"testing"
)

// FuzzDecode feeds corrupted packets after a valid keyframe: decoding
// must return (possibly an error), never panic or hang.
func FuzzDecode(f *testing.F) {
	for _, c := range clips {
		bs := readBlocks(f, c.name)
		for i := 0; i < len(bs) && i < 3; i++ {
			f.Add(bs[i])
		}
	}
	key := readBlocks(f, "vp9p0_opus_160x90")[0]
	maxPixels = 1 << 20 // keep corrupt huge frame sizes cheap
	f.Fuzz(func(t *testing.T, data []byte) {
		d := New()
		_ = d.Decode(data)
		for d.Next() != nil {
		}
		d = New()
		_ = d.Decode(key)
		_ = d.Decode(data)
		_ = d.Decode(data)
		for d.Next() != nil {
		}
	})
}

// TestCorruptPackets flips bytes and truncates every fixture packet.
func TestCorruptPackets(t *testing.T) {
	for _, c := range clips {
		bs := readBlocks(t, c.name)
		for i, b := range bs {
			d := New()
			for _, p := range bs[:i] {
				_ = d.Decode(p)
			}
			for k := 0; k < len(b); k += 1 + len(b)/7 {
				m := append([]byte(nil), b...)
				m[k] ^= 0x5a
				_ = d.Decode(m)
				_ = d.Decode(b[:k])
			}
		}
	}
}
