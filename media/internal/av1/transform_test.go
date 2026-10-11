package av1

import (
	"bufio"
	"os"
	"strconv"
	"strings"
	"testing"
)

// TestTransform1D (opt-in: AV1_TX_VECTORS) checks the 1-D inverse
// transforms against vectors from a reference implementation ("name
// inputs : outputs" per line).
func TestTransform1D(t *testing.T) {
	path := os.Getenv("AV1_TX_VECTORS")
	if path == "" {
		t.Skip("set AV1_TX_VECTORS")
	}
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	fns := map[string]func([]int32){
		"idct4": func(x []int32) { idct(x, 2) }, "idct8": func(x []int32) { idct(x, 3) },
		"idct16": func(x []int32) { idct(x, 4) }, "idct32": func(x []int32) { idct(x, 5) },
		"idct64": func(x []int32) { idct(x, 6) }, "iadst4": iadst4, "iadst8": iadst8, "iadst16": iadst16,
	}
	bad := map[string]int{}
	sc := bufio.NewScanner(f)
	sc.Buffer(nil, 1<<20)
	for sc.Scan() {
		parts := strings.Split(sc.Text(), " : ")
		in := strings.Fields(parts[0])
		name := in[0]
		var x, want []int32
		for _, s := range in[1:] {
			v, _ := strconv.Atoi(s)
			x = append(x, int32(v))
		}
		for _, s := range strings.Fields(parts[1]) {
			v, _ := strconv.Atoi(s)
			want = append(want, int32(v))
		}
		big := false
		for _, v := range x {
			big = big || v > 1<<15 || v < -(1<<15)
		}
		if big {
			continue // beyond conformant ranges (the reference overflows)
		}
		orig := append([]int32(nil), x...)
		fns[name](x)
		for i := range x {
			if x[i] != want[i] {
				if bad[name] < 2 {
					t.Errorf("%s(%v)\n got  %v\n want %v", name, orig, x, want)
				}
				bad[name]++
				break
			}
		}
	}
	for k, v := range bad {
		t.Errorf("%s: %d mismatches", k, v)
	}
}
