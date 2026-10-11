package h264

import "testing"

func TestDequant4x4DefaultScaling(t *testing.T) {
	var level [16]int32
	for i := range level {
		level[i] = 1
	}
	got := dequant4x4(level, 0) // qpPer=0: (Level*LevelScale*16 + 8) >> 4
	// i=0 -> class 0 -> 10; i=1 -> class 1 -> 13; i=5 -> class 2 -> 16.
	wants := map[int]int32{0: 10, 1: 13, 5: 16}
	for i, w := range wants {
		if got[i] != w {
			t.Errorf("dequant(1,qp0)[%d] = %d, want %d", i, got[i], w)
		}
	}
}

func TestIDCT4x4Zero(t *testing.T) {
	var in [16]int32
	if got := idct4x4(in); got != [16]int32{} {
		t.Errorf("idct(0) = %v, want zeros", got)
	}
}

func TestIDCT4x4DCOnly(t *testing.T) {
	var in [16]int32
	in[0] = 64 // (k+32)>>6 = 1 everywhere
	got := idct4x4(in)
	for i := 0; i < 16; i++ {
		if got[i] != 1 {
			t.Fatalf("idct(DC=64)[%d] = %d, want 1", i, got[i])
		}
	}
}

func TestHadamard4x4DCOnly(t *testing.T) {
	var in [16]int32
	in[0] = 7
	got := hadamard4x4(in)
	for i := 0; i < 16; i++ {
		if got[i] != 7 {
			t.Fatalf("hadamard4x4(DC=7)[%d] = %d, want 7", i, got[i])
		}
	}
}

func TestHadamard2x2DCOnly(t *testing.T) {
	in := [4]int32{5, 0, 0, 0}
	got := hadamard2x2(in)
	for i := 0; i < 4; i++ {
		if got[i] != 5 {
			t.Fatalf("hadamard2x2(DC=5)[%d] = %d, want 5", i, got[i])
		}
	}
}

func TestChromaQPMapping(t *testing.T) {
	if chromaQP(20) != 20 || chromaQP(51) != 39 {
		t.Errorf("chromaQP: 20->%d (want 20), 51->%d (want 39)", chromaQP(20), chromaQP(51))
	}
}
