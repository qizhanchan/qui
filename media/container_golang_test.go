//go:build cgo

package media

import (
	"bytes"
	"encoding/binary"
	"fmt"

	"math"
	"os"
	"os/exec"
	"testing"
	"time"

	"github.com/qizhanchan/qui/media/internal/audioout"
)

// containerClips are the Matroska/WebM fixtures; the hashes are FFmpeg's
// decode of all 24 frames as I420 (ffmpeg -fps_mode passthrough -f
// rawvideo -pix_fmt yuv420p). The H.264 and HEVC clips carry the same
// streams as their MP4 siblings, so the hashes match theirs.
var containerClips = []struct {
	file, codec, hash string
}{
	{"testdata/av1_opus_160x90.webm", "av1", "95a954a15f562c7b2f66894c28aea85bf99b55796fcbd26e08798ad80147b0c0"},
	{"testdata/h264_aac_160x90.mkv", "h264", "12751dbb2428437074dae7f56db9f14707cd013c6353d3afbc4d6d4942556fc3"},
	{"testdata/hevc_160x90.mkv", "hevc", "e2cbb5109bb5d1e0deb1a3e4f3a5572eb642d16caedb5737e516c48fcd1d8f3b"},
	{"testdata/vp9p0_opus_160x90.webm", "vp9", "d7cbdf4d196ad71381cc474aa58590c073e028c13f14b1cb865391720cb84a73"},
}

func TestSniffContainer(t *testing.T) {
	for file, want := range map[string]string{
		"testdata/av1_opus_160x90.webm":   "webm",
		"testdata/h264_aac_160x90.mkv":    "webm",
		"testdata/vorbis_audio.mka":       "webm",
		"testdata/tone.opus":              "ogg",
		"testdata/tone_vorbis.ogg":        "ogg",
		"testdata/bframes_aac_160x90.mp4": "mp4",
		"testdata/mp3_stereo_cbr128.mp3":  "mp3",
		"testdata/mp3_id3_tags.mp3":       "mp3",
		"testdata/mp3_noxing.mp3":         "mp3",
	} {
		b, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		if got := sniffContainer(bytes.NewReader(b), int64(len(b))); got != want {
			t.Errorf("%s: sniffed %q, want %q", file, got, want)
		}
	}
	junk := []byte("definitely not media, just some text that goes on")
	if got := sniffContainer(bytes.NewReader(junk), int64(len(junk))); got != "" {
		t.Errorf("junk sniffed as %q", got)
	}
}

func TestContainerVideoMatchesFFmpeg(t *testing.T) {
	for _, c := range containerClips {
		t.Run(c.codec, func(t *testing.T) {
			src, err := golangBackend{}.OpenSource(c.file)
			if err != nil {
				t.Fatalf("OpenSource: %v", err)
			}
			if d := src.VideoTrack().Duration; d < 900*time.Millisecond || d > 1100*time.Millisecond {
				t.Errorf("duration %v", d)
			}
			checkMatchesFFmpeg(t, src, c.codec, c.hash)
		})
		t.Run(c.codec+"/stream", func(t *testing.T) {
			b, err := os.ReadFile(c.file)
			if err != nil {
				t.Fatal(err)
			}
			ms := &memSource{Reader: bytes.NewReader(b)}
			src, err := golangBackend{}.OpenStream(ms)
			if err != nil {
				t.Fatalf("OpenStream: %v", err)
			}
			checkMatchesFFmpeg(t, src, c.codec, c.hash)
			if !ms.closed.Load() {
				t.Error("Source.Close did not close the ByteSource")
			}
		})
	}
}

func TestContainerVideoPreciseSeek(t *testing.T) {
	for _, c := range containerClips {
		t.Run(c.codec, func(t *testing.T) { testPreciseSeek(t, c.file) })
	}
}

// TestContainerRejects: VP9 profile 1 (4:4:4) is not decoded but its Opus
// audio plays;
// Vorbis-only files and junk fail.
func TestContainerRejects(t *testing.T) {
	src, err := golangBackend{}.OpenSource("testdata/vp9_opus_160x90.webm")
	if golangAudioAvailable() {
		if err != nil {
			t.Fatalf("vp9+opus: %v", err)
		}
		if src.VideoTrack() != nil || src.AudioTrack() == nil || src.AudioTrack().Codec != "opus" {
			t.Errorf("vp9+opus: video %v audio %v", src.VideoTrack(), src.AudioTrack())
		}
		src.Close()
	}
	for _, f := range []string{"testdata/tone_vorbis.ogg", "testdata/vorbis_audio.mka"} {
		if s, err := (golangBackend{}).OpenSource(f); err == nil {
			s.Close()
			t.Errorf("%s opened", f)
		}
	}
}

// ffmpegF32 decodes file's first audio stream with FFmpeg as interleaved
// float32 (skips the test without ffmpeg).
func ffmpegF32(t *testing.T, file, codec string, stream int) []float32 {
	t.Helper()
	bin, err := exec.LookPath("ffmpeg")
	if err != nil {
		t.Skip("ffmpeg not installed")
	}
	spec := fmt.Sprintf("a:%d", stream)
	args := []string{"-v", "error"}
	if codec == "opus" {
		// The reference is libopus (FFmpeg's native decoder is not).
		args = append(args, "-c:"+spec, "libopus")
	}
	args = append(args, "-i", file, "-map", "0:"+spec, "-f", "f32le", "-")
	out, err := exec.Command(bin, args...).Output()
	if err != nil {
		t.Fatalf("ffmpeg: %v", err)
	}
	pcm := make([]float32, len(out)/4)
	for i := range pcm {
		pcm[i] = math.Float32frombits(binary.LittleEndian.Uint32(out[4*i:]))
	}
	return pcm
}

