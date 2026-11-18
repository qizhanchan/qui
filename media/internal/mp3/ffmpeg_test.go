package mp3

import (
	"bytes"
	"encoding/binary"
	"io"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// Fixtures in media/testdata were generated with FFmpeg 9 + libmp3lame
// from an lavfi chirp/tone/noise-burst source (the bursts force short
// blocks):
//
//	SRC='aevalsrc=0.3*sin(2*PI*(200+1500*t)*t)+if(lt(mod(t\,0.25)\,0.02)\,0.6*(random(0)-0.5)\,0)|0.25*sin(2*PI*330*t)+0.2*sin(2*PI*(3000-1200*t)*t)+if(lt(mod(t+0.1\,0.3)\,0.015)\,0.5*(random(1)-0.5)\,0):s=44100:d=2'
//	ffmpeg -f lavfi -i "$SRC" -c:a libmp3lame -b:a 128k -joint_stereo 0 mp3_stereo_cbr128.mp3
//	ffmpeg -f lavfi -i "$SRC" -c:a libmp3lame -b:a 96k -joint_stereo 1 mp3_jstereo_96k.mp3
//	ffmpeg -f lavfi -i "$SRC" -ac 1 -ar 22050 -c:a libmp3lame -q:a 4 mp3_mono22k_vbr.mp3
//	ffmpeg -f lavfi -i "$SRC" -ar 8000 -c:a libmp3lame -b:a 16k mp3_stereo8k_mpeg25.mp3
//	ffmpeg -f lavfi -i "$SRC" -t 1.5 -c:a libmp3lame -b:a 112k -id3v2_version 3 -write_id3v1 1 \
//	       -metadata title="Fixture title" -metadata artist="qui" mp3_id3_tags.mp3
//	ffmpeg -f lavfi -i "$SRC" -t 1.5 -c:a mp2 -b:a 192k mp2_stereo44k.mp2
//	ffmpeg -f lavfi -i "$SRC" -t 1.5 -ar 24000 -ac 1 -c:a mp2 -b:a 64k mp2_mono24k_lsf.mp2
//	ffmpeg -f lavfi -i "$SRC" -t 1 -c:a libmp3lame -b:a 128k -write_xing 0 mp3_noxing.mp3
//
// The
// reference PCM is produced at test time by FFmpeg's float decoders
// (mp3float / mp2float), so these tests skip when ffmpeg is not installed.

func fixture(name string) string { return filepath.Join("..", "..", "testdata", name) }

func ffmpegDecode(t *testing.T, path, codec string) []float32 {
	t.Helper()
	bin, err := exec.LookPath("ffmpeg")
	if err != nil {
		t.Skip("ffmpeg not installed")
	}
	cmd := exec.Command(bin, "-hide_banner", "-loglevel", "error", "-c:a", codec, "-i", path, "-f", "f32le", "-")
	var out bytes.Buffer
	cmd.Stdout = &out
	if err := cmd.Run(); err != nil {
		t.Fatalf("ffmpeg %s: %v", path, err)
	}
	ref := make([]float32, out.Len()/4)
	if err := binary.Read(&out, binary.LittleEndian, ref); err != nil {
		t.Fatal(err)
	}
	return ref
}

func openFixture(t *testing.T, name string) (*Stream, []byte) {
	t.Helper()
	data, err := os.ReadFile(fixture(name))
	if err != nil {
		t.Fatal(err)
	}
	s, err := Open(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("Open %s: %v", name, err)
	}
	return s, data
}

func decodeStream(t *testing.T, s *Stream) []float32 {
	t.Helper()
	r := NewPCMReader(s)
	var pcm []float32
	for {
		var err error
		pcm, err = r.Read(pcm)
		if err == io.EOF {
			return pcm
		}
		if err != nil {
			t.Fatal(err)
		}
	}
}

// compare returns max abs error and SNR (dB) of got against ref.
func compare(got, ref []float32) (maxErr, snr float64) {
	var sig, noise float64
	for i := range ref {
		d := float64(got[i]) - float64(ref[i])
		sig += float64(ref[i]) * float64(ref[i])
		noise += d * d
		if math.Abs(d) > maxErr {
			maxErr = math.Abs(d)
		}
	}
	if noise == 0 {
		return 0, math.Inf(1)
	}
	return maxErr, 10 * math.Log10(sig/noise)
}

var fixtures = []struct {
	name, codec string
	sr, ch      int
	gapless     bool
}{
	{"mp3_stereo_cbr128.mp3", "mp3float", 44100, 2, true},  // MPEG-1 L3 stereo CBR 128k
	{"mp3_jstereo_96k.mp3", "mp3float", 44100, 2, true},    // MPEG-1 L3 joint (M/S) stereo
	{"mp3_mono22k_vbr.mp3", "mp3float", 22050, 1, true},    // MPEG-2 L3 mono VBR (-q:a 4)
	{"mp3_stereo8k_mpeg25.mp3", "mp3float", 8000, 2, true}, // MPEG-2.5 L3 8 kHz
	{"mp3_id3_tags.mp3", "mp3float", 44100, 2, true},       // ID3v2.3 + ID3v1
	{"mp3_noxing.mp3", "mp3float", 44100, 2, false},        // no Xing/LAME tag
	{"mp2_stereo44k.mp2", "mp2float", 44100, 2, false},     // MPEG-1 Layer II
	{"mp2_mono24k_lsf.mp2", "mp2float", 24000, 1, false},   // MPEG-2 Layer II LSF
}

func TestMatchesFFmpeg(t *testing.T) {
	for _, f := range fixtures {
		t.Run(f.name, func(t *testing.T) {
			s, _ := openFixture(t, f.name)
			if s.SampleRate() != f.sr || s.Channels() != f.ch {
				t.Fatalf("format %d Hz %d ch, want %d Hz %d ch", s.SampleRate(), s.Channels(), f.sr, f.ch)
			}
			if _, _, ok := s.Gapless(); ok != f.gapless {
				t.Fatalf("gapless tag = %v, want %v", ok, f.gapless)
			}
			got := decodeStream(t, s)
			ref := ffmpegDecode(t, fixture(f.name), f.codec)
			if len(got) != len(ref) {
				t.Fatalf("length %d samples, ffmpeg %d", len(got)/f.ch, len(ref)/f.ch)
			}
			maxErr, snr := compare(got, ref)
			t.Logf("%s: %d samples/ch, max abs err %.3g, SNR %.1f dB", f.name, len(got)/f.ch, maxErr, snr)
			if snr < 90 {
				t.Errorf("SNR %.1f dB < 90 dB", snr)
			}
		})
	}
}
