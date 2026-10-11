package h264

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"strings"
	"testing"
)

// TestDecodePOC2IntraInPMatchesReference decodes a Main-profile CAVLC stream
// whose picture order count is type 2 and which contains intra macroblocks
// inside P slices, asserting every frame is byte-identical to OpenH264 (the
// per-frame hashes in the sibling .txt come from tools/media-poc/cmd/oh264-hashes).
//
// It is the regression guard for the neighbour intra prediction-mode
// derivation: an inter P_L0_16x16 macroblock and an intra I_NxN macroblock both
// carry mb_type 0, so without the isIntraNxN() check an inter neighbour was
// mistaken for an intra4x4 one and the predicted mode was taken from stale
// (zero) data — corrupting every intra macroblock that sits next to an inter
// P_L0_16x16 macroblock inside a P/B slice.
func TestDecodePOC2IntraInPMatchesReference(t *testing.T) {
	const (
		stream = "testdata/poc2_intra_in_p_64x64.h264"
		hashes = "testdata/poc2_intra_in_p_64x64.txt"
	)
	data, err := os.ReadFile(stream)
	if err != nil {
		t.Skipf("missing testdata: %v", err)
	}
	raw, err := os.ReadFile(hashes)
	if err != nil {
		t.Skipf("missing hashes: %v", err)
	}
	var want []string
	for _, ln := range strings.Split(strings.TrimSpace(string(raw)), "\n") {
		if ln != "" {
			want = append(want, ln)
		}
	}

	frames, err := DecodeAnnexB(data)
	if err != nil {
		t.Fatalf("DecodeAllAnnexB: %v", err)
	}
	if len(frames) != len(want) {
		t.Fatalf("decoded %d frames, want %d", len(frames), len(want))
	}
	for i, f := range frames {
		w, h := f.Width, f.Height
		cw, ch := chromaSize(w), chromaSize(h)
		checks := []struct {
			name string
			data []byte
		}{
			{"Y", f.Y.Pix[:w*h]},
			{"Cb", f.Cb.Pix[:cw*ch]},
			{"Cr", f.Cr.Pix[:cw*ch]},
		}
		for _, ck := range checks {
			got := sha256.Sum256(ck.data)
			exp := hashField(want[i], ck.name)
			if hex.EncodeToString(got[:]) != exp {
				t.Errorf("frame %d %s mismatch:\n got  %x\n want %s", i, ck.name, got, exp)
			}
		}
	}
}

// hashField extracts "key=value" from one oracle hash line.
func hashField(line, key string) string {
	for _, f := range strings.Fields(line) {
		if strings.HasPrefix(f, key+"=") {
			return f[len(key)+1:]
		}
	}
	return ""
}
