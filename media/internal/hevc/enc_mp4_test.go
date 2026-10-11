package hevc

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/qizhanchan/qui/media/internal/mp4x"
)

// TestMP4RoundTrip muxes encoder output into progressive and fragmented
// MP4 (hvc1/hvcC), reads it back with mp4x, decodes it and compares with
// the reconstruction in presentation order; FFmpeg must decode the file
// to the same frames.
func TestMP4RoundTrip(t *testing.T) {
	dir := t.TempDir()
	frames := rcSource(t, 30)
	w, h := frames[0].Rect.Dx(), frames[0].Rect.Dy()
	for _, tc := range []struct {
		name string
		cfg  EncoderConfig
		frag bool
	}{
		{"p", EncoderConfig{QP: 30, KeyInt: 15}, false},
		{"p_frag", EncoderConfig{QP: 30, KeyInt: 15}, true},
		{"b", EncoderConfig{QP: 28, BFrames: 3, KeyInt: 12}, false},
		{"b_frag", EncoderConfig{QP: 28, BFrames: 2, KeyInt: 12, WPP: true}, true},
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
			vps, sps, pps := e.Headers()
			const timescale, dur = 90000, 3000
			reorder := int64(e.Reorder())
			ti, err := mw.AddTrack(mp4x.TrackConfig{Kind: mp4x.KindVideo, Codec: "hevc", Timescale: timescale,
				Width: w, Height: h, VPS: [][]byte{vps}, SPS: [][]byte{sps}, PPS: [][]byte{pps},
				Delay: uint32(reorder * dur)})
			if err != nil {
				t.Fatal(err)
			}
			mux := func(ps []Packet) {
				for _, p := range ps {
					var nals [][]byte
					for _, n := range p.NALs {
						if typ := int(n[0]>>1) & 63; typ < nalVPS || typ > nalPPS {
							nals = append(nals, n)
						}
					}
					err := mw.WriteSample(ti, mp4x.Sample{Data: LengthPrefixed(nals), DTS: (p.DTS + reorder) * dur,
						Duration: dur, CTSOffset: int32((p.PTS - p.DTS) * dur), Sync: p.Key})
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
			if err := mw.Close(); err != nil {
				t.Fatal(err)
			}
			f.Close()
			e.d.Flush()
			for fr := e.d.Next(); fr != nil; fr = e.d.Next() {
				e.recon = append(e.recon, fr)
			}
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
			if v == nil || v.Codec != "hevc" || v.SampleCount() != len(frames) {
				t.Fatalf("video track %+v", v)
			}
			hv, hs, hp, ls, err := rd.HVCCParameterSets()
			if err != nil || ls != 4 {
				t.Fatalf("hvcC: %v (length size %d)", err, ls)
			}
			d := New()
			for _, x := range append(append(hv, hs...), hp...) {
				if err := d.DecodeNAL(x); err != nil {
					t.Fatal(err)
				}
			}
			var out []*Frame
			for i := 0; i < v.SampleCount(); i++ {
				data, err := rd.ReadSample(v, i)
				if err != nil {
					t.Fatal(err)
				}
				nals, err := SplitLengthPrefixed(data, ls)
				if err != nil {
					t.Fatal(err)
				}
				d.SetTag(int64(rd.PTS(v, i)))
				if err := d.DecodeNALs(nals); err != nil {
					t.Fatal(err)
				}
				for fr := d.Next(); fr != nil; fr = d.Next() {
					out = append(out, fr)
				}
			}
			d.Flush()
			for fr := d.Next(); fr != nil; fr = d.Next() {
				out = append(out, fr)
			}
			if len(out) != len(frames) {
				t.Fatalf("decoded %d frames, want %d", len(out), len(frames))
			}
			var got []byte
			for i, fr := range out {
				if want := time.Duration(i) * time.Second / 30; time.Duration(fr.Tag) != want {
					t.Fatalf("frame %d PTS %v, want %v", i, time.Duration(fr.Tag), want)
				}
				got = append(got, framePlanes(fr)...)
			}
			if !bytes.Equal(got, recon) {
				t.Fatal("decoded MP4 differs from the reconstruction")
			}
			if keys := rd.Keyframes(v); len(keys) != (len(frames)+cfg.KeyInt-1)/cfg.KeyInt {
				t.Errorf("keyframes %v", keys)
			}
			ff, err := exec.LookPath("ffmpeg")
			if err != nil {
				return
			}
			yuv, err := exec.Command(ff, "-v", "error", "-i", path, "-f", "rawvideo", "-pix_fmt", "yuv420p", "-").Output()
			if err != nil {
				t.Fatalf("ffmpeg: %v", err)
			}
			if !bytes.Equal(yuv, recon) {
				t.Fatalf("FFmpeg decode of the MP4 differs (%d bytes, want %d)", len(yuv), len(recon))
			}
		})
	}
}
