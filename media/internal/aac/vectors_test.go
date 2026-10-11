package aac

import (
	"encoding/binary"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestVectors decodes the committed ADTS streams and compares them with
// FFmpeg's 16-bit decode (testdata/*.s16) to within 1 LSB.
func TestVectors(t *testing.T) {
	files, _ := filepath.Glob("testdata/*.aac")
	if len(files) == 0 {
		t.Fatal("no vectors")
	}
	for _, f := range files {
		t.Run(strings.TrimSuffix(filepath.Base(f), ".aac"), func(t *testing.T) {
			data, _ := os.ReadFile(f)
			raw, err := os.ReadFile(strings.TrimSuffix(f, ".aac") + ".s16")
			if err != nil {
				t.Fatal(err)
			}
			h, _, err := ParseADTS(data)
			if err != nil {
				t.Fatal(err)
			}
			d, err := NewDecoder(&Config{ObjectType: h.ObjectType, SampleRateIndex: h.SampleRateIndex,
				SampleRate: h.SampleRate, ChannelConfig: h.Channels, Channels: h.Channels, FrameLength: 1024})
			if err != nil {
				t.Fatal(err)
			}
			aus, err := RawBlocks(data)
			if err != nil {
				t.Fatal(err)
			}
			var out []int16
			for i, au := range aus {
				if out, err = d.DecodeInt16(au, out); err != nil {
					t.Fatalf("frame %d: %v", i, err)
				}
			}
			ref := make([]int16, len(raw)/2)
			for i := range ref {
				ref[i] = int16(binary.LittleEndian.Uint16(raw[2*i:]))
			}
			if len(out) != len(ref) {
				t.Fatalf("decoded %d samples, want %d", len(out), len(ref))
			}
			bad := 0
			for i := range ref {
				if d := int(out[i]) - int(ref[i]); d > 1 || d < -1 {
					bad++
				}
			}
			if bad > 0 {
				t.Errorf("%d of %d samples differ from FFmpeg by more than 1 LSB", bad, len(ref))
			}
		})
	}
}
