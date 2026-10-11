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

// TestRemux copies the committed B-frame + AAC clips (H.264 and HEVC)
// sample by sample into progressive and fragmented files and checks that
// every sample, timestamp and codec configuration reads back unchanged (and
// that FFmpeg accepts the files).
func TestRemux(t *testing.T) {
	for _, name := range []string{"bframes_aac_160x90.mp4", "hevc_aac_160x90.mp4"} {
		t.Run(name, func(t *testing.T) { testRemux(t, name) })
	}
}

func testRemux(t *testing.T, name string) {
	src, err := Open(filepath.Join("..", "..", "testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	defer src.Close()
	v, a := src.VideoTrack(), src.AudioTrack()
	if v == nil || a == nil {
		t.Fatal("missing track")
	}
	var vps, sps, pps [][]byte
	if v.Codec == "hevc" {
		vps, sps, pps, _, err = src.HVCCParameterSets()
		if err == nil && (len(vps) == 0 || len(sps) == 0 || len(pps) == 0) {
			t.Fatalf("hvcC parameter sets: %d VPS, %d SPS, %d PPS", len(vps), len(sps), len(pps))
		}
	} else {
		sps, pps, _, err = src.AVCCParameterSets()
	}
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
		vi, _ := w.AddTrack(TrackConfig{Kind: KindVideo, Codec: v.Codec, Timescale: v.Timescale, Width: v.Width, Height: v.Height, VPS: vps, SPS: sps, PPS: pps, Delay: delay})
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
		if v.Codec == "hevc" {
			if !bytes.Equal(HEVCDecoderConfig(vps, sps, pps), hvcCPayload(t, path)) {
				t.Fatalf("frag=%v: hvcC differs from the source parameter sets", frag)
			}
			if got.VideoTrack().hvcC.GeneralProfileIdc != v.hvcC.GeneralProfileIdc || got.VideoTrack().hvcC.GeneralLevelIdc != v.hvcC.GeneralLevelIdc {
				t.Fatalf("frag=%v: hvcC profile/level %d/%d, want %d/%d", frag, got.VideoTrack().hvcC.GeneralProfileIdc,
					got.VideoTrack().hvcC.GeneralLevelIdc, v.hvcC.GeneralProfileIdc, v.hvcC.GeneralLevelIdc)
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

// hvcCPayload returns the payload of the first hvcC box in the file.
func hvcCPayload(t *testing.T, path string) []byte {
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	i := bytes.Index(b, []byte("hvcC"))
	if i < 4 {
		t.Fatal("no hvcC box")
	}
	n := int(b[i-4])<<24 | int(b[i-3])<<16 | int(b[i-2])<<8 | int(b[i-1])
	return b[i+4 : i-4+n]
}

// TestHEVCDecoderConfigFields checks the fields hvcC repeats from the SPS
// against the record libx265 + FFmpeg wrote for the committed clip.
func TestHEVCDecoderConfigFields(t *testing.T) {
	path := filepath.Join("..", "..", "testdata", "hevc_aac_160x90.mp4")
	r, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	v := r.VideoTrack()
	if v == nil || v.Codec != "hevc" || v.Width != 160 || v.Height != 90 {
		t.Fatalf("video track %+v", v)
	}
	vps, sps, pps, ls, err := r.HVCCParameterSets()
	if err != nil || ls != 4 {
		t.Fatalf("HVCCParameterSets: ls %d err %v", ls, err)
	}
	ours := HEVCDecoderConfig(vps, sps, pps)
	theirs := hvcCPayload(t, path)
	// Bytes 0..22 hold the fixed fields; FFmpeg fills avgFrameRate /
	// constantFrameRate / parallelismType from encoder knowledge we do not
	// have, so compare the fields derived from the SPS.
	if !bytes.Equal(ours[1:13], theirs[1:13]) {
		t.Errorf("profile/tier/level %x, want %x", ours[1:13], theirs[1:13])
	}
	if !bytes.Equal(ours[16:19], theirs[16:19]) {
		t.Errorf("chroma/bit depth %x, want %x", ours[16:19], theirs[16:19])
	}
	if ours[21]&0x3b != theirs[21]&0x3b { // numTemporalLayers, lengthSizeMinusOne
		t.Errorf("layers/length byte %#x, want %#x", ours[21], theirs[21])
	}
	if len(r.Keyframes(v)) == 0 || !r.IsSync(v, 0) {
		t.Error("no keyframes")
	}
}
