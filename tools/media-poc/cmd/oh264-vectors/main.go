// Command oh264-vectors generates a small Baseline/CAVLC H.264 stream with
// inter (P) frames using Cisco OpenH264's encoder, then decodes it with
// OpenH264's own decoder and prints per-frame plane hashes as the pixel-exact
// oracle for our self-written decoder's H4/H5 milestones.
//
// Usage:
//
//	go run ./cmd/oh264-vectors -lib /path/to/libopenh264.dylib \
//	    [-w 64] [-h 48] [-frames 4] [-qp 26] [-intra 30] [-deblock]
package main

import (
	"crypto/sha256"
	"encoding/hex"
	"flag"
	"fmt"
	"math"
	"os"
	"runtime"
	"unsafe"

	oh "github.com/y9o/go-openh264"
)

func main() {
	lib := flag.String("lib", "libopenh264.dylib", "path to the OpenH264 shared library")
	w := flag.Int("w", 64, "luma width")
	h := flag.Int("h", 48, "luma height")
	frames := flag.Int("frames", 4, "number of frames")
	qp := flag.Int("qp", 26, "base QP (layer QP)")
	intra := flag.Int("intra", 30, "intra period (frames between IDRs)")
	deblock := flag.Bool("deblock", false, "enable the in-loop deblocking filter")
	noise := flag.Bool("noise", false, "use a high-detail pattern (forces I4x4 intra)")
	high := flag.Bool("high", false, "High profile (enables the 8x8 transform)")
	out := flag.String("out", "", "write the Annex-B stream here")
	dump := flag.String("dump", "", "write per-frame raw Y/Cb/Cr planes into this directory")
	flag.Parse()
	if err := run(*lib, *w, *h, *frames, *qp, *intra, *deblock, *out, *dump, *noise, *high); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}

