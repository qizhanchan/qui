// Command gen-idr generates a small Baseline CAVLC IDR with hi264's encoder
// and prints the Annex-B hex plus per-plane SHA-256 hashes from hi264's own
// decoder. The hashes are the pixel-exact oracle for our decoder's
// integration test.
//
// Usage: go run ./cmd/gen-idr [grid] [qp]
//
//	grid chars x,y,z,w map to a fixed palette; rows separated by '\n' or ';'.
package main

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/Eyevinn/hi264/pkg/decoder"
	"github.com/Eyevinn/hi264/pkg/encode"
	"github.com/Eyevinn/hi264/pkg/yuv"
)

var palette = yuv.ColorMap{
	'x': {Y: 100, Cb: 90, Cr: 200},
	'y': {Y: 180, Cb: 200, Cr: 60},
	'z': {Y: 60, Cb: 128, Cr: 128},
	'w': {Y: 220, Cb: 160, Cr: 90},
}

func main() {
	gridStr := "xy"
	if len(os.Args) > 1 {
		gridStr = os.Args[1]
	}
	qp := 26
	if len(os.Args) > 2 {
		qp, _ = strconv.Atoi(os.Args[2])
	}
	deblock := len(os.Args) > 3 && os.Args[3] == "deblock"
	grid, err := yuv.ParseGrid(strings.ReplaceAll(gridStr, ";", "\n"))
	if err != nil {
		panic(err)
	}
	disableDeblock := 1
	if deblock {
		disableDeblock = 0
	}
	enc := &encode.FrameEncoder{Grid: grid, Colors: palette, QP: qp, DisableDeblock: disableDeblock}
	bs, err := enc.Encode()
	if err != nil {
		panic(err)
	}

	dec := decoder.New()
	dec.SkipDeblock = !deblock
	f, err := dec.DecodeAnnexB(bs)
	if err != nil {
		panic(err)
	}
	w, h := f.Width, f.Height
	cw, ch := (w+1)/2, (h+1)/2
	fmt.Printf("// stream=%q\n", hex.EncodeToString(bs))
	fmt.Printf("// dims=%dx%d\n", w, h)
	fmt.Printf("// Y =%x\n", sha256.Sum256(f.Y[:w*h]))
	fmt.Printf("// Cb=%x\n", sha256.Sum256(f.Cb[:cw*ch]))
	fmt.Printf("// Cr=%x\n", sha256.Sum256(f.Cr[:cw*ch]))
}
