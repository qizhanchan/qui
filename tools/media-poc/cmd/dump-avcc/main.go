// Command dump-avcc prints the SPS/PPS NAL units and NAL length prefix size
// from an MP4's avcC box, as hex. Useful when developing/testing the H.264
// decoder without shipping binary fixtures.
//
// Usage: go run ./cmd/dump-avcc input.mp4
package main

import (
	"fmt"
	"os"

	"github.com/qizhanchan/qui/tools/media-poc/internal/mp4util"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: dump-avcc <input.mp4>")
		os.Exit(2)
	}
	f, err := os.Open(os.Args[1])
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	defer f.Close()
	sps, pps, err := mp4util.AVCCParameterSets(f)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	for i, s := range sps {
		fmt.Printf("SPS[%d] (%d bytes) = % x\n", i, len(s), s)
	}
	for i, p := range pps {
		fmt.Printf("PPS[%d] (%d bytes) = % x\n", i, len(p), p)
	}
}
