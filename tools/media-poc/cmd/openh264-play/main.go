// Command openh264-play is the "can we actually PLAY video" proof: it
// demuxes an MP4 with github.com/abema/go-mp4 and decodes EVERY frame
// (I/P/B) with Cisco OpenH264 loaded through purego (no cgo, no ffmpeg).
//
// It exists as the correctness oracle for the self-written pure-Go H.264
// decoder: run both over the same file and diff the decoded planes.
//
// Usage:
//
//	go run ./cmd/openh264-play -lib /path/to/libopenh264.dylib [-out dir] [-every N] [-max N] input.mp4
package main

import (
	"flag"
	"fmt"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"time"

	mp4 "github.com/abema/go-mp4"
	oh "github.com/y9o/go-openh264"

	"github.com/qizhanchan/qui/tools/media-poc/internal/mp4util"
)

func main() {
	lib := flag.String("lib", "libopenh264.dylib", "path to the OpenH264 shared library")
	out := flag.String("out", "", "if set, write decoded frames here (PNG)")
	every := flag.Int("every", 30, "with -out, dump every Nth frame")
	max := flag.Int("max", 0, "stop after N frames (0 = all)")
	flag.Parse()
	if flag.NArg() < 1 {
		fmt.Fprintln(os.Stderr, "usage: openh264-play -lib <libopenh264> [-out dir] [-every N] [-max N] <input.mp4>")
		os.Exit(2)
	}
	if err := run(*lib, flag.Arg(0), *out, *every, *max); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}

func run(lib, in, outDir string, every, max int) error {
	if err := oh.Open(lib); err != nil {
		return fmt.Errorf("openh264 Open(%s): %w", lib, err)
	}
	defer oh.Close()
	ver := oh.WelsGetCodecVersion()
	fmt.Printf("OpenH264 version %d.%d.%d\n", ver.UMajor, ver.UMinor, ver.URevision)

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
	fmt.Printf("video: %dx%d  samples=%d  lengthSize=%d  ~%.2fs\n",
		vt.AVC.Width, vt.AVC.Height, len(vt.Samples), vt.AVC.LengthSize,
		mp4util.Seconds(vt.Duration, vt.Timescale))

	sps, pps, err := mp4util.AVCCParameterSets(f)
	if err != nil {
		return err
	}
	idrs, _ := mp4.FindIDRFrames(f, &mp4.TrackInfo{AVC: vt.AVC, Chunks: vt.Chunks, Samples: vt.Samples})
	isIDR := make(map[int]bool, len(idrs))
	for _, i := range idrs {
		isIDR[i] = true
	}

	var dec *oh.ISVCDecoder
	if ret := oh.WelsCreateDecoder(&dec); ret != 0 || dec == nil {
		return fmt.Errorf("WelsCreateDecoder ret=%d", ret)
	}
	defer oh.WelsDestroyDecoder(dec)

	var param oh.SDecodingParam
	param.EEcActiveIdc = 1 // ERROR_CON_SLICE_COPY
	if ret := dec.Initialize(&param); ret != 0 {
		return fmt.Errorf("Initialize ret=%d", ret)
	}
	defer dec.Uninitialize()

	if outDir != "" {
		if err := os.MkdirAll(outDir, 0o755); err != nil {
			return err
		}
	}

	nDecoded := 0
	start := time.Now()
	for i := range vt.Samples {
		buf, err := mp4util.ReadSample(f, vt, i)
		if err != nil {
			return err
		}
		nalus, err := mp4util.SplitAVCC(buf, int(vt.AVC.LengthSize))
		if err != nil {
			return err
		}
		au := mp4util.AnnexB(sps, pps, nalus, isIDR[i] || i == 0)

		var dst [3][]byte
		var bi oh.SBufferInfo
		if ret := dec.DecodeFrameNoDelay(au, len(au), &dst, &bi); ret != 0 {
			return fmt.Errorf("decode sample %d: ret=%d", i, ret)
		}
		if bi.IBufferStatus == 1 {
			if outDir != "" && (nDecoded == 0 || nDecoded%every == 0) {
				if err := writeFrame(dst, &bi, filepath.Join(outDir, fmt.Sprintf("frame_%05d.png", nDecoded))); err != nil {
					return err
				}
			}
			nDecoded++
		}
		if max > 0 && nDecoded >= max {
			break
		}
	}
	for { // drain the reorder buffer
		var dst [3][]byte
		var bi oh.SBufferInfo
		if ret := dec.FlushFrame(&dst, &bi); ret != 0 || bi.IBufferStatus != 1 {
			break
		}
		nDecoded++
	}
	el := time.Since(start)
	fmt.Printf("DECODED %d frames in %v (%.2f ms/frame, %.1f fps) from %d samples\n",
		nDecoded, el, float64(el.Milliseconds())/float64(maxInt(nDecoded, 1)),
		float64(nDecoded)/el.Seconds(), len(vt.Samples))
	return nil
}

func writeFrame(dst [3][]byte, bi *oh.SBufferInfo, path string) error {
	sb := bi.UsrData_sSystemBuffer()
	w, h := int(sb.IWidth), int(sb.IHeight)
	ys, cs := int(sb.IStride[0]), int(sb.IStride[1])
	if w <= 0 || h <= 0 || len(dst[0]) == 0 {
		return fmt.Errorf("bad frame geometry %dx%d", w, h)
	}
	img := &image.YCbCr{
		Y:              append([]byte(nil), dst[0]...),
		Cb:             append([]byte(nil), dst[1]...),
		Cr:             append([]byte(nil), dst[2]...),
		YStride:        ys,
		CStride:        cs,
		SubsampleRatio: image.YCbCrSubsampleRatio420,
		Rect:           image.Rect(0, 0, w, h),
	}
	pf, err := os.Create(path)
	if err != nil {
		return err
	}
	defer pf.Close()
	return png.Encode(pf, img)
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
