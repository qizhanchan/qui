package h264enc

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

// Opt-in quality comparison against libx264:
//
//	H264ENC_YUV=clip.yuv H264ENC_SIZE=640x360 [H264ENC_X264="-preset ultrafast"] \
//	[H264ENC_CFG="preset=1,cabac=1,profile=100,8x8=1,bframes=2,refs=1"] \
//	go test -run TestQualityVsX264 -v ./media/internal/h264enc
//
// It encodes the raw I420 clip at several QPs with both encoders and reports
// bitrate, PSNR, speed and the BD-PSNR (average PSNR gap at equal bitrate).
func TestQualityVsX264(t *testing.T) {
	path := os.Getenv("H264ENC_YUV")
	if path == "" {
		t.Skip("H264ENC_YUV not set")
	}
	frames := loadYUV(t, 0)
	w, h := frames[0].Rect.Dx(), frames[0].Rect.Dy()
	n := len(frames)
	fsz := w * h * 3 / 2
	raw, _ := os.ReadFile(path)
	cfg := parseCfg(t, os.Getenv("H264ENC_CFG"))
	cfg.Width, cfg.Height = w, h
	x264 := os.Getenv("H264ENC_X264")
	if x264 == "" {
		x264 = "-preset ultrafast"
	}
	qps := []int{22, 27, 32, 37}
	var ours, ref [][2]float64 // (log rate, psnr)
	src := raw[:n*fsz]
	dir := t.TempDir()
	for _, qp := range qps {
		c := cfg
		c.QP = qp
		start := time.Now()
		r := encodeAll(t, c, frames)
		el := time.Since(start)
		p := psnr(bytes.Join(r.recon, nil), src)
		kbps := float64(r.bits) / (float64(n) / 30) / 1000
		ours = append(ours, [2]float64{math.Log(kbps), p})

		in := filepath.Join(dir, "x.264")
		args := []string{"-v", "error", "-y", "-f", "rawvideo", "-pix_fmt", "yuv420p", "-s", fmt.Sprintf("%dx%d", w, h),
			"-r", "30", "-i", path, "-frames:v", strconv.Itoa(n), "-c:v", "libx264"}
		args = append(args, strings.Fields(x264)...)
		args = append(args, "-qp", strconv.Itoa(qp), in)
		t0 := time.Now()
		if out, err := exec.Command("ffmpeg", args...).CombinedOutput(); err != nil {
			t.Fatalf("x264: %v %s", err, out)
		}
		xel := time.Since(t0)
		dec := filepath.Join(dir, "x.yuv")
		if out, err := exec.Command("ffmpeg", "-v", "error", "-y", "-i", in, "-f", "rawvideo", "-pix_fmt", "yuv420p", dec).CombinedOutput(); err != nil {
			t.Fatalf("decode: %v %s", err, out)
		}
		xs, _ := os.Stat(in)
		xd, _ := os.ReadFile(dec)
		xp := psnr(xd, src)
		xk := float64(xs.Size()*8) / (float64(n) / 30) / 1000
		ref = append(ref, [2]float64{math.Log(xk), xp})
		t.Logf("QP %d: ours %7.0f kbps %.2f dB %5.1f fps | x264 %7.0f kbps %.2f dB %5.1f fps",
			qp, kbps, p, float64(n)/el.Seconds(), xk, xp, float64(n)/xel.Seconds())
	}
	t.Logf("BD-PSNR (ours - x264): %+.2f dB", bdPSNR(ours, ref))
}

// loadYUV loads up to n frames (0 = H264ENC_FRAMES or all) of the raw
// I420 clip H264ENC_YUV sized H264ENC_SIZE.
func loadYUV(t testing.TB, n int) []*image.YCbCr {
	var w, h int
	fmt.Sscanf(os.Getenv("H264ENC_SIZE"), "%dx%d", &w, &h)
	raw, err := os.ReadFile(os.Getenv("H264ENC_YUV"))
	if err != nil {
		t.Fatal(err)
	}
	fsz := w * h * 3 / 2
	total := len(raw) / fsz
	if n == 0 {
		n = total
		if v, _ := strconv.Atoi(os.Getenv("H264ENC_FRAMES")); v > 0 && v < n {
			n = v
		}
	}
	frames := make([]*image.YCbCr, n)
	for i := range frames {
		f := raw[(i%total)*fsz : (i%total+1)*fsz]
		frames[i] = &image.YCbCr{
			Y: f[:w*h], Cb: f[w*h : w*h*5/4], Cr: f[w*h*5/4:],
			YStride: w, CStride: w / 2, SubsampleRatio: image.YCbCrSubsampleRatio420,
			Rect: image.Rect(0, 0, w, h),
		}
	}
	return frames
}

func parseCfg(t *testing.T, s string) Config {
	var c Config
	for _, kv := range strings.Split(s, ",") {
		k, v, ok := strings.Cut(kv, "=")
		if !ok {
			continue
		}
		n, _ := strconv.Atoi(v)
		switch k {
		case "preset":
			c.Preset = n
		case "cabac":
			c.CABAC = n != 0
		case "profile":
			c.Profile = n
		case "8x8":
			c.Transform8x8 = n != 0
		case "bframes":
			c.BFrames = n
		case "refs":
			c.Refs = n
		case "slices":
			c.Slices = n
		case "keyint":
			c.KeyInt = n
		default:
			t.Fatalf("unknown cfg key %q", k)
		}
	}
	return c
}

// bdPSNR integrates the piecewise-linear PSNR(log rate) curves over their
// common rate range and returns the mean difference a - b.
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
