package av1

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/qizhanchan/qui/media/internal/mp4x"
)

// TestMP4 muxes testdata/ra.ivf into progressive and fragmented MP4
// (av01/av1C) with mp4x, reads the files back, and checks that FFmpeg
// decodes them to the same frames as the IVF; an MP4 muxed by FFmpeg must
// read back to the same samples.
func TestMP4(t *testing.T) {
	data, err := os.ReadFile("testdata/ra.ivf")
	if err != nil {
		t.Fatal(err)
	}
	tus, w, h, err := ReadIVF(data)
	if err != nil {
		t.Fatal(err)
	}
	seq := SplitSequenceHeader(tus[0])
	rec, err := ConfigRecord(seq)
	if err != nil {
		t.Fatal(err)
	}
	var samples [][]byte
	for _, tu := range tus {
		samples = append(samples, StripTemporalDelimiters(tu))
	}
	_, ffErr := exec.LookPath("ffmpeg")
	framemd5 := func(path string) []byte {
		out, err := exec.Command("ffmpeg", "-v", "error", "-i", path, "-f", "framemd5", "-").Output()
		if err != nil {
			t.Fatalf("ffmpeg %s: %v", path, err)
		}
		var md5s []byte
		for _, l := range bytes.Split(out, []byte("\n")) {
			if len(l) > 0 && l[0] != '#' {
				f := bytes.Fields(l)
				md5s = append(md5s, f[len(f)-1]...)
			}
		}
		return md5s
	}
	check := func(t *testing.T, path string, wantKey []int) {
		rd, err := mp4x.Open(path)
		if err != nil {
			t.Fatal(err)
		}
		defer rd.Close()
		v := rd.VideoTrack()
		if v == nil || v.Codec != "av1" || v.SampleCount() != len(samples) || v.Width != w || v.Height != h {
			t.Fatalf("video track %+v", v)
		}
		cfg, err := rd.AV1ConfigOBUs()
		if err != nil || !bytes.Equal(cfg, seq) {
			t.Fatalf("av1C configOBUs %x (%v), want %x", cfg, err, seq)
		}
		for i := range samples {
			b, err := rd.ReadSample(v, i)
			if err != nil || !bytes.Equal(b, samples[i]) {
				t.Fatalf("sample %d differs (%v)", i, err)
			}
		}
		if k := rd.Keyframes(v); wantKey != nil && !slices.Equal(k, wantKey) {
			t.Errorf("keyframes %v, want %v", k, wantKey)
		}
	}
	dir := t.TempDir()
	for _, frag := range []bool{false, true} {
		name := "prog"
		if frag {
			name = "frag"
		}
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(dir, name+".mp4")
			f, err := os.Create(path)
			if err != nil {
				t.Fatal(err)
			}
			var mw *mp4x.Writer
			if frag {
				mw = mp4x.CreateFragmented(f, 200*time.Millisecond)
			} else {
				mw = mp4x.Create(f)
			}
			const dur = 3600
			ti, err := mw.AddTrack(mp4x.TrackConfig{Kind: mp4x.KindVideo, Codec: "av1", Timescale: 90000, Width: w, Height: h, AV1C: rec})
			if err != nil {
				t.Fatal(err)
			}
			for i, s := range samples {
				if err := mw.WriteSample(ti, mp4x.Sample{Data: s, DTS: int64(i * dur), Duration: dur, Sync: i == 0}); err != nil {
					t.Fatal(err)
				}
			}
			if err := mw.Close(); err != nil {
				t.Fatal(err)
			}
			f.Close()
			check(t, path, []int{0})
			if ffErr == nil && !bytes.Equal(framemd5(path), framemd5("testdata/ra.ivf")) {
				t.Error("FFmpeg decodes the MP4 differently from the IVF")
			}
		})
	}
	if ffErr != nil {
		return
	}
	t.Run("ffmpeg", func(t *testing.T) {
		path := filepath.Join(dir, "ff.mp4")
		if out, err := exec.Command("ffmpeg", "-v", "error", "-i", "testdata/ra.ivf", "-c", "copy", path).CombinedOutput(); err != nil {
			t.Fatalf("ffmpeg: %v %s", err, out)
		}
		check(t, path, nil)
	})
}
