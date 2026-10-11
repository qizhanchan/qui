// Command keyframe demuxes an MP4 and decodes an IDR (key) frame with the
// pure-Go hi264 decoder (IDR-only — no inter frames), writing a PNG.
//
// This is the "video thumbnail / first-frame" capability: fully pure Go,
// MIT-licensed, no native dependency at all. It cannot play video.
//
// Usage:
//
//	go run ./cmd/keyframe [-idr N] [-bench N] input.mp4 [out.png]
package main

import (
	"flag"
	"fmt"
	"image"
	"image/png"
	"os"
	"time"

	"github.com/Eyevinn/hi264/pkg/decoder"
	hframe "github.com/Eyevinn/hi264/pkg/frame"
	"github.com/Eyevinn/hi264/pkg/yuv"
	mp4 "github.com/abema/go-mp4"

	"github.com/qizhanchan/qui/tools/media-poc/internal/mp4util"
)

func main() {
	ord := flag.Int("idr", 0, "which IDR frame to decode (0 = first)")
	benchN := flag.Int("bench", 0, "decode up to N IDR frames and report timing")
	flag.Parse()
	args := flag.Args()
	if len(args) < 1 {
		fmt.Fprintln(os.Stderr, "usage: keyframe [-idr N] [-bench N] <input.mp4> [out.png]")
		os.Exit(2)
	}
	out := "frame0.png"
	if len(args) >= 2 {
		out = args[1]
	}
	if err := run(args[0], out, *ord, *benchN); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}

func run(in, out string, ord, benchN int) error {
	f, err := os.Open(in)
	if err != nil {
		return err
	}
	defer f.Close()

	info, err := mp4.Probe(f)
	if err != nil {
		return fmt.Errorf("probe: %w", err)
	}
	vt := mp4util.VideoTrack(info)
	if vt == nil {
		return fmt.Errorf("no H.264 (avc1) track in %s", in)
	}
	fmt.Printf("video: %dx%d  samples=%d  ~%.2fs\n",
		vt.AVC.Width, vt.AVC.Height, len(vt.Samples),
		mp4util.Seconds(vt.Duration, vt.Timescale))

	sps, pps, err := mp4util.AVCCParameterSets(f)
	if err != nil {
		return err
	}
	idrs, err := mp4.FindIDRFrames(f, &mp4.TrackInfo{AVC: vt.AVC, Chunks: vt.Chunks, Samples: vt.Samples})
	if err != nil {
		return err
	}
	fmt.Printf("IDR sample indices (first 8): %v\n", mp4util.FirstN(idrs, 8))
	if len(idrs) == 0 {
		return fmt.Errorf("no IDR frame found")
	}

	if benchN > 0 {
		if benchN > len(idrs) {
			benchN = len(idrs)
		}
		start := time.Now()
		okN := 0
		for k := 0; k < benchN; k++ {
			buf, err := mp4util.ReadSample(f, vt, idrs[k])
			if err != nil {
				return err
			}
			nalus, err := mp4util.SplitAVCC(buf, int(vt.AVC.LengthSize))
			if err != nil {
				return err
			}
			full := append(append(append([][]byte{}, sps...), pps...), nalus...)
			if _, err := decoder.New().DecodeNALUs(full); err != nil {
				continue
			}
			okN++
		}
		el := time.Since(start)
		fmt.Printf("BENCH: decoded %d/%d IDR frames in %v (%.1f ms/frame, %dx%d)\n",
			okN, benchN, el, float64(el.Milliseconds())/float64(maxInt(benchN, 1)),
			vt.AVC.Width, vt.AVC.Height)
		return nil
	}

	if ord >= len(idrs) {
		ord = len(idrs) - 1
	}
	buf, err := mp4util.ReadSample(f, vt, idrs[ord])
	if err != nil {
		return err
	}
	nalus, err := mp4util.SplitAVCC(buf, int(vt.AVC.LengthSize))
	if err != nil {
		return err
	}
	dec := decoder.New()
	fr, err := dec.DecodeNALUs(append(append(append([][]byte{}, sps...), pps...), nalus...))
	if err != nil {
		return fmt.Errorf("hi264 decode: %w", err)
	}
	fmt.Printf("DECODED %dx%d (matrix=%d fullRange=%v)\n",
		fr.Width, fr.Height, fr.MatrixCoefficients, fr.VideoFullRangeFlag)

	img := frameToRGBA(fr)
	pf, err := os.Create(out)
	if err != nil {
		return err
	}
	defer pf.Close()
	if err := png.Encode(pf, img); err != nil {
		return err
	}
	fmt.Printf("wrote %s\n", out)
	return nil
}

// frameToRGBA converts hi264's planar YUV420 into RGB, honoring the
// stream's color matrix / range when present (else BT.601 limited).
func frameToRGBA(f *hframe.Frame) *image.RGBA {
	cs := yuv.BT601
	switch f.MatrixCoefficients {
	case 1:
		cs = yuv.BT709
	case 9:
		cs = yuv.BT2020
	}
	var rng yuv.Range
	if f.VideoFullRangeFlag {
		rng = yuv.FullRange
	}
	img := image.NewRGBA(image.Rect(0, 0, f.Width, f.Height))
	for y := 0; y < f.Height; y++ {
		for x := 0; x < f.Width; x++ {
			Y := f.Y[y*f.StrideY+x]
			Cb := f.Cb[(y/2)*f.StrideC+x/2]
			Cr := f.Cr[(y/2)*f.StrideC+x/2]
			r, g, b := yuv.YCbCrToRGBCS(int(Y), int(Cb), int(Cr), cs, rng)
			i := y*img.Stride + x*4
			img.Pix[i], img.Pix[i+1], img.Pix[i+2], img.Pix[i+3] = r, g, b, 255
		}
	}
	return img
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
