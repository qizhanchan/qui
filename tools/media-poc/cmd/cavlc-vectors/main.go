// Command cavlc-vectors generates CAVLC residual test vectors using
// hi264's encoder, as a round-trip oracle for our decoder. Output is a Go
// literal to paste into media/internal/h264/cavlc_test.go.
//
// Usage: go run ./cmd/cavlc-vectors
package main

import (
	"encoding/hex"
	"fmt"

	"github.com/Eyevinn/hi264/pkg/encode"
)

type vec struct {
	name        string
	nC          int
	maxNumCoeff int
	coeffs      []int32
}

func main() {
	vecs := []vec{
		{"dc_only", 0, 16, []int32{5, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, -3}},
		{"two_ac", 0, 16, []int32{1, 0, 0, 0, 0, 0, 0, 2, 0, 0, 0, 0, 0, 0, 0, 0}},
		{"chroma_dc", -1, 4, []int32{7, -2, 0, 1}},
		{"ac15", 3, 15, []int32{0, 2, 0, 0, 0, 0, -1, 0, 0, 0, 0, 4, 0, 0, 0}},
		{"full16", 8, 16, []int32{1, -1, 2, -2, 3, -3, 1, 1, 1, 0, 0, 0, 4, -4, 5, -5}},
		{"big_levels", 2, 16, []int32{100, 0, 0, 0, 0, 0, 0, -200, 0, 0, 0, 0, 0, 0, 0, 1}},
	}
	for _, v := range vecs {
		w := encode.NewBitWriter()
		encode.EncodeResidualBlock(w, v.coeffs, v.nC, v.maxNumCoeff)
		fmt.Printf("{%q, %d, %d, %#v, %q},\n",
			v.name, v.nC, v.maxNumCoeff, v.coeffs, hex.EncodeToString(w.Bytes()))
	}
}