func run(lib string, w, h, nFrames, qp, intra int, deblock bool, outPath, dumpDir string, noise, high bool) error {
	if err := oh.Open(lib); err != nil {
		return fmt.Errorf("openh264 Open: %w", err)
	}
	defer oh.Close()

	// --- encode ---
	var enc *oh.ISVCEncoder
	if ret := oh.WelsCreateSVCEncoder(&enc); ret != 0 || enc == nil {
		return fmt.Errorf("WelsCreateSVCEncoder: %d", ret)
	}
	defer oh.WelsDestroySVCEncoder(enc)

	var param oh.SEncParamExt
	if ret := enc.GetDefaultParams(&param); ret != 0 {
		return fmt.Errorf("GetDefaultParams: %d", ret)
	}
	param.IUsageType = oh.CAMERA_VIDEO_REAL_TIME
	param.IPicWidth = int32(w)
	param.IPicHeight = int32(h)
	param.ITargetBitrate = 500_000
	param.IRCMode = 0 // quality
	param.FMaxFrameRate = 30
	param.ISpatialLayerNum = 1
	param.ITemporalLayerNum = 1
	param.IEntropyCodingModeFlag = 0 // CAVLC (Baseline)
	param.INumRefFrame = 1
	param.UiIntraPeriod = uint32(intra)
	param.ILoopFilterDisableIdc = 0
	if !deblock {
		param.ILoopFilterDisableIdc = 1
	}
	sl := &param.SSpatialLayers[0]
	sl.IVideoWidth = int32(w)
	sl.IVideoHeight = int32(h)
	sl.FFrameRate = 30
	sl.ISpatialBitrate = 500_000
	sl.IMaxSpatialBitrate = 500_000
	sl.UiProfileIdc = 66 // Baseline
	if high {
		sl.UiProfileIdc = 100 // High (enables 8x8 transform)
	}
	sl.UiLevelIdc = 30
	sl.IDLayerQp = int32(qp)
	if ret := enc.InitializeExt(&param); ret != 0 {
		return fmt.Errorf("InitializeExt: %d", ret)
	}
	defer enc.Uninitialize()

	pinner := &runtime.Pinner{}
	defer pinner.Unpin()

	var stream []byte
	cw, ch := (w+1)/2, (h+1)/2
	// Source planes are kept alive across the encode call; Pin them.
	yPlane := make([]byte, w*h)
	cbPlane := make([]byte, cw*ch)
	crPlane := make([]byte, cw*ch)
	pinner.Pin(&yPlane[0])
	pinner.Pin(&cbPlane[0])
	pinner.Pin(&crPlane[0])

	frameTypes := make([]uint32, 0, nFrames)
	for k := 0; k < nFrames; k++ {
		genFrame(yPlane, cbPlane, crPlane, w, h, k, noise)
		src := oh.SSourcePicture{
			IColorFormat: oh.VideoFormatI420,
			IPicWidth:    int32(w),
			IPicHeight:   int32(h),
			UiTimeStamp:  int64(k),
		}
		src.IStride[0] = int32(w)
		src.IStride[1] = int32(cw)
		src.IStride[2] = int32(cw)
		src.PData[0] = (*uint8)(unsafe.Pointer(&yPlane[0]))
		src.PData[1] = (*uint8)(unsafe.Pointer(&cbPlane[0]))
		src.PData[2] = (*uint8)(unsafe.Pointer(&crPlane[0]))

		var info oh.SFrameBSInfo
		if ret := enc.EncodeFrame(&src, &info); ret != oh.CmResultSuccess {
			return fmt.Errorf("EncodeFrame %d: %d", k, ret)
		}
		frameTypes = append(frameTypes, info.EFrameType)
		if os.Getenv("DBG") != "" && k < 2 {
			fmt.Fprintf(os.Stderr, "frame %d type=%d layers=%d\n", k, info.EFrameType, info.ILayerNum)
		}
		if info.EFrameType == oh.VideoFrameTypeSkip {
			continue
		}
		for layer := 0; layer < int(info.ILayerNum); layer++ {
			li := &info.SLayerInfo[layer]
			lengths := unsafe.Slice(li.PNalLengthInByte, li.INalCount)
			var total int32
			for _, l := range lengths {
				total += l
			}
			nals := unsafe.Slice(li.PBsBuf, total)
			cp := make([]byte, total)
			copy(cp, nals)
			if os.Getenv("DBG") != "" && k == 0 {
				n := len(cp)
				if n > 12 {
					n = 12
				}
				fmt.Fprintf(os.Stderr, "frame0 layer%d nals=%d lens=%v cp=% x\n", layer, li.INalCount, lengths, cp[:n])
			}
			// OpenH264's bitstream buffer already contains each NAL with a
			// 4-byte Annex-B start code; the reported lengths include it.
			stream = append(stream, cp...)
			_ = lengths
		}
	}

	// --- decode with OpenH264 as the oracle ---
	if outPath != "" {
		if err := os.WriteFile(outPath, stream, 0o644); err != nil {
			return err
		}
	}
	if os.Getenv("DBG") != "" {
		fmt.Fprintf(os.Stderr, "stream=%d bytes\n", len(stream))
		n := len(stream)
		if n > 64 {
			n = 64
		}
		fmt.Fprintf(os.Stderr, "first %d bytes: % x\n", n, stream[:n])
		for _, nal := range splitAnnexB(stream) {
			if len(nal) == 0 {
				continue
			}
			fmt.Fprintf(os.Stderr, "  nal type=%d ref=%d len=%d\n", nal[0]&0x1f, (nal[0]>>5)&3, len(nal))
		}
	}
	hashes, err := oracleDecode(stream, w, h, dumpDir)
	if err != nil {
		return err
	}

	fmt.Printf("// dims=%dx%d frames=%d entropy=CAVLC profile=baseline deblock=%v\n", w, h, nFrames, deblock)
	fmt.Printf("// frametypes=%v\n", frameTypes)
	fmt.Printf("// stream=%q\n", hex.EncodeToString(stream))
	for i, hs := range hashes {
		fmt.Printf("// frame %d: Y=%s Cb=%s Cr=%s\n", i, hs[0], hs[1], hs[2])
	}
	return nil
}

// genFrame writes a deterministic sub-pixel-translating texture so the encoder
// produces inter frames with (fractional) motion vectors.
func genFrame(y, cb, cr []byte, w, h, k int, noise bool) {
	if noise {
		for row := 0; row < h; row++ {
			for col := 0; col < w; col++ {
				s := uint32(col+k) * 2654435761
				t := uint32(row) * 40503
				v := 128 + int((s^(t+(s>>3)))%160) - 80
				y[row*w+col] = clampByte(float64(v))
			}
		}
		cw, ch := (w+1)/2, (h+1)/2
		for row := 0; row < ch; row++ {
			for col := 0; col < cw; col++ {
				s := uint32(col*2+k) * 2246822519
				t := uint32(row*2) * 3266489917
				cb[row*cw+col] = clampByte(float64(128 + int((s^t)%120) - 60))
				cr[row*cw+col] = clampByte(float64(128 + int((s*3^t)%120) - 60))
			}
		}
		return
	}
	dx := float64(k) * 0.5
	dy := float64(k) * 0.375
	for row := 0; row < h; row++ {
		for col := 0; col < w; col++ {
			s := float64(col) - dx
			t := float64(row) - dy
			v := 128 + 70*math.Sin(s*0.20)*math.Sin(t*0.17) + 40*math.Sin(s*0.53+t*0.61)
			y[row*w+col] = clampByte(v)
		}
	}
	cw, ch := (w+1)/2, (h+1)/2
	for row := 0; row < ch; row++ {
		for col := 0; col < cw; col++ {
			s := float64(col) - dx*0.5
			t := float64(row) - dy*0.5
			cb[row*cw+col] = clampByte(128 + 50*math.Sin(s*0.30+t*0.11))
			cr[row*cw+col] = clampByte(128 + 50*math.Cos(s*0.22-t*0.19))
		}
	}
}

