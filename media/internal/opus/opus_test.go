package opus

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// fixtureDir holds short Ogg Opus clips made with FFmpeg's libopus encoder
// from synthetic speech-like / music-like sources, e.g.
//
//	ffmpeg -i speech.wav -application voip -b:a 8k -cutoff 4000 -frame_duration 20 -c:a libopus silk_nb_mono_20ms.opus
//	ffmpeg -i mix.wav -application audio -b:a 128k -frame_duration 2.5 -c:a libopus celt_fb_stereo_2.5ms.opus
//
// plus re-muxed variants: spliced_mode_switches interleaves 7-packet runs of
// SILK, CELT and hybrid streams (mode switches without redundancy), plc_*
// replace packets with TOC-only packets (decoded as lost by libopus) and
// gain_* patches the OpusHead output gain. The reference is FFmpeg's libopus
// float decode, computed at test time when ffmpeg is installed.
const fixtureDir = "../../testdata/opus"

// readOggPackets is a minimal Ogg page reader (RFC 3533): it returns the
// packets of the first logical stream in order.
func readOggPackets(b []byte) ([][]byte, error) {
	var pkts [][]byte
	var cur []byte
	serial := uint32(0)
	first := true
	for len(b) > 0 {
		if len(b) < 27 || string(b[:4]) != "OggS" {
			return nil, errors.New("ogg: bad capture pattern")
		}
		nseg := int(b[26])
		if len(b) < 27+nseg {
			return nil, errors.New("ogg: truncated page header")
		}
		s := binary.LittleEndian.Uint32(b[14:])
		if first {
			serial = s
			first = false
		}
		lacing := b[27 : 27+nseg]
		body := b[27+nseg:]
		total := 0
		for _, l := range lacing {
			total += int(l)
		}
		if len(body) < total {
			return nil, errors.New("ogg: truncated page body")
		}
		if s == serial {
			off := 0
			for _, l := range lacing {
				cur = append(cur, body[off:off+int(l)]...)
				off += int(l)
				if l < 255 {
					pkts = append(pkts, cur)
					cur = nil
				}
			}
		}
		b = body[total:]
	}
	return pkts, nil
}

type fixture struct {
	head    Head
	packets [][]byte
}

func loadFixture(t testing.TB, path string) fixture {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	pkts, err := readOggPackets(data)
	if err != nil {
		t.Fatal(err)
	}
	if len(pkts) < 3 {
		t.Fatalf("%s: only %d packets", path, len(pkts))
	}
	h, err := ParseOpusHead(pkts[0])
	if err != nil {
		t.Fatal(err)
	}
	return fixture{head: h, packets: pkts[2:]}
}

// referenceDecode runs FFmpeg's libopus decoder (float build) on path.
func referenceDecode(t testing.TB, path string, rate int) []float32 {
	t.Helper()
	ff, err := exec.LookPath("ffmpeg")
	if err != nil {
		t.Skip("ffmpeg not installed; reference decode unavailable")
	}
	cmd := exec.Command(ff, "-hide_banner", "-loglevel", "error", "-request_sample_fmt", "flt", "-c:a", "libopus", "-i", path,
		"-f", "f32le", "-ar", fmt.Sprint(rate), "-")
	var out, stderr bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		t.Skipf("ffmpeg libopus decode failed: %v %s", err, stderr.String())
	}
	raw := out.Bytes()
	ref := make([]float32, len(raw)/4)
	for i := range ref {
		ref[i] = math.Float32frombits(binary.LittleEndian.Uint32(raw[4*i:]))
	}
	return ref
}

func snrDB(ref, got []float32) float64 {
	var sig, noise float64
	for i := range ref {
		r := float64(ref[i])
		e := r - float64(got[i])
		sig += r * r
		noise += e * e
	}
	if noise == 0 {
		return math.Inf(1)
	}
	return 10 * math.Log10(sig/noise)
}

