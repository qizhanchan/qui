package vp9

// Intra prediction with libvpx's edge rules (vp9_reconintra.c
// build_intra_predictors and vpx_dsp/intrapred.c).

const (
	needLeft       = 2
	needAbove      = 4
	needAboveRight = 8
)

var extendModes = [10]uint8{6, 4, 2, 8, 6, 6, 6, 2, 8, 6}

func avg2(a, b uint8) uint8    { return uint8((int(a) + int(b) + 1) >> 1) }
func avg3(a, b, c uint8) uint8 { return uint8((int(a) + 2*int(b) + int(c) + 2) >> 2) }

func (t *tileState) predictIntra(p, tx, mode, x, y int, haveTop, haveLeft, haveRight bool) {
	d := t.d
	cur := d.cur
	stride := cur.stride[p]
	pix := cur.planes[p]
	bs := 4 << tx
	fw := d.miCols * 8
	fh := d.miRows * 8
	if p > 0 {
		fw >>= 1
		fh >>= 1
	}
	var aboveBuf [64 + 1]uint8
	var left [32]uint8
	above := aboveBuf[1:]
	em := extendModes[mode]
	if em&needLeft != 0 {
		if haveLeft {
			n := min(bs, fh-y)
			for i := 0; i < n; i++ {
				left[i] = pix[(y+i)*stride+x-1]
			}
			for i := n; i < bs; i++ {
				left[i] = left[n-1]
			}
		} else {
			for i := 0; i < bs; i++ {
				left[i] = 129
			}
		}
	}
	if em&(needAbove|needAboveRight) != 0 {
		if haveTop {
			need := bs
			if em&needAboveRight != 0 && bs == 4 && haveRight {
				need = 8
			}
			n := min(need, fw-x)
			row := pix[(y-1)*stride+x:]
			copy(above[:n], row[:n])
			for i := n; i < 2*bs; i++ {
				above[i] = above[n-1]
			}
			if haveLeft {
				aboveBuf[0] = pix[(y-1)*stride+x-1]
			} else {
				aboveBuf[0] = 129
			}
		} else {
			for i := range aboveBuf[:2*bs+1] {
				aboveBuf[i] = 127
			}
		}
	}
	dst := pix[y*stride+x:]
	tl := aboveBuf[0]
	switch mode {
	case dcPred:
		sum, cnt := 0, 0
		if haveTop {
			for _, v := range above[:bs] {
				sum += int(v)
			}
			cnt += bs
		}
		if haveLeft {
			for _, v := range left[:bs] {
				sum += int(v)
			}
			cnt += bs
		}
		v := uint8(128)
		if cnt > 0 {
			v = uint8((sum + cnt/2) / cnt)
		}
		for r := 0; r < bs; r++ {
			row := dst[r*stride : r*stride+bs]
			for i := range row {
				row[i] = v
			}
		}
	case vPred:
		for r := 0; r < bs; r++ {
			copy(dst[r*stride:r*stride+bs], above[:bs])
		}
	case hPred:
		for r := 0; r < bs; r++ {
			row := dst[r*stride : r*stride+bs]
			v := left[r]
			for i := range row {
				row[i] = v
			}
		}
	case tmPred:
		for r := 0; r < bs; r++ {
			row := dst[r*stride : r*stride+bs]
			base := int(left[r]) - int(tl)
			for i := range row {
				row[i] = clipPixel(base + int(above[i]))
			}
		}
	case d45Pred:
		for r := 0; r < bs; r++ {
			for c := 0; c < bs; c++ {
				var v uint8
				if r+c+2 < 2*bs {
					v = avg3(above[r+c], above[r+c+1], above[r+c+2])
				} else {
					v = above[2*bs-1]
				}
				dst[r*stride+c] = v
			}
		}
	case d63Pred:
		for r := 0; r < bs; r++ {
			k := r >> 1
			for c := 0; c < bs; c++ {
				if r&1 == 0 {
					dst[r*stride+c] = avg2(above[k+c], above[k+c+1])
				} else {
					dst[r*stride+c] = avg3(above[k+c], above[k+c+1], above[k+c+2])
				}
			}
		}
	case d135Pred:
		// e: left reversed, top-left, above.
		var e [2*32 + 1]uint8
		for i := 0; i < bs; i++ {
			e[i] = left[bs-1-i]
		}
		e[bs] = tl
		copy(e[bs+1:], above[:bs])
		var border [2 * 32]uint8
		// border[k] = filtered edge value at diagonal offset k-(bs-1).
		for k := 0; k < 2*bs-1; k++ {
			i := k + 1 // index into e of the center
			border[k] = avg3(e[i-1], e[i], e[i+1])
		}
		for r := 0; r < bs; r++ {
			copy(dst[r*stride:r*stride+bs], border[bs-1-r:2*bs-1-r])
		}
	case d117Pred:
		for c := 0; c < bs; c++ {
			dst[c] = avg2(aboveAt(above, tl, c-1), above[c])
		}
		dst[stride] = avg3(left[0], tl, above[0])
		for c := 1; c < bs; c++ {
			dst[stride+c] = avg3(aboveAt(above, tl, c-2), aboveAt(above, tl, c-1), above[c])
		}
		dst[2*stride] = avg3(tl, left[0], left[1])
		for r := 3; r < bs; r++ {
			dst[r*stride] = avg3(left[r-3], left[r-2], left[r-1])
		}
		for r := 2; r < bs; r++ {
			for c := 1; c < bs; c++ {
				dst[r*stride+c] = dst[(r-2)*stride+c-1]
			}
		}
	case d153Pred:
		dst[0] = avg2(tl, left[0])
		for r := 1; r < bs; r++ {
			dst[r*stride] = avg2(left[r-1], left[r])
		}
		dst[1] = avg3(left[0], tl, above[0])
		dst[stride+1] = avg3(tl, left[0], left[1])
		for r := 2; r < bs; r++ {
			dst[r*stride+1] = avg3(left[r-2], left[r-1], left[r])
		}
		for c := 0; c < bs-2; c++ {
			dst[2+c] = avg3(aboveAt(above, tl, c-1), above[c], above[c+1])
		}
		for r := 1; r < bs; r++ {
			for c := 0; c < bs-2; c++ {
				dst[r*stride+2+c] = dst[(r-1)*stride+c]
			}
		}
	case d207Pred:
		for r := 0; r < bs-1; r++ {
			dst[r*stride] = avg2(left[r], left[r+1])
		}
		dst[(bs-1)*stride] = left[bs-1]
		for r := 0; r < bs-2; r++ {
			dst[r*stride+1] = avg3(left[r], left[r+1], left[r+2])
		}
		dst[(bs-2)*stride+1] = avg3(left[bs-2], left[bs-1], left[bs-1])
		dst[(bs-1)*stride+1] = left[bs-1]
		for c := 0; c < bs-2; c++ {
			dst[(bs-1)*stride+2+c] = left[bs-1]
		}
		for r := bs - 2; r >= 0; r-- {
			for c := 0; c < bs-2; c++ {
				dst[r*stride+2+c] = dst[(r+1)*stride+c]
			}
		}
	}
}

func aboveAt(above []uint8, tl uint8, i int) uint8 {
	if i < 0 {
		return tl
	}
	return above[i]
}
