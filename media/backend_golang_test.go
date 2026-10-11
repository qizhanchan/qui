//go:build cgo

package media

import (
	"context"
	"errors"
	"io"
	"os"
	"testing"
	"time"
)

const (
	golangMP4     = "testdata/tiny_p_64x64.mp4"
	golangFragMP4 = "testdata/frag_p_64x64.mp4"
)

func TestGolangBackendCapabilities(t *testing.T) {
	caps := golangBackend{}.Capabilities()
	found := false
	for _, c := range caps.VideoCodecs {
		if c == "h264" {
			found = true
		}
	}
	if !found {
		t.Fatalf("VideoCodecs = %v, want to contain h264", caps.VideoCodecs)
	}
}

// TestGolangBackendDecodesMP4 drives the pure-Go pipeline end to end: MP4
// demux → H.264 decode → I420→RGBA, over the committed tiny Main-profile
// clip (5 frames, no B frames). The decoder itself is verified plane-exact
// against OpenH264 elsewhere; this checks the integration: track metadata,
// frame count, dimensions, timestamps and the RGBA buffer.
func TestGolangBackendDecodesMP4(t *testing.T) {
	if _, err := os.Stat(golangMP4); err != nil {
		t.Skipf("missing testdata: %v", err)
	}
	restore := swapBackend(t, golangBackend{})
	defer restore()

	src, err := golangBackend{}.OpenSource(golangMP4)
	if err != nil {
		t.Fatalf("OpenSource: %v", err)
	}
	defer src.Close()

	vt := src.VideoTrack()
	if vt == nil {
		t.Fatal("VideoTrack is nil")
	}
	if vt.Codec != "h264" || vt.Width != 64 || vt.Height != 64 {
		t.Fatalf("VideoTrack = %+v, want h264 64x64", vt)
	}
	if vt.Duration <= 0 {
		t.Errorf("Duration = %v, want > 0", vt.Duration)
	}
	if src.AudioTrack() != nil {
		t.Errorf("AudioTrack = %+v, want nil (AAC not wired yet)", src.AudioTrack())
	}

	var frames []VideoFrame
	ctx := context.Background()
	for {
		f, err := src.NextVideoFrame(ctx)
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			t.Fatalf("NextVideoFrame: %v", err)
		}
		frames = append(frames, f)
	}
	if len(frames) != 5 {
		t.Fatalf("decoded %d frames, want 5", len(frames))
	}
	var prev time.Duration = -1
	for i, f := range frames {
		if f.Width() != 64 || f.Height() != 64 {
			t.Errorf("frame %d is %dx%d, want 64x64", i, f.Width(), f.Height())
		}
		if f.PTS() < prev {
			t.Errorf("frame %d PTS %v went backwards (prev %v)", i, f.PTS(), prev)
		}
		prev = f.PTS()
		gf, ok := f.(*golangVideoFrame)
		if !ok {
			t.Fatalf("frame %d is %T, want *golangVideoFrame", i, f)
		}
		if len(gf.RGBA()) != 64*64*4 {
			t.Errorf("frame %d RGBA len = %d, want %d", i, len(gf.RGBA()), 64*64*4)
		}
		if !hasVariation(gf.RGBA()) {
			t.Errorf("frame %d RGBA is flat", i)
		}
		f.Release()
	}
}

// TestGolangBackendPlaysFragmentedMP4 drives the same pipeline over a
// fragmented MP4 (moof/traf), which mp4x now reads by assembling the sample
// table from the movie fragments.
func TestGolangBackendPlaysFragmentedMP4(t *testing.T) {
	if _, err := os.Stat(golangFragMP4); err != nil {
		t.Skipf("missing testdata: %v", err)
	}
	src, err := (golangBackend{}).OpenSource(golangFragMP4)
	if err != nil {
		t.Fatalf("OpenSource(fragmented): %v", err)
	}
	defer src.Close()
	vt := src.VideoTrack()
	if vt == nil || vt.Width != 64 || vt.Height != 64 {
		t.Fatalf("VideoTrack = %+v, want 64x64", vt)
	}
	n := 0
	ctx := context.Background()
	for {
		f, err := src.NextVideoFrame(ctx)
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			t.Fatalf("NextVideoFrame: %v", err)
		}
		n++
		f.Release()
	}
	if n != 5 {
		t.Errorf("decoded %d frames from fragmented file, want 5", n)
	}
}