func modeSummary(pkts [][]byte) string {
	counts := map[string]int{}
	for _, p := range pkts {
		if len(p) == 0 {
			continue
		}
		name := map[int]string{modeSILK: "SILK", modeHybrid: "Hybrid", modeCELT: "CELT"}[tocMode(p[0])]
		bw := []string{"NB", "MB", "WB", "SWB", "FB"}[tocBandwidth(p[0])]
		ms := float64(tocSamplesPerFrame(p[0], 48000)) / 48
		st := "mono"
		if p[0]&4 != 0 {
			st = "stereo"
		}
		counts[fmt.Sprintf("%s/%s/%gms/%s", name, bw, ms, st)]++
		code := int(p[0] & 3)
		counts[fmt.Sprintf("code%d", code)]++
		if code == 3 && len(p) > 1 && p[1]&0x40 != 0 {
			counts["padded"]++
		}
	}
	var keys []string
	for k := range counts {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var sb strings.Builder
	for _, k := range keys {
		fmt.Fprintf(&sb, "%s×%d ", k, counts[k])
	}
	return sb.String()
}

// vorbisToFFmpeg maps FFmpeg's channel order to the Vorbis (Opus mapping
// family 1) order for 5.1: FFmpeg emits FL FR FC LFE BL BR.
var vorbisToFFmpeg = map[int][]int{6: {0, 2, 1, 5, 3, 4}}

func decodeFixture(t testing.TB, fx fixture, rate int) ([]float32, int) {
	t.Helper()
	var out []float32
	var err error
	ch := fx.head.Channels
	if fx.head.MappingFamily == 0 {
		d, e := NewDecoder(rate, ch)
		if e != nil {
			t.Fatal(e)
		}
		d.SetGain(fx.head.OutputGain)
		for i, p := range fx.packets {
			if out, err = d.Decode(p, out); err != nil {
				t.Fatalf("packet %d: %v", i, err)
			}
		}
	} else {
		d, e := NewMultistreamDecoderRate(fx.head, rate)
		if e != nil {
			t.Fatal(e)
		}
		for i, p := range fx.packets {
			if out, err = d.Decode(p, out); err != nil {
				t.Fatalf("packet %d: %v", i, err)
			}
		}
	}
	skip := fx.head.PreSkip * rate / 48000 * ch
	if skip > len(out) {
		skip = len(out)
	}
	return out[skip:], ch
}

// minSNR is the required SNR against libopus' float decoder per fixture
// prefix.
var minSNR = map[string]float64{
	"celt":        60,
	"silk":        60,
	"hybrid":      60,
	"transitions": 60,
	"surround":    60,
}

func TestFixturesAgainstLibopus(t *testing.T) {
	files, _ := filepath.Glob(filepath.Join(fixtureDir, "*.opus"))
	if len(files) == 0 {
		t.Fatal("no fixtures")
	}
	for _, f := range files {
		name := strings.TrimSuffix(filepath.Base(f), ".opus")
		t.Run(name, func(t *testing.T) {
			fx := loadFixture(t, f)
			ref := referenceDecode(t, f, 48000)
			got, ch := decodeFixture(t, fx, 48000)
			if perm, ok := vorbisToFFmpeg[ch]; ok && fx.head.MappingFamily == 1 {
				re := make([]float32, len(got))
				for i := 0; i+ch <= len(got); i += ch {
					for j := 0; j < ch; j++ {
						re[i+j] = got[i+perm[j]]
					}
				}
				got = re
			}
			n := min(len(ref), len(got))
			if n == 0 || len(ref)-n > 960*ch || len(got)-n > 6000*ch {
				t.Fatalf("length mismatch: ref %d got %d", len(ref), len(got))
			}
			snr := snrDB(ref[:n], got[:n])
			want := 60.0
			for p, v := range minSNR {
				if strings.HasPrefix(name, p) {
					want = v
				}
			}
			t.Logf("%-24s SNR %7.2f dB  (%s)", name, snr, modeSummary(fx.packets))
			if snr < want {
				t.Errorf("SNR %.2f dB < %.0f dB", snr, want)
			}
		})
	}
}