// snrDB compares got (s16) with want over n samples.
func snrDB(got []int16, want []float32, n int) float64 {
	var sig, noise float64
	for i := 0; i < n; i++ {
		w := float64(want[i])
		d := float64(got[i])/32768 - w
		sig += w * w
		noise += d * d
	}
	if noise == 0 {
		return math.Inf(1)
	}
	return 10 * math.Log10(sig/noise)
}

// openCapture opens file with an injected capturing sink and returns the
// renderer.
func openCapture(t *testing.T, file string) (*golangAudio, *captureSink) {
	t.Helper()
	f, err := os.Open(file)
	if err != nil {
		t.Fatal(err)
	}
	st, _ := f.Stat()
	cs := &captureSink{}
	open := func(audioout.Format) (audioout.Sink, error) { return uncappedSink{cs}, nil }
	var src Source
	switch kind := sniffContainer(f, st.Size()); kind {
	case "webm":
		src, err = openWebM(f, st.Size(), f, open)
	case "ogg":
		src, err = openOgg(f, st.Size(), f, open)
	case "mp3":
		src, err = openMP3(f, st.Size(), f, open)
	default:
		t.Fatalf("%s: container %q", file, kind)
	}
	if err != nil {
		f.Close()
		t.Fatalf("%s: %v", file, err)
	}
	t.Cleanup(func() { src.Close() })
	return src.(*golangSource).audio, cs
}

// audioClips: renderer output vs FFmpeg's decode, from the start and after
// a seek (minimum SNR in dB).
var audioClips = []struct {
	file, codec string
	minSNR      float64
	stream      int // FFmpeg's audio stream index
}{
	{"testdata/tone.opus", "opus", 40, 0},
	{"testdata/long_6k.opus", "opus", 30, 0},
	{"testdata/av1_opus_160x90.webm", "opus", 40, 0},
	{"testdata/vp9_opus_160x90.webm", "opus", 40, 0},
	{"testdata/flac_opus_multi.ogg", "opus", 40, 1},
	{"testdata/h264_aac_160x90.mkv", "aac", 40, 0},
	{"testdata/mp3_stereo_cbr128.mp3", "mp3", 40, 0},
	{"testdata/mp3_mono22k_vbr.mp3", "mp3", 40, 0},
	{"testdata/mp3_id3_tags.mp3", "mp3", 40, 0},
}

func TestContainerAudioMatchesFFmpeg(t *testing.T) {
	if !golangAudioAvailable() {
		t.Skip("no audio output")
	}
	for _, c := range audioClips {
		t.Run(c.file, func(t *testing.T) {
			ref := ffmpegF32(t, c.file, c.codec, c.stream)
			a, cs := openCapture(t, c.file)
			if a.info.Codec != c.codec {
				t.Fatalf("codec %q", a.info.Codec)
			}
			chs := a.chs
			want := len(ref) / chs
			if d := time.Duration(want) * time.Second / time.Duration(a.rate); absDur(d-a.info.Duration) > 30*time.Millisecond {
				t.Errorf("duration %v, ffmpeg decodes %v", a.info.Duration, d)
			}
			got := waitEnd(a, cs)
			if g := len(got) / chs; g < want-2 || g > want+2 {
				t.Errorf("%d frames, ffmpeg %d", g, want)
			}
			n := min(len(got), len(ref))
			if snr := snrDB(got, ref, n); snr < c.minSNR {
				t.Errorf("start: SNR %.1f dB < %.0f", snr, c.minSNR)
			}
			// Seek to a third of the way in.
			at := want / 3
			_ = a.SeekTo(time.Duration(at) * time.Second / time.Duration(a.rate))
			got = waitEnd(a, cs)
			tail := ref[at*chs:]
			n = min(len(got), len(tail))
			if n < len(tail)-4*chs {
				t.Errorf("seek: %d samples, want %d", len(got), len(tail))
			}
			// Opus converges over its pre-roll only approximately: the
			// first 50 ms after a seek may differ from a full decode.
			skip := 0
			if c.codec == "opus" {
				skip = a.rate / 20 * chs
			}
			if snr := snrDB(got[skip:], tail[skip:], n-skip); snr < c.minSNR {
				t.Errorf("seek: SNR %.1f dB < %.0f", snr, c.minSNR)
			}
		})
	}
}

// waitEnd waits until the renderer has decoded everything and returns
// what the sink received.
func waitEnd(a *golangAudio, cs *captureSink) []int16 {
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		a.mu.Lock()
		done := a.eof && len(a.pending) == 0
		a.mu.Unlock()
		if done {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	return cs.samples()
}

// uncappedSink is a captureSink without its 1 MB limit.
type uncappedSink struct{ *captureSink }

func (u uncappedSink) Write(p []byte) (int, error) {
	u.mu.Lock()
	defer u.mu.Unlock()
	u.pcm = append(u.pcm, p...)
	return len(p), nil
}
