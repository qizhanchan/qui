package mp4x

import (
	"encoding/binary"
	"os"
	"testing"
)

// TestAV1KeyFrame classifies the temporal units of the av1 package's
// test stream: only the first starts with a shown key frame.
func TestAV1KeyFrame(t *testing.T) {
	b, err := os.ReadFile("../av1/testdata/ra.ivf")
	if err != nil {
		t.Skip(err)
	}
	n := 0
	for p := int(binary.LittleEndian.Uint16(b[6:])); p+12 <= len(b); n++ {
		size := int(binary.LittleEndian.Uint32(b[p:]))
		tu := b[p+12 : p+12+size]
		if got := av1KeyFrame(tu); got != (n == 0) {
			t.Errorf("TU %d: key frame %v", n, got)
		}
		p += 12 + size
	}
	if n == 0 {
		t.Fatal("no frames")
	}
}
