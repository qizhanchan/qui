package hevc

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"strings"
	"testing"
)

// TestEncoderStreams pins the SHA-256 of every encConfigs stream (9 frames
// of the committed content) in testdata/encstreams.txt, so any change to
// the encoder's output is deliberate. NEON and -tags purego builds produce
// the same streams. HEVC_UPDATE=1 rewrites the file.
func TestEncoderStreams(t *testing.T) {
	frames := rcSource(t, 9)
	got := map[string]string{}
	var names []string
	for _, c := range encConfigs {
		cfg := c.cfg
		cfg.Width, cfg.Height = 176, 144
		stream, _ := encodeAll(t, cfg, frames)
		sum := sha256.Sum256(stream)
		got[c.name] = hex.EncodeToString(sum[:])
		names = append(names, c.name)
	}
	const path = "testdata/encstreams.txt"
	if os.Getenv("HEVC_UPDATE") != "" {
		var b strings.Builder
		for _, n := range names {
			fmt.Fprintf(&b, "%s %s\n", n, got[n])
		}
		if err := os.WriteFile(path, []byte(b.String()), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	want := map[string]string{}
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		if fs := strings.Fields(sc.Text()); len(fs) == 2 {
			want[fs[0]] = fs[1]
		}
	}
	for _, n := range names {
		if want[n] != got[n] {
			t.Errorf("%s: stream %s, pinned %s", n, got[n], want[n])
		}
	}
}
