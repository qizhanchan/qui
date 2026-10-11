package hevc

import (
	"bytes"
	"fmt"
	"image"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"
)

// TestQualityVsX265 (opt-in) compares rate and PSNR against libx265 over
// four QPs and reports BD-PSNR (positive: we are better at equal rate).
//
//	HEVC_ENC_YUV=clip.yuv HEVC_ENC_SIZE=416x240 [HEVC_ENC_FRAMES=30]
//	[HEVC_ENC_CFG="Preset=2 CTU=64"] [HEVC_X265="-preset ultrafast"]
func TestQualityVsX265(t *testing.T) {
	path := os.Getenv("HEVC_ENC_YUV")
	if path == "" {
		t.Skip("HEVC_ENC_YUV not set")
	}
	var w, h int
	fmt.Sscanf(os.Getenv("HEVC_ENC_SIZE"), "%dx%d", &w, &h)
	n := 30
	if v, err := strconv.Atoi(os.Getenv("HEVC_ENC_FRAMES")); err == nil {
		n = v
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	fs := w * h * 3 / 2
	n = min(n, len(raw)/fs)
	src := raw[:n*fs]
	frames := make([]*image.YCbCr, n)
	for i := range frames {
		img := image.NewYCbCr(image.Rect(0, 0, w, h), image.YCbCrSubsampleRatio420)
		f := src[i*fs:]
		copy(img.Y, f[:w*h])
		copy(img.Cb, f[w*h:w*h+w*h/4])
		copy(img.Cr, f[w*h+w*h/4:fs])
		frames[i] = img
	}
	cfg := EncoderConfig{Width: w, Height: h}
	for _, kv := range strings.Fields(os.Getenv("HEVC_ENC_CFG")) {
		k, v, _ := strings.Cut(kv, "=")
		iv, _ := strconv.Atoi(v)
		switch k {
		case "Preset":
			cfg.Preset = iv
		case "CTU":
			cfg.CTU = iv
		case "Refs":
			cfg.Refs = iv
		case "KeyInt":
			cfg.KeyInt = iv
		case "BFrames":
			cfg.BFrames = iv
		case "NoSAO":
			cfg.NoSAO = iv != 0
		case "WPP":
			cfg.WPP = iv != 0
		}
	}
	x265 := os.Getenv("HEVC_X265")
	if x265 == "" {
		x265 = "-preset ultrafast"
	}
	dir := t.TempDir()
	var ours, ref [][2]float64
	for _, qp := range []int{22, 27, 32, 37} {
		c := cfg
		c.QP = qp
		start := time.Now()
		stream, recon := encodeAll(t, c, frames)
		el := time.Since(start)
		var all []byte
		for _, f := range recon {
			all = append(all, framePlanes(f)...)
		}
		kbps := float64(len(stream)*8) / (float64(n) / 30) / 1000
		p := yuvPSNR(all, src, w, h)
		ours = append(ours, [2]float64{math.Log(kbps), p})

		in := filepath.Join(dir, "in.yuv")
		os.WriteFile(in, src, 0o644)
		out := filepath.Join(dir, "x.265")
		args := []string{"-v", "error", "-y", "-f", "rawvideo", "-pix_fmt", "yuv420p", "-s", fmt.Sprintf("%dx%d", w, h), "-r", "30", "-i", in, "-c:v", "libx265"}
		args = append(args, strings.Fields(x265)...)
		args = append(args, "-x265-params", fmt.Sprintf("log-level=0:qp=%d", qp), out)
		if msg, err := exec.Command("ffmpeg", args...).CombinedOutput(); err != nil {
			t.Fatalf("x265: %v %s", err, msg)
		}
		xs, _ := os.ReadFile(out)
		dec, err := exec.Command("ffmpeg", "-v", "error", "-i", out, "-f", "rawvideo", "-pix_fmt", "yuv420p", "-").Output()
		if err != nil {
			t.Fatal(err)
		}
		xk := float64(len(xs)*8) / (float64(n) / 30) / 1000
		xp := yuvPSNR(dec, src, w, h)
		ref = append(ref, [2]float64{math.Log(xk), xp})
		t.Logf("QP %d: ours %.1f kbps %.2f dB (%.1f ms/frame) | x265 %.1f kbps %.2f dB", qp, kbps, p, float64(el.Microseconds())/1000/float64(n), xk, xp)
	}
	t.Logf("BD-PSNR vs x265 %s: %+.2f dB", x265, bdPSNR(ours, ref))
}

// yuvPSNR is the PSNR of I420 frames, luma and chroma weighted 4:1:1.
func yuvPSNR(a, b []byte, w, h int) float64 {
	fs := w * h * 3 / 2
	var sy, sc float64
	for f := 0; f+fs <= len(a) && f+fs <= len(b); f += fs {
		for i := 0; i < w*h; i++ {
			d := float64(a[f+i]) - float64(b[f+i])
			sy += d * d
		}
		for i := w * h; i < fs; i++ {
			d := float64(a[f+i]) - float64(b[f+i])
			sc += d * d
		}
	}
	nf := float64(len(a) / fs)
	py := 10 * math.Log10(255*255/(sy/(nf*float64(w*h))+1e-9))
	pc := 10 * math.Log10(255*255/(sc/(nf*float64(w*h/2))+1e-9))
	return (4*py + 2*pc) / 6
}

func bdPSNR(a, b [][2]float64) float64 {
	sort.Slice(a, func(i, j int) bool { return a[i][0] < a[j][0] })
	sort.Slice(b, func(i, j int) bool { return b[i][0] < b[j][0] })
	lo := math.Max(a[0][0], b[0][0])
	hi := math.Min(a[len(a)-1][0], b[len(b)-1][0])
	if hi <= lo {
		return math.NaN()
	}
	interp := func(c [][2]float64, x float64) float64 {
		for i := 1; i < len(c); i++ {
			if x <= c[i][0] {
				f := (x - c[i-1][0]) / (c[i][0] - c[i-1][0])
				return c[i-1][1] + f*(c[i][1]-c[i-1][1])
			}
		}
		return c[len(c)-1][1]
	}
	const steps = 200
	var s float64
	for i := 0; i <= steps; i++ {
		x := lo + (hi-lo)*float64(i)/steps
		s += interp(a, x) - interp(b, x)
	}
	return s / (steps + 1)
}

var _ = bytes.Equal
