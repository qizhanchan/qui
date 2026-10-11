//go:build cgo

package media

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"testing"
	"time"
)

// e2eClips are 1 s at 24 fps plus AAC-LC stereo, as written by FFmpeg with
// an edit list:
//   - bframes_aac_160x90.mp4: x264 High profile (B-pyramid, 3 refs,
//     weighted P);
//   - hevc_aac_160x90.mp4: the same picture re-encoded with libx265 (3 B
//     frames, closed GOP of 12), tagged hvc1.
//
// The hashes are FFmpeg's decode of all 24 frames as I420 (VideoToolbox
// agrees for the HEVC clip).
var e2eClips = []struct {
	file, codec, hash string
}{
	{"testdata/bframes_aac_160x90.mp4", "h264", "12751dbb2428437074dae7f56db9f14707cd013c6353d3afbc4d6d4942556fc3"},
	{"testdata/hevc_aac_160x90.mp4", "hevc", "e2cbb5109bb5d1e0deb1a3e4f3a5572eb642d16caedb5737e516c48fcd1d8f3b"},
}

const e2eFrames = 24

func TestGolangBackendMatchesFFmpeg(t *testing.T) {
	for _, c := range e2eClips {
		t.Run(c.codec, func(t *testing.T) { testMatchesFFmpeg(t, c.file, c.codec, c.hash) })
	}
}

func testMatchesFFmpeg(t *testing.T, file, codec, hash string) {
	src, err := golangBackend{}.OpenSource(file)
	if err != nil {
		t.Fatalf("OpenSource: %v", err)
	}
	defer src.Close()
	vt := src.VideoTrack()
	if vt.Width != 160 || vt.Height != 90 || vt.Codec != codec {
		t.Fatalf("track %s %dx%d", vt.Codec, vt.Width, vt.Height)
	}
	h := sha256.New()
	var last time.Duration = -1
	n := 0
	for {
		f, err := src.NextVideoFrame(context.Background())
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		gf := f.(*golangVideoFrame)
		if want := time.Duration(n) * time.Second / 24; absDur(f.PTS()-want) > time.Millisecond {
			t.Errorf("frame %d PTS = %v, want %v", n, f.PTS(), want)
		}
		if f.PTS() <= last {
			t.Errorf("frame %d PTS %v not increasing", n, f.PTS())
		}
		last = f.PTS()
		h.Write(gf.frame.Y.Pix)
		h.Write(gf.frame.Cb.Pix)
		h.Write(gf.frame.Cr.Pix)
		f.Release()
		n++
	}
	if n != e2eFrames {
		t.Fatalf("decoded %d frames, want %d", n, e2eFrames)
	}
	if got := hex.EncodeToString(h.Sum(nil)); got != hash {
		t.Errorf("decoded planes differ from FFmpeg:\n got  %s\n want %s", got, hash)
	}
}

func TestGolangBackendPreciseSeek(t *testing.T) {
	for _, c := range e2eClips {
		t.Run(c.codec, func(t *testing.T) { testPreciseSeek(t, c.file) })
	}
}

func testPreciseSeek(t *testing.T, file string) {
	src, err := golangBackend{}.OpenSource(file)
	if err != nil {
		t.Fatal(err)
	}
	defer src.Close()
	for _, target := range []time.Duration{500 * time.Millisecond, 0, 900 * time.Millisecond, 250 * time.Millisecond} {
		if err := src.SeekVideo(target, true); err != nil {
			t.Fatal(err)
		}
		f, err := src.NextVideoFrame(context.Background())
		if err != nil {
			t.Fatalf("seek %v: %v", target, err)
		}
		frameDur := time.Second / 24
		if f.PTS() > target || f.PTS()+frameDur <= target {
			t.Errorf("seek %v: first frame PTS %v does not cover the target", target, f.PTS())
		}
		f.Release()
	}
	sd, err := src.OpenScrubDecoder()
	if err != nil {
		t.Fatal(err)
	}
	for _, target := range []time.Duration{300 * time.Millisecond, 350 * time.Millisecond, 100 * time.Millisecond, 950 * time.Millisecond} {
		f, err := sd.DecodeAt(target)
		if err != nil {
			t.Fatalf("DecodeAt %v: %v", target, err)
		}
		if f.PTS() > target || f.PTS()+time.Second/24 <= target {
			t.Errorf("DecodeAt %v returned PTS %v", target, f.PTS())
		}
		f.Release()
	}
}

func absDur(d time.Duration) time.Duration {
	if d < 0 {
		return -d
	}
	return d
}
