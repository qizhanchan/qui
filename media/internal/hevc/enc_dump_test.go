package hevc

import (
	"os"
	"path/filepath"
	"testing"
)

// TestEncoderDump writes each encConfigs stream to HEVC_DUMP (opt-in), for
// checks with external decoders.
func TestEncoderDump(t *testing.T) {
	dir := os.Getenv("HEVC_DUMP")
	if dir == "" {
		t.Skip("HEVC_DUMP not set")
	}
	frames := testSource(t, 176, 144, 4)
	for _, c := range encConfigs {
		cfg := c.cfg
		cfg.Width, cfg.Height = 176, 144
		stream, _ := encodeAll(t, cfg, frames)
		if err := os.WriteFile(filepath.Join(dir, c.name+".265"), stream, 0o644); err != nil {
			t.Fatal(err)
		}
	}
}
