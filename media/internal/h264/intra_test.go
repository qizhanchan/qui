package h264

import "testing"

func fill(v byte, n int) []byte {
	s := make([]byte, n)
	for i := range s {
		s[i] = v
	}
	return s
}

func TestPredict16x16DC(t *testing.T) {
	pred := predict16x16(I16x16DC, fill(100, 16), fill(100, 16), 100)
	for y := 0; y < 16; y++ {
		for x := 0; x < 16; x++ {
			if pred[y][x] != 100 {
				t.Fatalf("DC pred[%d][%d] = %d, want 100", y, x, pred[y][x])
			}
		}
	}
}

func TestPredict16x16Vertical(t *testing.T) {
	top := make([]byte, 16)
	for i := range top {
		top[i] = byte(i)
	}
	pred := predict16x16(I16x16Vertical, top, nil, 0)
	for y := 0; y < 16; y++ {
		for x := 0; x < 16; x++ {
			if pred[y][x] != byte(x) {
				t.Fatalf("V pred[%d][%d] = %d, want %d", y, x, pred[y][x], x)
			}
		}
	}
}

func TestPredict16x16DCNoNeighbors(t *testing.T) {
	pred := predict16x16(I16x16DC, nil, nil, 0)
	if pred[0][0] != 128 {
		t.Fatalf("DC no-neighbor = %d, want 128", pred[0][0])
	}
}

func TestPredict4x4DC(t *testing.T) {
	var ref [13]byte
	for i := range ref {
		ref[i] = 100
	}
	pred := predict4x4(I4x4DC, ref, true, true)
	if pred[0][0] != 100 || pred[3][3] != 100 {
		t.Fatalf("4x4 DC = %d/%d, want 100", pred[0][0], pred[3][3])
	}
	pred = predict4x4(I4x4DC, ref, false, false)
	if pred[0][0] != 128 {
		t.Fatalf("4x4 DC no-neighbor = %d, want 128", pred[0][0])
	}
}

func TestPredict4x4Vertical(t *testing.T) {
	var ref [13]byte
	// T0..T3 at ref[5..8].
	ref[5], ref[6], ref[7], ref[8] = 10, 20, 30, 40
	pred := predict4x4(I4x4Vertical, ref, false, true)
	row := []byte{10, 20, 30, 40}
	for y := 0; y < 4; y++ {
		for x := 0; x < 4; x++ {
			if pred[y][x] != row[x] {
				t.Fatalf("V4 pred[%d][%d] = %d, want %d", y, x, pred[y][x], row[x])
			}
		}
	}
}

func TestPredictChromaDC(t *testing.T) {
	pred := predictChroma(ChromaDC, fill(120, 8), fill(120, 8), 120)
	for y := 0; y < 8; y++ {
		for x := 0; x < 8; x++ {
			if pred[y][x] != 120 {
				t.Fatalf("chroma DC[%d][%d] = %d, want 120", y, x, pred[y][x])
			}
		}
	}
}
