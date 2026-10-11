package av1

import (
	"math"
	"math/rand"
	"testing"
)

// TestFastFwdTx compares the fast forward transforms with the exact
// matrix inverses on random residuals.
func TestFastFwdTx(t *testing.T) {
	rng := rand.New(rand.NewSource(5))
	res := make([]int32, 64*64)
	tmp := make([]float64, 64*64)
	a := make([]float64, 32*32)
	b := make([]float64, 32*32)
	for tx := 0; tx < txSizesAll; tx++ {
		for _, typ := range []int{dctDct, adstDct, dctAdst, flipadstFlipadst, idtx, vDct, hDct} {
			if txSqrUp[tx] > tx32x32 && typ != dctDct || txSqrUp[tx] == tx32x32 && typ != dctDct && typ != idtx {
				continue
			}
			w, h := txW[tx], txH[tx]
			worst := 0.0
			for it := 0; it < 4; it++ {
				for i := range res[:w*h] {
					res[i] = int32(rng.Intn(511) - 255)
				}
				fwdTx(res, tx, typ, tmp, a)
				fwdTxMat(res, tx, typ, tmp, b)
				for i := range a[:min(w, 32)*min(h, 32)] {
					d := math.Abs(a[i] - b[i])
					worst = max(worst, d)
				}
			}
			// The integer transforms are cosine transforms up to their 12-bit
			// constants: differences stay far below a quantiser step (>= 4).
			if worst > 1 {
				t.Errorf("tx %d type %d: max difference %.3f", tx, typ, worst)
			}
		}
	}
}
