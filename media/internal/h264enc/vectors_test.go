package h264enc

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"strings"
	"testing"
)

// TestStreamVectors pins the SHA-256 of the encoder's output for a set of
// configurations (testdata/streams.txt), so any bitstream change is
// deliberate. NEON and purego builds must produce identical streams.
// Regenerate with H264ENC_UPDATE=1 after an intended change.
func TestStreamVectors(t *testing.T) {
	frames := sourceFrames(t, "vt_main_p8x8.264")
	var lines []string
	for _, tc := range testConfigs {
		cfg := tc.cfg
		cfg.Width, cfg.Height = frames[0].Rect.Dx(), frames[0].Rect.Dy()
		r := encodeAll(t, cfg, frames)
		sum := sha256.Sum256(r.stream)
		lines = append(lines, fmt.Sprintf("%s %d %s", tc.name, len(r.stream), hex.EncodeToString(sum[:])))
	}
	got := strings.Join(lines, "\n") + "\n"
	if os.Getenv("H264ENC_UPDATE") != "" {
		if err := os.WriteFile("testdata/streams.txt", []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile("testdata/streams.txt")
	if err != nil {
		t.Fatal(err)
	}
	if got != string(want) {
		t.Errorf("stream hashes changed (H264ENC_UPDATE=1 to accept):\n got:\n%s\nwant:\n%s", got, want)
	}
}
