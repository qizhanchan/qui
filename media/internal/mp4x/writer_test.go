package mp4x

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"testing"
	"time"
)

// TestRemux copies the committed B-frame + AAC clip sample by sample into
// progressive and fragmented files and checks that every sample, timestamp
// and codec configuration reads back unchanged (and that FFmpeg accepts
// the files).
func TestRemux(t *testing.T) {
	src, err := Open(filepath.Join("..", "..", "testdata", "bframes_aac_160x90.mp4"))
	if err != nil {
		t.Fatal(err)
	}
	defer src.Close()
	v, a := src.VideoTrack(), src.AudioTrack()
	sps, pps, _, err := src.AVCCParameterSets()
	if err != nil {
		t.Fatal(err)
	}
	asc, err := src.AudioSpecificConfig()
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	for _, frag := range []bool{false, true} {
		path := filepath.Join(dir, "remux.mp4")
		f, err := os.Create(path)
		if err != nil {
			t.Fatal(err)
		}
		var w *Writer
		if frag {
			w = CreateFragmented(f, 500*time.Millisecond)
		} else {
			w = Create(f)
		}
		delay := uint32(v.shift)
		vi, _ := w.AddTrack(TrackConfig{Kind: KindVideo, Timescale: v.Timescale, Width: v.Width, Height: v.Height, SPS: sps, PPS: pps, Delay: delay})
		ai, _ := w.AddTrack(TrackConfig{Kind: KindAudio, Timescale: a.Timescale, SampleRate: a.SampleRate, Channels: a.Channels, ASC: asc, Delay: uint32(a.shift)})
		// Interleave by decode time.
		type item struct {
			track *Track
			idx   int
			t     time.Duration
		}
		var items []item
		for i := 0; i < v.SampleCount(); i++ {
			items = append(items, item{v, i, src.DTS(v, i)})
		}
		for i := 0; i < a.SampleCount(); i++ {
			items = append(items, item{a, i, src.DTS(a, i)})
		}
		sort.SliceStable(items, func(i, j int) bool { return items[i].t < items[j].t })
		for _, it := range items {
			data, err := src.ReadSample(it.track, it.idx)
			if err != nil {
				t.Fatal(err)
			}
			s := it.track.samples[it.idx]
			ti := vi
			if it.track == a {
				ti = ai
			}
			if err := w.WriteSample(ti, Sample{Data: data, DTS: int64(s.dts), Duration: s.dur, CTSOffset: int32(s.cto), Sync: s.sync}); err != nil {
				t.Fatal(err)
			}
		}
		if err := w.Close(); err != nil {
			t.Fatal(err)
		}
		f.Close()

		got, err := Open(path)
		if err != nil {
			t.Fatal(err)
		}
		if got.Fragmented() != frag {
			t.Fatalf("Fragmented() = %v", got.Fragmented())
		}
		for _, pair := range [][2]*Track{{v, got.VideoTrack()}, {a, got.AudioTrack()}} {
			o, n := pair[0], pair[1]
			if n == nil || n.SampleCount() != o.SampleCount() || n.Codec != o.Codec {
				t.Fatalf("frag=%v: track %v mismatch", frag, o.Codec)
			}
			for i := 0; i < o.SampleCount(); i++ {
				d0, _ := src.ReadSample(o, i)
				d1, _ := got.ReadSample(n, i)
				if !bytes.Equal(d0, d1) || src.PTS(o, i) != got.PTS(n, i) || src.DTS(o, i) != got.DTS(n, i) || src.IsSync(o, i) != got.IsSync(n, i) {
					t.Fatalf("frag=%v %s sample %d differs (pts %v/%v dts %v/%v)", frag, o.Codec, i,
						src.PTS(o, i), got.PTS(n, i), src.DTS(o, i), got.DTS(n, i))
				}
			}
		}
		if a2, _ := got.AudioSpecificConfig(); !bytes.Equal(a2, asc) {
			t.Fatalf("ASC %x, want %x", a2, asc)
		}
		got.Close()
		if ff, err := exec.LookPath("ffmpeg"); err == nil {
			if msg, err := exec.Command(ff, "-v", "error", "-i", path, "-f", "null", "-").CombinedOutput(); err != nil || len(msg) > 0 {
				t.Fatalf("ffmpeg: %v %s", err, msg)
			}
		}
	}
}
