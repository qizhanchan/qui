package av1

import (
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

// TestQualityVsSVT (opt-in) compares rate and PSNR against libsvtav1 and
// reports BD-PSNR (positive: we are better at equal rate).
//
//	AV1_ENC_YUV=clip.yuv AV1_ENC_SIZE=640x360 [AV1_ENC_FRAMES=10]
//	[AV1_SVT="-preset 10"] [AV1_SVT_PARAMS="keyint=1"]
func TestQualityVsSVT(t *testing.T) {
	path := os.Getenv("AV1_ENC_YUV")
	if path == "" {
		t.Skip("AV1_ENC_YUV not set")
	}
	var w, h int
	fmt.Sscanf(os.Getenv("AV1_ENC_SIZE"), "%dx%d", &w, &h)
	n := 10
	if v, err := strconv.Atoi(os.Getenv("AV1_ENC_FRAMES")); err == nil {
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
	svt := os.Getenv("AV1_SVT")
	if svt == "" {
		svt = "-preset 10"
	}
	svtParams := os.Getenv("AV1_SVT_PARAMS")
	if svtParams == "" {
		svtParams = "keyint=1"
	}
	dir := t.TempDir()
	in := filepath.Join(dir, "in.yuv")
	os.WriteFile(in, src, 0o644)
	var ours, ref [][2]float64
	for _, qp := range []int{24, 32, 40, 48} {
		qindex := qp * 4
		cfg := EncoderConfig{Width: w, Height: h, QIndex: qindex}
		if v, err := strconv.Atoi(os.Getenv("AV1_ENC_KEYINT")); err == nil {
			cfg.KeyInt = v
		}
		start := time.Now()
		e, err := NewEncoder(cfg)
		if err != nil {
			t.Fatal(err)
		}
		e.keepRecon = true
		size := 0
		for _, img := range frames {
			p, err := e.Encode(img)
			if err != nil {
				t.Fatal(err)
			}
			size += len(p.Data)
		}
		el := time.Since(start)
		var all []byte
		for _, f := range e.recon {
			all = append(all, framePlanes(f)...)
		}
		kbps := float64(size*8) / (float64(n) / 30) / 1000
		p := yuvPSNR(all, src, w, h)
		ours = append(ours, [2]float64{math.Log(kbps), p})

		out := filepath.Join(dir, "s.ivf")
		args := []string{"-v", "error", "-y", "-f", "rawvideo", "-pix_fmt", "yuv420p", "-s", fmt.Sprintf("%dx%d", w, h), "-r", "30", "-i", in, "-c:v", "libsvtav1"}
		args = append(args, strings.Fields(svt)...)
		args = append(args, "-svtav1-params", fmt.Sprintf("rc=0:qp=%d:%s", qp, svtParams), out)
		if msg, err := exec.Command("ffmpeg", args...).CombinedOutput(); err != nil {
			t.Fatalf("svt: %v %s", err, msg)
		}
		data, _ := os.ReadFile(out)
		tus, _, _, _ := ReadIVF(data)
		ss := 0
		for _, tu := range tus {
			ss += len(tu)
		}
		dec, err := exec.Command("ffmpeg", "-v", "error", "-i", out, "-f", "rawvideo", "-pix_fmt", "yuv420p", "-").Output()
		if err != nil {
			t.Fatal(err)
		}
		sk := float64(ss*8) / (float64(n) / 30) / 1000
		sp := yuvPSNR(dec, src, w, h)
		ref = append(ref, [2]float64{math.Log(sk), sp})
		t.Logf("q %d (qindex %d): ours %.1f kbps %.2f dB (%.0f ms/frame) | svt %.1f kbps %.2f dB", qp, qindex, kbps, p, float64(el.Microseconds())/1000/float64(n), sk, sp)
	}
	t.Logf("BD-PSNR vs SVT-AV1 %s %s: %+.2f dB", svt, svtParams, bdPSNR(ours, ref))
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