// TestGolangBackendSeek checks that a seek lands on a keyframe and that the
// next frame's PTS is at or after the target.
func TestGolangBackendSeek(t *testing.T) {
	if _, err := os.Stat(golangMP4); err != nil {
		t.Skipf("missing testdata: %v", err)
	}
	src, err := golangBackend{}.OpenSource(golangMP4)
	if err != nil {
		t.Fatalf("OpenSource: %v", err)
	}
	defer src.Close()

	target := 400 * time.Millisecond
	if err := src.SeekVideo(target, false); err != nil {
		t.Fatalf("SeekVideo: %v", err)
	}
	// snap-to-keyframe: the first frame after the seek is the keyframe at or
	// before the target (this clip has a single IDR at t=0), and decoding
	// continues forward from there to reach the target.
	ctx := context.Background()
	f, err := src.NextVideoFrame(ctx)
	if err != nil {
		t.Fatalf("NextVideoFrame after seek: %v", err)
	}
	first := f.PTS()
	f.Release()
	if first > target {
		t.Errorf("first frame after seek PTS = %v, want <= %v (snap-to-keyframe)", first, target)
	}
	for i := 0; i < 16; i++ {
		f, err := src.NextVideoFrame(ctx)
		if errors.Is(err, io.EOF) {
			t.Fatalf("reached EOF before a frame at/after %v", target)
		}
		if err != nil {
			t.Fatalf("NextVideoFrame: %v", err)
		}
		pts := f.PTS()
		f.Release()
		if pts >= target {
			return
		}
	}
	t.Errorf("no frame at/after %v after seeking", target)
}

// TestGolangBackendScrubDecoder checks the independent single-frame fetcher.
func TestGolangBackendScrubDecoder(t *testing.T) {
	if _, err := os.Stat(golangMP4); err != nil {
		t.Skipf("missing testdata: %v", err)
	}
	src, err := golangBackend{}.OpenSource(golangMP4)
	if err != nil {
		t.Fatalf("OpenSource: %v", err)
	}
	defer src.Close()

	sd, err := src.OpenScrubDecoder()
	if err != nil {
		t.Fatalf("OpenScrubDecoder: %v", err)
	}
	f, err := sd.DecodeAt(0)
	if err != nil {
		t.Fatalf("DecodeAt: %v", err)
	}
	defer f.Release()
	if f.Width() != 64 || f.Height() != 64 {
		t.Errorf("scrub frame is %dx%d, want 64x64", f.Width(), f.Height())
	}
}

// TestGolangBackendRejectsNonMP4 checks the error surface for junk input.
func TestGolangBackendRejectsNonMP4(t *testing.T) {
	f, err := os.CreateTemp(t.TempDir(), "junk*.mp4")
	if err != nil {
		t.Fatal(err)
	}
	_, _ = f.Write([]byte("this is not an mp4"))
	_ = f.Close()
	if _, err := (golangBackend{}).OpenSource(f.Name()); err == nil {
		t.Fatal("OpenSource(junk) = nil error, want failure")
	}
}

// TestBackendSelection exercises the QUI_MEDIA_BACKEND switch.
func TestBackendSelection(t *testing.T) {
	// A named backend is selected by name.
	restore := swapBackend(t, nil)
	restore()
	resetBackendForTest()
	t.Cleanup(resetBackendForTest)

	setBackend("__test_marker__", markerBackend{})
	t.Setenv("QUI_MEDIA_BACKEND", "__test_marker__")
	resetBackendForTest()
	if _, ok := activeBackend().(markerBackend); !ok {
		t.Fatalf("activeBackend() = %T, want markerBackend", activeBackend())
	}

	// An unknown name selects nothing.
	t.Setenv("QUI_MEDIA_BACKEND", "__nope__")
	resetBackendForTest()
	if got := activeBackend(); got != nil {
		t.Fatalf("activeBackend() = %T for unknown name, want nil", got)
	}

	// auto/golang resolves to the pure-Go backend.
	t.Setenv("QUI_MEDIA_BACKEND", "golang")
	resetBackendForTest()
	if got := activeBackend(); got == nil {
		t.Fatal("activeBackend() = nil for golang, want the pure-Go backend")
	}

	// Default (unset) must resolve to *something* registered.
	t.Setenv("QUI_MEDIA_BACKEND", "")
	resetBackendForTest()
	if got := activeBackend(); got == nil {
		t.Fatal("activeBackend() = nil by default, want a backend")
	}
}

type markerBackend struct{}

func (markerBackend) OpenSource(string) (Source, error) { return nil, ErrNotSupported }
func (markerBackend) Capabilities() Capabilities        { return Capabilities{} }

// hasVariation reports whether buf has at least two distinct bytes.
func hasVariation(buf []byte) bool {
	if len(buf) == 0 {
		return false
	}
	first := buf[0]
	for _, b := range buf[1:] {
		if b != first {
			return true
		}
	}
	return false
}
