package av1

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/qizhanchan/qui/media/internal/mp4x"
)

// TestEncMP4RoundTrip muxes encoder output into progressive and
// fragmented MP4 (av01/av1C), reads it back with mp4x, decodes it and
// compares with the reconstruction; FFmpeg must decode the file to the
// same frames.
func TestEncMP4RoundTrip(t *testing.T) {
	dir := t.TempDir()
	frames := rcSource(t, 24)
	w, h := frames[0].Rect.Dx(), frames[0].Rect.Dy()
	for _, tc := range []struct {
		name string
		cfg  EncoderConfig
		frag bool
	}{
		{"p", EncoderConfig{QIndex: 120, KeyInt: 12}, false},
		{"p_frag", EncoderConfig{CRF: 30, KeyInt: 10, Refs: 2}, true},
		{"arf", EncoderConfig{QIndex: 120, KeyInt: 12, MiniGOP: 4}, false},
		{"arf_frag", EncoderConfig{CRF: 32, KeyInt: 24, MiniGOP: 5, Refs: 2}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg := tc.cfg
			cfg.Width, cfg.Height = w, h
			e, err := NewEncoder(cfg)
			if err != nil {
				t.Fatal(err)
			}
			e.keepRecon = true
			path := filepath.Join(dir, tc.name+".mp4")
			f, err := os.Create(path)
			if err != nil {
				t.Fatal(err)
			}
			var mw *mp4x.Writer
			if tc.frag {
				mw = mp4x.CreateFragmented(f, 300*time.Millisecond)
			} else {
				mw = mp4x.Create(f)
			}
			rec, err := ConfigRecord(e.SequenceHeader())
			if err != nil {
				t.Fatal(err)
			}
			const timescale, dur = 90000, 3000
			ti, err := mw.AddTrack(mp4x.TrackConfig{Kind: mp4x.KindVideo, Codec: "av1", Timescale: timescale,
				Width: w, Height: h, AV1C: rec})
			if err != nil {
				t.Fatal(err)
			}
			for _, p := range encodeAll(t, e, frames) {
				if err := mw.WriteSample(ti, mp4x.Sample{Data: p.Data, DTS: p.PTS * dur, Duration: dur, Sync: p.Key}); err != nil {
					t.Fatal(err)
				}
			}
			if err := mw.Close(); err != nil {
				t.Fatal(err)
			}
			f.Close()
			var recon []byte
			for _, fr := range e.recon {
				recon = append(recon, framePlanes(fr)...)
			}

			rd, err := mp4x.Open(path)
			if err != nil {
				t.Fatal(err)
			}
			defer rd.Close()
			v := rd.VideoTrack()
			if v == nil || v.Codec != "av1" || v.SampleCount() != len(frames) {
				t.Fatalf("video track %+v", v)
			}
			cfgOBUs, err := rd.AV1ConfigOBUs()
			if err != nil {
				t.Fatal(err)
			}
			d := New()
			if err := d.Decode(cfgOBUs); err != nil {
				t.Fatal(err)
			}
			var got []byte
			n := 0
			for i := 0; i < v.SampleCount(); i++ {
				data, err := rd.ReadSample(v, i)
				if err != nil {
					t.Fatal(err)
				}
				d.SetTag(int64(rd.PTS(v, i)))
				if err := d.Decode(data); err != nil {
					t.Fatal(err)
				}
				for fr := d.Next(); fr != nil; fr = d.Next() {
					if want := time.Duration(n) * time.Second / 30; time.Duration(fr.Tag) != want {
						t.Fatalf("frame %d PTS %v, want %v", n, time.Duration(fr.Tag), want)
					}
					got = append(got, framePlanes(fr)...)
					n++
				}
			}
			if n != len(frames) || !bytes.Equal(got, recon) {
				t.Fatalf("decoded MP4 (%d frames) differs from the reconstruction", n)
			}
			if keys := rd.Keyframes(v); len(keys) != (len(frames)+cfg.KeyInt-1)/cfg.KeyInt {
				t.Errorf("keyframes %v", keys)
			}
			ff, err := exec.LookPath("ffmpeg")
			if err != nil {
				return
			}
			yuv, err := exec.Command(ff, "-v", "error", "-c:v", "libdav1d", "-i", path, "-f", "rawvideo", "-pix_fmt", "yuv420p", "-").Output()
			if err != nil {
				t.Fatalf("ffmpeg: %v", err)
			}
			if !bytes.Equal(yuv, recon) {
				t.Fatalf("FFmpeg decodes the MP4 differently (%d vs %d bytes)", len(yuv), len(recon))
			}
		})
	}
}
