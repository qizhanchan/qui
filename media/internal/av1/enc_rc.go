package av1

import (
	"math"
	"sort"
)

// Frame-level rate control: the H.264 / HEVC encoders' one-pass model
// (after x264) on AV1's quantiser scale. The model works with qscale, the
// AC quantiser step in sample units (acQ / 8), which grows like an H.264
// step with QP:
//
//   - CQP: inter frames at EncoderConfig.QIndex, key frames at a step
//     about 1.4x finer.
//   - CRF: qscale = blurredComplexity^(1-qcomp) / rateFactorConstant;
//     CRF n codes typical content at qindex 4n (the 0..63 scale of other
//     AV1 encoders).
//   - ABR: the same model with the rate factor re-fit to the bits spent,
//     plus an overflow correction.
//   - VBV: a leaky bucket predicts each frame's size from a per-type
//     bits ~ coef * complexity / qscale predictor and raises the
//     quantiser so the buffer cannot underflow; a frame that still
//     overflows is coded again more coarsely (Encode).

const (
	rcKeyRatio = 1.41 // inter / key quantiser step ratio
	rcQComp    = 0.6
	rcMaxStep  = 1.6 // largest qscale change between frames of a type (ABR)
)

// qscaleOf is the quantiser step (in samples) of a qindex.
func qscaleOf(qindex int) float64 { return float64(acQ(8, qindex)) / 8 }

// qindexOf is the smallest qindex whose step reaches qs.
func qindexOf(qs float64) int {
	return clip3(1, 255, sort.Search(256, func(i int) bool { return qscaleOf(i) >= qs }))
}

type rcPredictor struct{ coef, count float64 }

func (p *rcPredictor) predict(cplx, qscale float64) float64 { return p.coef * cplx / qscale }

func (p *rcPredictor) update(cplx, qscale, bits float64) {
	if cplx <= 0 {
		return
	}
	p.count = p.count*0.5 + 1
	p.coef += (bits*qscale/cplx - p.coef) / p.count
}

type rateControl struct {
	mode int // 0 CQP, 1 CRF, 2 ABR
	fps  float64

	cplxSum, cplxCount float64
	rfConst            float64
	wantedWindow       float64
	cplxrSum           float64
	totalBits          float64
	wantedBits         float64
	lastQS             [2]float64
	haveLast           [2]bool

	vbvRate, vbvSize float64
	vbvFill          float64
	pred             [2]rcPredictor

	cplx, keyCplx float64
	prevY         []byte // luma of the previously coded source
}

func (r *rateControl) init(e *Encoder) {
	c := &e.cfg
	r.fps = float64(c.FPSNum) / float64(c.FPSDen)
	blocks := float64(((c.Width + 15) / 16) * ((c.Height + 15) / 16))
	switch {
	case c.Bitrate > 0:
		r.mode = 2
	case c.CRF > 0:
		r.mode = 1
	}
	qs := qscaleOf(c.QIndex)
	if r.mode == 1 {
		qs = qscaleOf(clip3(1, 255, int(math.Round(c.CRF*4))))
	}
	r.rfConst = math.Pow(blocks*250, 1-rcQComp) / qs
	if c.VBVMaxRate > 0 && c.VBVBuffer > 0 {
		r.vbvRate, r.vbvSize = float64(c.VBVMaxRate), float64(c.VBVBuffer)
		r.vbvFill = r.vbvSize * 0.9
	}
	for i := range r.pred {
		r.pred[i] = rcPredictor{coef: 1.5, count: 1}
	}
	if r.mode == 2 {
		r.wantedWindow = float64(c.Bitrate) / r.fps
		r.cplxrSum = r.wantedWindow / r.rfConst
	}
}

func rcType(key bool) int {
	if key {
		return 0
	}
	return 1
}

// frameQIndex returns the qindex of the next frame, whose source is
// loaded.
func (r *rateControl) frameQIndex(key bool, e *Encoder) int {
	c := &e.cfg
	r.cplx = r.complexity(e, key)
	if key {
		r.keyCplx = r.cplx
	}
	typ := rcType(key)
	ratio := 1.0
	if key {
		ratio = 1 / rcKeyRatio
	}
	var qs float64
	switch {
	case r.mode == 0:
		qs = qscaleOf(c.QIndex) * ratio
	case key && r.haveLast[1]:
		// As in x264, key frames follow the inter frames' quantiser.
		qs = r.lastQS[1] * ratio
	default:
		r.cplxSum = r.cplxSum*0.5 + r.cplx
		r.cplxCount = r.cplxCount*0.5 + 1
		blurred := r.cplxSum / r.cplxCount
		rf := r.rfConst
		if r.mode == 2 {
			rf = r.wantedWindow / r.cplxrSum
		}
		qs = math.Pow(blurred, 1-rcQComp) / rf
		if r.mode == 2 && r.wantedBits > 0 {
			over := 1 + (r.totalBits-r.wantedBits)/float64(c.Bitrate)
			qs *= math.Min(math.Max(over, 0.5), 2)
		}
		qs *= ratio
		if r.mode == 2 && r.haveLast[typ] {
			qs = math.Min(math.Max(qs, r.lastQS[typ]/rcMaxStep), r.lastQS[typ]*rcMaxStep)
		}
	}
	qs = r.vbvClamp(key, qs, e)
	return qindexOf(qs)
}

