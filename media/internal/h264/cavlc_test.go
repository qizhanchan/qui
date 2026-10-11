package h264

import "testing"

// TestDecodeResidualBlockRoundTrip decodes CAVLC bitstreams produced by
// hi264's encoder (tools/media-poc/cmd/cavlc-vectors) and asserts we
// recover the exact coefficients. This validates the coeff_token,
// level, total_zeros and run_before paths against a reference encoder.
func TestDecodeResidualBlockRoundTrip(t *testing.T) {
	cases := []struct {
		name        string
		nC          int
		maxNumCoeff int
		coeffs      []int32
		hex         string
	}{
		{"dc_only", 0, 16, []int32{5, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, -3}, "0710800020"},
		{"two_ac", 0, 16, []int32{1, 0, 0, 0, 0, 0, 0, 2, 0, 0, 0, 0, 0, 0, 0, 0}, "07c900"},
		{"chroma_dc", -1, 4, []int32{7, -2, 0, 1}, "064080"},
		{"ac15", 3, 15, []int32{0, 2, 0, 0, 0, 0, -1, 0, 0, 0, 0, 4, 0, 0, 0}, "0e1b86c8"},
		{"full16", 8, 16, []int32{1, -1, 2, -2, 3, -3, 1, 1, 1, 0, 0, 0, 4, -4, 5, -5}, "c06476922a7d63f8"},
		{"big_levels", 2, 16, []int32{100, 0, 0, 0, 0, 0, 0, -200, 0, 0, 0, 0, 0, 0, 0, 1}, "2800022de000211400c0"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r := newBitReader(mustHex(t, c.hex))
			got, totalCoeff, err := decodeResidualBlock(r, c.nC, c.maxNumCoeff)
			if err != nil {
				t.Fatalf("decodeResidualBlock: %v", err)
			}
			for i := range c.coeffs {
				if got[i] != c.coeffs[i] {
					t.Errorf("coeff[%d] = %d, want %d\n got  %v\n want %v",
						i, got[i], c.coeffs[i], got, c.coeffs)
					break
				}
			}
			want := 0
			for _, v := range c.coeffs {
				if v != 0 {
					want++
				}
			}
			if totalCoeff != want {
				t.Errorf("totalCoeff = %d, want %d", totalCoeff, want)
			}
		})
	}
}

func TestDecodeResidualZeroBlock(t *testing.T) {
	// A coeff_token of "1" means totalCoeff == 0: no further data.
	r := newBitReader(packBits("1"))
	coeffs, totalCoeff, err := decodeResidualBlock(r, 0, 16)
	if err != nil {
		t.Fatalf("decodeResidualBlock: %v", err)
	}
	if totalCoeff != 0 {
		t.Errorf("totalCoeff = %d, want 0", totalCoeff)
	}
	for i, v := range coeffs {
		if v != 0 {
			t.Errorf("coeff[%d] = %d, want 0", i, v)
		}
	}
}
