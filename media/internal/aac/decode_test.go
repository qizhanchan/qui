package aac

import (
	"encoding/binary"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestADTSCorpus decodes every *.aac (ADTS) in $AAC_CORPUS and compares it
// with the sibling *.f32 (FFmpeg's float decode).
func TestADTSCorpus(t *testing.T) {
	dir := os.Getenv("AAC_CORPUS")
	if dir == "" {
		t.Skip("AAC_CORPUS not set")
	}
	files, _ := filepath.Glob(filepath.Join(dir, "*.aac"))
	for _, f := range files {
		name := strings.TrimSuffix(filepath.Base(f), ".aac")
		t.Run(name, func(t *testing.T) {
			data, _ := os.ReadFile(f)
			refRaw, err := os.ReadFile(strings.TrimSuffix(f, ".aac") + ".f32")
			if err != nil {
				t.Skip("no reference")
			}
			ref := make([]float32, len(refRaw)/4)
			for i := range ref {
				ref[i] = math.Float32frombits(binary.LittleEndian.Uint32(refRaw[4*i:]))
			}
			h, _, err := ParseADTS(data)
			if err != nil {
				t.Fatal(err)
			}
			cfg := &Config{ObjectType: h.ObjectType, SampleRateIndex: h.SampleRateIndex, SampleRate: h.SampleRate, ChannelConfig: h.Channels, Channels: h.Channels, FrameLength: 1024}
			d, err := NewDecoder(cfg)
			if err != nil {
				t.Fatal(err)
			}
			aus, err := RawBlocks(data)
			if err != nil {
				t.Fatal(err)
			}
			var out []float32
			for i, au := range aus {
				if out, err = d.Decode(au, out); err != nil {
					t.Fatalf("frame %d: %v", i, err)
				}
			}
			if len(out) != len(ref) {
				t.Logf("length: got %d samples, ffmpeg %d", len(out), len(ref))
			}
			n := min(len(out), len(ref))
			var sig, noise float64
			maxErr := 0.0
			for i := 0; i < n; i++ {
				e := float64(out[i] - ref[i])
				sig += float64(ref[i]) * float64(ref[i])
				noise += e * e
				maxErr = math.Max(maxErr, math.Abs(e))
			}
			snr := 10 * math.Log10(sig/max(noise, 1e-30))
			t.Logf("SNR %.1f dB, max abs err %.2g", snr, maxErr)
			if snr < 90 {
				t.Errorf("SNR %.1f dB too low", snr)
			}
		})
	}
}