// vbvClamp raises qs until the predicted frame size leaves the buffer at
// least 10% full and, ahead of the next key frame, room for that frame.
func (r *rateControl) vbvClamp(key bool, qs float64, e *Encoder) float64 {
	if r.vbvSize == 0 {
		return qs
	}
	p := &r.pred[rcType(key)]
	reserve := 0.1 * r.vbvSize
	if !key && r.pred[0].count > 1 {
		until := float64(int64(e.cfg.KeyInt) - (e.frameCount - e.keyFrame))
		need := r.pred[0].predict(r.keyCplx, qs/rcKeyRatio) - until*r.vbvRate/r.fps
		reserve = math.Max(reserve, math.Min(need, 0.8*r.vbvSize))
	}
	for qs < qscaleOf(255) && r.vbvFill-p.predict(r.cplx, qs) < reserve {
		qs *= 1.12
	}
	return qs
}

func (r *rateControl) vbvOverflow(bits int) bool {
	return r.vbvSize > 0 && r.vbvFill-float64(bits) < 0
}

// update accounts a coded frame.
func (r *rateControl) update(key bool, qindex, bits int, e *Encoder) {
	t := rcType(key)
	b := float64(bits)
	qs := qscaleOf(qindex)
	r.pred[t].update(r.cplx, qs, b)
	r.lastQS[t], r.haveLast[t] = qs, true
	r.totalBits += b
	if e.cfg.Bitrate > 0 {
		r.wantedBits += float64(e.cfg.Bitrate) / r.fps
		qb := qs
		if key {
			qb *= rcKeyRatio
		}
		blurred := r.cplxSum / math.Max(r.cplxCount, 1)
		r.cplxrSum += b * qb / math.Pow(math.Max(blurred, 1), 1-rcQComp)
		r.wantedWindow += float64(e.cfg.Bitrate) / r.fps
		r.cplxrSum *= 0.995
		r.wantedWindow *= 0.995
	}
	if r.vbvSize > 0 {
		r.vbvFill = math.Min(r.vbvFill-b+r.vbvRate/r.fps, r.vbvSize)
	}
	// Keep the luma for the next frame's complexity estimate.
	w, h := e.cfg.Width, e.cfg.Height
	if len(r.prevY) != w*h {
		r.prevY = make([]byte, w*h)
	}
	for y := 0; y < h; y++ {
		copy(r.prevY[y*w:y*w+w], e.src[0][y*e.srcStride[0]:])
	}
}

// complexity estimates the loaded source's complexity: per 16x16 block,
// the smaller of the intra mean absolute deviation and the zero-motion
// SAD against the previous source (rows subsampled by two).
func (r *rateControl) complexity(e *Encoder, key bool) float64 {
	w, h := e.cfg.Width, e.cfg.Height
	src, ys := e.src[0], e.srcStride[0]
	prev := r.prevY
	if key || len(prev) != w*h {
		prev = nil
	}
	var total float64
	for by := 0; by+16 <= h; by += 16 {
		for bx := 0; bx+16 <= w; bx += 16 {
			o := by*ys + bx
			sum := 0
			for y := 0; y < 16; y += 2 {
				for _, v := range src[o+y*ys : o+y*ys+16] {
					sum += int(v)
				}
			}
			mean := (sum + 64) >> 7
			mad := 0
			for y := 0; y < 16; y += 2 {
				for _, v := range src[o+y*ys : o+y*ys+16] {
					mad += abs(int(v) - mean)
				}
			}
			c := mad
			if prev != nil {
				sad := 0
				for y := 0; y < 16; y += 2 {
					a := src[o+y*ys : o+y*ys+16]
					b := prev[(by+y)*w+bx : (by+y)*w+bx+16]
					for i := range a {
						sad += abs(int(a[i]) - int(b[i]))
					}
				}
				c = min(c, sad)
			}
			total += float64(2 * c)
		}
	}
	return math.Max(total, float64((w/16)*(h/16)))
}
