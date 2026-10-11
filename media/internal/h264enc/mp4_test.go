package h264enc

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/qizhanchan/qui/media/internal/h264"
	"github.com/qizhanchan/qui/media/internal/mp4x"
)

// writeMP4 encodes frames and muxes them into an MP4 at path.
func writeMP4(t *testing.T, cfg Config, path string, fragmented bool) encResult {
	t.Helper()
	frames := longSource(t, "vt_main_p8x8.264", 40)
	cfg.Width, cfg.Height = frames[0].Rect.Dx(), frames[0].Rect.Dy()
	e, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	e.logRecon = true
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	var w *mp4x.Writer
	if fragmented {
		w = mp4x.CreateFragmented(f, 300*time.Millisecond)
	} else {
		w = mp4x.Create(f)
	}
	sps, pps := e.Headers()
	const timescale, dur = 90000, 3000
	ti, err := w.AddTrack(mp4x.TrackConfig{Kind: mp4x.KindVideo, Timescale: timescale,
		Width: cfg.Width, Height: cfg.Height, SPS: [][]byte{sps}, PPS: [][]byte{pps},
		Delay: uint32(e.reorder * dur)})
	if err != nil {
		t.Fatal(err)
	}
	reorder := int64(e.reorder)
	mux := func(ps []Packet) {
		for _, p := range ps {
			var nals [][]byte
			for _, n := range p.NALs {
				if typ := n[0] & 0x1f; typ != 7 && typ != 8 {
					nals = append(nals, n)
				}
			}
			err := w.WriteSample(ti, mp4x.Sample{
				Data: LengthPrefixed(nals), DTS: (p.DTS + reorder) * dur, Duration: dur,
				CTSOffset: int32((p.PTS - p.DTS) * dur), Sync: p.Key,
			})
			if err != nil {
				t.Fatal(err)
			}
		}
	}
	for _, fr := range frames {
		p, err := e.Encode(fr)
		if err != nil {
			t.Fatal(err)
		}
		mux(p)
	}
	p, _ := e.Flush()
	mux(p)
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return encResult{recon: e.reconLog}
}

// TestMP4RoundTrip muxes encoder output, reads it back with mp4x, decodes
// it in presentation order and compares with the reconstruction; FFmpeg
// must decode the file to the same frames.
func TestMP4RoundTrip(t *testing.T) {
	dir := t.TempDir()
	for _, tc := range []struct {
		name string
		cfg  Config
		frag bool
	}{
		{"baseline", Config{QP: 28, KeyInt: 15}, false},
		{"baseline_frag", Config{QP: 28, KeyInt: 15}, true},
		{"high_b", Config{Profile: ProfileHigh, CABAC: true, Transform8x8: true, BFrames: 2, QP: 27, KeyInt: 12}, false},
		{"high_b_frag", Config{Profile: ProfileHigh, CABAC: true, Transform8x8: true, BFrames: 3, QP: 27, KeyInt: 12}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(dir, tc.name+".mp4")
			r := writeMP4(t, tc.cfg, path, tc.frag)
			rd, err := mp4x.Open(path)
			if err != nil {
				t.Fatal(err)
			}
			defer rd.Close()
			if rd.Fragmented() != tc.frag {
				t.Fatalf("Fragmented() = %v", rd.Fragmented())
			}
			v := rd.VideoTrack()
			if v == nil || v.SampleCount() != len(r.recon) {
				t.Fatalf("video track with %d samples, want %d", v.SampleCount(), len(r.recon))
			}
			if v.Width != tc.cfg.Width && v.Width != 160 {
				t.Fatalf("width %d", v.Width)
			}
			sps, pps, ls, err := rd.AVCCParameterSets()
			if err != nil || ls != 4 {
				t.Fatalf("avcC: %v (length size %d)", err, ls)
			}
			d := h264.New()
			for _, x := range append(sps, pps...) {
				if err := d.DecodeNAL(x); err != nil {
					t.Fatal(err)
				}
			}
			type out struct {
				pts  time.Duration
				data []byte
			}
			var frames []*h264.Frame
			for i := 0; i < v.SampleCount(); i++ {
				data, err := rd.ReadSample(v, i)
				if err != nil {
					t.Fatal(err)
				}
				if rd.PTS(v, i) != time.Duration(0) && i == 0 {
					t.Errorf("first sample PTS %v, want 0 (edit list)", rd.PTS(v, i))
				}
				nals, err := h264.SplitLengthPrefixed(data, ls)
				if err != nil {
					t.Fatal(err)
				}
				d.SetTag(int64(rd.PTS(v, i)))
				if err := d.DecodeNALs(nals); err != nil {
					t.Fatal(err)
				}
				for f := d.Next(); f != nil; f = d.Next() {
					frames = append(frames, f)
				}
			}
			d.Flush()
			for f := d.Next(); f != nil; f = d.Next() {
				frames = append(frames, f)
			}
			if len(frames) != len(r.recon) {
				t.Fatalf("decoded %d frames, want %d", len(frames), len(r.recon))
			}
			for i, f := range frames {
				if want := time.Duration(i) * time.Second / 30; time.Duration(f.Tag) != want {
					t.Fatalf("frame %d PTS %v, want %v", i, time.Duration(f.Tag), want)
				}
				if !bytes.Equal(planes(f), r.recon[i]) {
					t.Fatalf("frame %d differs from the reconstruction", i)
				}
			}
			if keys := rd.Keyframes(v); len(keys) != (len(r.recon)+tc.cfg.KeyInt-1)/tc.cfg.KeyInt {
				t.Errorf("keyframes %v", keys)
			}
			ff, err := exec.LookPath("ffmpeg")
			if err != nil {
				return
			}
			yuv := filepath.Join(dir, tc.name+".yuv")
			if msg, err := exec.Command(ff, "-v", "error", "-y", "-i", path, "-f", "rawvideo", "-pix_fmt", "yuv420p", yuv).CombinedOutput(); err != nil || len(msg) > 0 {
				t.Fatalf("ffmpeg: %v %s", err, msg)
			}
			got, _ := os.ReadFile(yuv)
			if !bytes.Equal(got, bytes.Join(r.recon, nil)) {
				t.Fatalf("FFmpeg decode of the MP4 differs (%d bytes)", len(got))
			}
		})
	}
}
