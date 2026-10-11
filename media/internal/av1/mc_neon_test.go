package av1

import (
	"math/rand"
	"testing"
)

// TestMcNEON checks the NEON subpel kernels against mcKernel for every
// filter, phase and block width.
func TestMcNEON(t *testing.T) {
	if !haveNEON {
		t.Skip("no NEON")
	}
	rng := rand.New(rand.NewSource(1))
	const stride = 160
	src := make([]uint8, stride*(128+8))
	for i := range src {
		src[i] = uint8(rng.Intn(256))
	}
	tl := &tile{mc: new(mcScratch)}
	want := make([]int32, 128*128)
	got := make([]int32, 128*128)
	for _, sz := range [][2]int{{4, 4}, {8, 4}, {4, 16}, {8, 8}, {16, 8}, {32, 32}, {64, 16}, {128, 128}} {
		w, h := sz[0], sz[1]
		for fi := 0; fi < 6; fi++ {
			for fx := 0; fx < 16; fx++ {
				for fy := 0; fy < 16; fy += 5 {
					for _, r1 := range []uint{11, 7} {
						mcKernel(src, stride, w, h, fx, fy, &subpelFilters[fi][fx], &subpelFilters[fi][fy], 3, r1, tl.mc.inter[:], want)
						tl.mcNEON(src, stride, w, h, fx, fy, &subpelHalf[fi][fx], &subpelHalf[fi][fy], r1, got)
						for i := 0; i < w*h; i++ {
							if got[i] != want[i] {
								t.Fatalf("%dx%d filter %d phase %d/%d r1 %d: sample %d got %d want %d", w, h, fi, fx, fy, r1, i, got[i], want[i])
							}
						}
					}
				}
			}
		}
	}
}
