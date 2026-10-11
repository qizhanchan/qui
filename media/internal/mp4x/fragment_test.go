package mp4x

import (
	"bytes"
	"os"
	"testing"
	"time"
)

// TestFragmentedMatchesProgressive reads a fragmented MP4 (moov + moof/traf/trun
// + mdat) and its progressive twin (same source, same mdat payload) and asserts
// the sample table assembled from the movie fragments is equivalent to the one
// built from stbl: same sample count, byte-identical samples, the same decode
// timeline, the same keyframes and the same keyframe seek.
func TestFragmentedMatchesProgressive(t *testing.T) {
	if _, err := os.Stat("testdata/frag_p_64x64.mp4"); err != nil {
		t.Skipf("missing testdata: %v", err)
	}
	prog, err := Open("testdata/prog_p_64x64.mp4")
	if err != nil {
		t.Fatalf("open progressive: %v", err)
	}
	defer prog.Close()
	frag, err := Open("testdata/frag_p_64x64.mp4")
	if err != nil {
		t.Fatalf("open fragmented: %v", err)
	}
	defer frag.Close()

	if prog.Fragmented() {
		t.Error("progressive file reported as fragmented")
	}
	if !frag.Fragmented() {
		t.Error("fragmented file not reported as fragmented")
	}

	pt, ft := prog.VideoTrack(), frag.VideoTrack()
	if pt == nil || ft == nil {
		t.Fatal("missing video track")
	}
	if ft.Codec != "h264" || ft.Width != 64 || ft.Height != 64 {
		t.Fatalf("fragmented VideoTrack = %+v, want h264 64x64", ft)
	}

	pn, fn := pt.SampleCount(), ft.SampleCount()
	if pn == 0 {
		t.Fatal("progressive sample count is 0")
	}
	if pn != fn {
		t.Fatalf("sample count: progressive %d, fragmented %d", pn, fn)
	}

	for i := 0; i < fn; i++ {
		ps, err := prog.ReadSample(pt, i)
		if err != nil {
			t.Fatalf("progressive sample %d: %v", i, err)
		}
		fs, err := frag.ReadSample(ft, i)
		if err != nil {
			t.Fatalf("fragmented sample %d: %v", i, err)
		}
		if !bytes.Equal(ps, fs) {
			t.Errorf("sample %d differs: progressive %d bytes, fragmented %d bytes", i, len(ps), len(fs))
		}
		if pd, fd := prog.DTS(pt, i), frag.DTS(ft, i); pd != fd {
			t.Errorf("sample %d DTS: progressive %v, fragmented %v", i, pd, fd)
		}
	}

	pk, fk := prog.Keyframes(pt), frag.Keyframes(ft)
	if len(pk) != len(fk) {
		t.Fatalf("keyframes: progressive %v, fragmented %v", pk, fk)
	}
	for i := range pk {
		if pk[i] != fk[i] {
			t.Fatalf("keyframes: progressive %v, fragmented %v", pk, fk)
		}
	}

	target := 400 * time.Millisecond
	if a, b := prog.SeekKeyframe(pt, target), frag.SeekKeyframe(ft, target); a != b {
		t.Errorf("SeekKeyframe(%v): progressive %d, fragmented %d", target, a, b)
	}
}

// TestFragmentedSampleCount checks the fragmented file exposes its samples
// even though it has no stbl sample table.
func TestFragmentedSampleCount(t *testing.T) {
	if _, err := os.Stat("testdata/frag_p_64x64.mp4"); err != nil {
		t.Skipf("missing testdata: %v", err)
	}
	frag, err := Open("testdata/frag_p_64x64.mp4")
	if err != nil {
		t.Fatalf("open fragmented: %v", err)
	}
	defer frag.Close()
	ft := frag.VideoTrack()
	if ft == nil {
		t.Fatal("missing video track")
	}
	if got := ft.SampleCount(); got != 5 {
		t.Errorf("SampleCount = %d, want 5", got)
	}
	if ft.Duration <= 0 {
		t.Errorf("Duration = %v, want > 0", ft.Duration)
	}
	// Every sample must be readable and non-empty.
	for i := 0; i < ft.SampleCount(); i++ {
		s, err := frag.ReadSample(ft, i)
		if err != nil {
			t.Fatalf("sample %d: %v", i, err)
		}
		if len(s) == 0 {
			t.Errorf("sample %d is empty", i)
		}
	}
}