func clampByte(v float64) byte {
	if v < 0 {
		return 0
	}
	if v > 255 {
		return 255
	}
	return byte(v + 0.5)
}

// oracleDecode decodes an Annex-B stream with OpenH264 and returns per-frame
// Y/Cb/Cr SHA-256 hashes.
func oracleDecode(stream []byte, w, h int, dumpDir string) ([][3]string, error) {
	var dec *oh.ISVCDecoder
	if ret := oh.WelsCreateDecoder(&dec); ret != 0 || dec == nil {
		return nil, fmt.Errorf("WelsCreateDecoder: %d", ret)
	}
	defer oh.WelsDestroyDecoder(dec)
	dp := oh.SDecodingParam{}
	dp.EEcActiveIdc = oh.ERROR_CON_SLICE_MV_COPY_CROSS_IDR_FREEZE_RES_CHANGE
	if r := dec.Initialize(&dp); r != 0 {
		return nil, fmt.Errorf("decoder Initialize: %d", r)
	}
	defer dec.Uninitialize()

	cw, ch := (w+1)/2, (h+1)/2
	var out [][3]string

	handle := func(pDst [3][]byte, info *oh.SBufferInfo) {
		if pDst[0] == nil {
			return
		}
		sys := info.UsrData_sSystemBuffer()
		yStride := int(sys.IStride[0])
		cStride := int(sys.IStride[1])
		y := make([]byte, w*h)
		for r := 0; r < h; r++ {
			copy(y[r*w:], pDst[0][r*yStride:r*yStride+w])
		}
		cb := make([]byte, cw*ch)
		cr := make([]byte, cw*ch)
		for r := 0; r < ch; r++ {
			copy(cb[r*cw:], pDst[1][r*cStride:r*cStride+cw])
			copy(cr[r*cw:], pDst[2][r*cStride:r*cStride+cw])
		}
		out = append(out, [3]string{hash(y), hash(cb), hash(cr)})
		if dumpDir != "" {
			n := len(out) - 1
			_ = os.WriteFile(fmt.Sprintf("%s/frame%d_Y.raw", dumpDir, n), y, 0o644)
			_ = os.WriteFile(fmt.Sprintf("%s/frame%d_Cb.raw", dumpDir, n), cb, 0o644)
			_ = os.WriteFile(fmt.Sprintf("%s/frame%d_Cr.raw", dumpDir, n), cr, 0o644)
		}
	}

	rest := stream
	for len(rest) > 4 {
		pos := indexStartCode(rest[4:])
		length := len(rest)
		if pos != -1 {
			length = pos + 4
		}
		var pDst [3][]byte
		var info oh.SBufferInfo
		if r := dec.DecodeFrameNoDelay(rest[:length], length, &pDst, &info); r != 0 {
			return nil, fmt.Errorf("DecodeFrameNoDelay: %d", r)
		}
		handle(pDst, &info)
		if pos == -1 {
			break
		}
		rest = rest[pos+4:]
	}
	var remaining int
	dec.GetOption(oh.DECODER_OPTION_NUM_OF_FRAMES_REMAINING_IN_BUFFER, &remaining)
	for i := 0; i < remaining; i++ {
		var pDst [3][]byte
		var info oh.SBufferInfo
		if r := dec.FlushFrame(&pDst, &info); r != 0 {
			return nil, fmt.Errorf("FlushFrame: %d", r)
		}
		handle(pDst, &info)
	}
	return out, nil
}

func indexStartCode(b []byte) int {
	for i := 0; i+3 < len(b); i++ {
		if b[i] == 0 && b[i+1] == 0 && b[i+2] == 0 && b[i+3] == 1 {
			return i
		}
	}
	return -1
}

// splitAnnexB splits an Annex-B byte stream into NAL units (without start codes).
func splitAnnexB(b []byte) [][]byte {
	var out [][]byte
	i := 0
	// find first start code
	for i+3 < len(b) {
		if b[i] == 0 && b[i+1] == 0 && b[i+2] == 0 && b[i+3] == 1 {
			i += 4
			break
		}
		i++
	}
	for i < len(b) {
		start := i
		next := -1
		for j := i; j+3 < len(b); j++ {
			if b[j] == 0 && b[j+1] == 0 && b[j+2] == 0 && b[j+3] == 1 {
				next = j
				break
			}
		}
		if next == -1 {
			out = append(out, b[start:])
			break
		}
		out = append(out, b[start:next])
		i = next + 4
	}
	return out
}

func hash(b []byte) string {
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}
