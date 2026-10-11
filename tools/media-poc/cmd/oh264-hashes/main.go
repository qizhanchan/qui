// Command oh264-hashes decodes a raw Annex-B H.264 stream with Cisco OpenH264
// (via purego) and prints the frame dimensions plus per-frame Y/Cb/Cr SHA-256
// hashes. It is the pixel-exact oracle for self-written streams that are not
// packaged in an MP4 (e.g. the OpenH264 conformance vectors used for B slices).
//
// Usage:
//
//	go run ./cmd/oh264-hashes -lib /path/to/libopenh264.dylib stream.264
package main

import (
	"crypto/sha256"
	"encoding/hex"
	"flag"
	"fmt"
	"os"

	oh "github.com/y9o/go-openh264"
)

func main() {
	lib := flag.String("lib", "libopenh264.dylib", "path to the OpenH264 shared library")
	dump := flag.String("dump", "", "write per-frame raw Y/Cb/Cr planes into this directory")
	flag.Parse()
	if flag.NArg() < 1 {
		fmt.Fprintln(os.Stderr, "usage: oh264-hashes -lib <libopenh264> <stream.264>")
		os.Exit(2)
	}
	if err := run(*lib, flag.Arg(0), *dump); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}

func run(lib, in, dumpDir string) error {
	if err := oh.Open(lib); err != nil {
		return err
	}
	defer oh.Close()
	stream, err := os.ReadFile(in)
	if err != nil {
		return err
	}

	var dec *oh.ISVCDecoder
	if ret := oh.WelsCreateDecoder(&dec); ret != 0 || dec == nil {
		return fmt.Errorf("WelsCreateDecoder: %d", ret)
	}
	defer oh.WelsDestroyDecoder(dec)
	dp := oh.SDecodingParam{}
	dp.EEcActiveIdc = oh.ERROR_CON_SLICE_MV_COPY_CROSS_IDR_FREEZE_RES_CHANGE
	if r := dec.Initialize(&dp); r != 0 {
		return fmt.Errorf("Initialize: %d", r)
	}
	defer dec.Uninitialize()

	var w, h, frameNo int
	handle := func(pDst [3][]byte, info *oh.SBufferInfo) {
		if pDst[0] == nil {
			return
		}
		sys := info.UsrData_sSystemBuffer()
		if w == 0 {
			w, h = int(sys.IWidth), int(sys.IHeight)
		}
		yStride, cStride := int(sys.IStride[0]), int(sys.IStride[1])
		cw, ch := (w+1)/2, (h+1)/2
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
		fmt.Printf("Y=%s Cb=%s Cr=%s\n", hash(y), hash(cb), hash(cr))
		if dumpDir != "" {
			n := frameNo
			frameNo++
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
			return fmt.Errorf("DecodeFrameNoDelay: %d", r)
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
			return fmt.Errorf("FlushFrame: %d", r)
		}
		handle(pDst, &info)
	}
	fmt.Printf("// dims=%dx%d\n", w, h)
	return nil
}

func indexStartCode(b []byte) int {
	for i := 0; i+3 < len(b); i++ {
		if b[i] == 0 && b[i+1] == 0 && b[i+2] == 0 && b[i+3] == 1 {
			return i
		}
	}
	return -1
}

func hash(b []byte) string {
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}
