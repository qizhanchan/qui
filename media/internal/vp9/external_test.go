package vp9

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestExternalClips compares every clip.webm in $VP9_EXTRA_DIR with the
// FFmpeg decode clip.yuv beside it and logs the decode speed.
func TestExternalClips(t *testing.T) {
	dir := os.Getenv("VP9_EXTRA_DIR")
	if dir == "" {
		t.Skip("VP9_EXTRA_DIR not set")
	}
	files, _ := filepath.Glob(filepath.Join(dir, "*.webm"))
	for _, f := range files {
		name := strings.TrimSuffix(filepath.Base(f), ".webm")
		t.Run(name, func(t *testing.T) {
			blocks := readBlocksPath(t, f)
			ref, err := os.ReadFile(strings.TrimSuffix(f, ".webm") + ".yuv")
			if err != nil {
				t.Skip("no reference")
			}
			d := New()
			d.stats = &decodeStats{}
			n := 0
			var dur time.Duration
			for i, b := range blocks {
				st := time.Now()
				if err := d.Decode(b); err != nil {
					t.Fatalf("block %d: %v", i, err)
				}
				dur += time.Since(st)
				for fr := d.Next(); fr != nil; fr = d.Next() {
					fb := frameBytes(fr)
					if len(ref) < len(fb) {
						t.Fatalf("frame %d: reference too short", n)
					}
					if !bytes.Equal(fb, ref[:len(fb)]) {
						reportDiff(t, n, fr, fb, ref[:len(fb)])
					}
					ref = ref[len(fb):]
					Recycle(fr)
					n++
				}
			}
			if len(ref) != 0 {
				t.Errorf("%d bytes of reference left after %d frames", len(ref), n)
			}
			t.Logf("%d frames bit-exact, %.1f fps; %v", n, float64(n)/dur.Seconds(), d.stats)
		})
	}
}
