package media

// colorMatrix returns the YCbCr→RGB conversion for a frame, as the
// compositor's DrawYUV420 takes it: rgb = m · ((yuv − off) ∘ scale), m
// column-major. matrix is the stream's matrix_coefficients (1 = BT.709,
// 5/6 = BT.601); unknown picks by height.
func colorMatrix(matrix int, fullRange bool, height int) (m [9]float32, off, scale [3]float32) {
	kr, kb := float32(0.299), float32(0.114) // BT.601
	switch matrix {
	case 1: // BT.709
		kr, kb = 0.2126, 0.0722
	case 9, 10: // BT.2020
		kr, kb = 0.2627, 0.0593
	case 5, 6:
	default:
		if height >= 720 {
			kr, kb = 0.2126, 0.0722
		}
	}
	kg := 1 - kr - kb
	// R = Y + 2(1-kr) Cr; B = Y + 2(1-kb) Cb; G = (Y - kr R - kb B)/kg.
	rv := 2 * (1 - kr)
	bu := 2 * (1 - kb)
	gu := -bu * kb / kg
	gv := -rv * kr / kg
	// Columns: Y, Cb, Cr.
	m = [9]float32{1, 1, 1, 0, gu, bu, rv, gv, 0}
	if fullRange {
		off = [3]float32{0, 128.0 / 255, 128.0 / 255}
		scale = [3]float32{1, 1, 1}
	} else {
		off = [3]float32{16.0 / 255, 128.0 / 255, 128.0 / 255}
		scale = [3]float32{255.0 / 219, 255.0 / 224, 255.0 / 224}
	}
	return
}
