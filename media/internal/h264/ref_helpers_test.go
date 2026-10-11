package h264

import (
	"encoding/hex"
	"strings"
	"testing"
)

// Reference vectors inherited from the first decoder: IDR streams verified
// against hi264, and Baseline P / POC type 2 streams verified against
// OpenH264.

func mustHex(t *testing.T, s string) []byte {
	t.Helper()
	b, err := hex.DecodeString(strings.ReplaceAll(s, " ", ""))
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func chromaSize(n int) int { return (n + 1) / 2 }

// decodeFirst decodes a stream and returns its first picture.
func decodeFirst(data []byte) (*Frame, error) {
	frames, err := DecodeAnnexB(data)
	if len(frames) == 0 {
		if err == nil {
			err = errf("no picture decoded")
		}
		return nil, err
	}
	return frames[0], err
}
