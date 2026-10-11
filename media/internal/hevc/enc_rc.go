package hevc

import "math"

// Picture-level rate control, the H.264 encoder's one-pass model (after
// x264) on HEVC's identical QP scale:
//
//   - CQP: P pictures at EncoderConfig.QP, I pictures 3 lower, B 2 higher.
//   - CRF: qscale = blurredComplexity^(1-qcomp) / rateFactorConstant.
//   - ABR: the same model with the rate factor re-fit to the bits spent,
//     plus an overflow correction.
//   - VBV: a leaky bucket predicts each picture's size from a per-type
//     bits ~ coef * complexity / qscale predictor and raises QP so the
//     buffer cannot underflow; a picture that still overflows is coded
//     again at a higher QP (codePicture).
//
// Complexity is a cheap pre-encode estimate: per 16x16 block, the smaller
// of the zero-motion SAD against the previous source and the intra mean
// absolute deviation.

const (
	rcIPOffset = 3
	rcPBOffset = 2
	rcQComp    = 0.6
	rcQPStep   = 4
)

func qp2qscale(qp float64) float64 { return 0.85 * math.Pow(2, (qp-12)/6) }
func qscale2qp(q float64) float64  { return 12 + 6*math.Log2(q/0.85) }

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
	lastQP             [3]float64
	haveLast           [3]bool

	vbvRate, vbvSize float64
	vbvFill          float64
	pred             [3]rcPredictor

	cplx, iCplx float64
	prevY       []byte // luma of the previously coded source
}

func (r *rateControl) init(e *Encoder) {
	c := &e.cfg
	r.fps = float64(c.FPSNum) / float64(c.FPSDen)
	blocks := float64(((e.seq.width + 15) / 16) * ((e.seq.height + 15) / 16))
	switch {
	case c.Bitrate > 0:
		r.mode = 2
	case c.CRF > 0:
		r.mode = 1
	}
	// A typical complexity of 250 per 16x16 block maps CRF n to QP n for
	// P pictures (the H.264 encoder's calibration).
	crf := c.CRF
	if r.mode == 2 {
		crf = float64(c.QP)
	}
	r.rfConst = math.Pow(blocks*250, 1-rcQComp) / qp2qscale(crf)
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

func rcTypeOffset(typ int) int {
	switch typ {
	case sliceI:
		return -rcIPOffset
	case sliceB:
		return rcPBOffset
	}
	return 0
}

func rcTypeIdx(typ int) int {
	switch typ {
	case sliceI:
		return 0
	case sliceB:
		return 2
	}
	return 1
}

// pictureQP returns the QP of the next picture, of type typ, whose source
// is loaded.
func (r *rateControl) pictureQP(typ int, e *Encoder) int {
	c := &e.cfg
	r.cplx = r.complexity(e, typ)
	if typ == sliceI {
		r.iCplx = r.cplx
	}
	if r.mode == 0 && r.vbvSize == 0 {
		return clip3(0, 51, c.QP+rcTypeOffset(typ))
	}
	var qp float64
	switch {
	case r.mode == 0:
		qp = float64(c.QP + rcTypeOffset(typ))
	case typ != sliceP && r.haveLast[1]:
		// As in x264, I and B pictures follow the P pictures' QP with
		// their offsets; only P pictures run the model.
		qp = r.lastQP[1] + float64(rcTypeOffset(typ))
	default:
		r.cplxSum = r.cplxSum*0.5 + r.cplx
		r.cplxCount = r.cplxCount*0.5 + 1
		blurred := r.cplxSum / r.cplxCount
		rf := r.rfConst
		if r.mode == 2 {
			rf = r.wantedWindow / r.cplxrSum
		}
		q := math.Pow(blurred, 1-rcQComp) / rf
		if r.mode == 2 && r.wantedBits > 0 {
			buf := float64(c.Bitrate)
			over := 1 + (r.totalBits-r.wantedBits)/buf
			q *= math.Min(math.Max(over, 0.5), 2)
		}
		qp = qscale2qp(q) + float64(rcTypeOffset(typ))
		if r.mode == 2 {
			t := rcTypeIdx(typ)
			if r.haveLast[t] {
				qp = math.Min(math.Max(qp, r.lastQP[t]-rcQPStep), r.lastQP[t]+rcQPStep)
			}
		}
	}
	qp = r.vbvClamp(typ, qp, e)
	return clip3(0, 51, int(math.Round(qp)))
}

// vbvClamp raises qp until the predicted picture size leaves the buffer at
// least 10% full and, ahead of the next IDR, room for that picture.
func (r *rateControl) vbvClamp(typ int, qp float64, e *Encoder) float64 {
	if r.vbvSize == 0 {
		return qp
	}
	p := &r.pred[rcTypeIdx(typ)]
	reserve := 0.1 * r.vbvSize
	if typ != sliceI && r.pred[0].count > 1 {
		until := float64(int64(e.cfg.KeyInt) - (e.frameCount - e.keyPTS))
		iq := qp2qscale(math.Min(qp-rcIPOffset, 51))
		need := r.pred[0].predict(r.iCplx, iq) - until*r.vbvRate/r.fps
		reserve = math.Max(reserve, math.Min(need, 0.8*r.vbvSize))
	}
	for qp < 51 && r.vbvFill-p.predict(r.cplx, qp2qscale(qp)) < reserve {
		qp++
	}
	return qp
}

func (r *rateControl) vbvOverflow(bits int) bool {
	return r.vbvSize > 0 && r.vbvFill-float64(bits) < 0
}

// update accounts a coded picture.
func (r *rateControl) update(typ, qp, bits int, e *Encoder) {
	t := rcTypeIdx(typ)
	b := float64(bits)
	r.pred[t].update(r.cplx, qp2qscale(float64(qp)), b)
	r.lastQP[t], r.haveLast[t] = float64(qp), true
	r.totalBits += b
	if e.cfg.Bitrate > 0 {
		r.wantedBits += float64(e.cfg.Bitrate) / r.fps
		qb := qp2qscale(float64(qp - rcTypeOffset(typ)))
		blurred := r.cplxSum / math.Max(r.cplxCount, 1)
		r.cplxrSum += b * qb / math.Pow(math.Max(blurred, 1), 1-rcQComp)
		r.wantedWindow += float64(e.cfg.Bitrate) / r.fps
		r.cplxrSum *= 0.995
		r.wantedWindow *= 0.995
	}
	if r.vbvSize > 0 {
		r.vbvFill = math.Min(r.vbvFill-b+r.vbvRate/r.fps, r.vbvSize)
	}
	// Keep the luma for the next picture's complexity estimate.
	r.prevY = append(r.prevY[:0], e.srcY...)
}

// complexity estimates the loaded source's complexity (rows subsampled by
// two).
func (r *rateControl) complexity(e *Encoder, typ int) float64 {
	w, h, ys := e.seq.width, e.seq.height, e.ys
	src := e.srcY
	prev := r.prevY
	if typ == sliceI || len(prev) != len(src) {
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
					mad += iabs(int(v) - mean)
				}
			}
			c := mad
			if prev != nil {
				sad := 0
				for y := 0; y < 16; y += 2 {
					a, b := src[o+y*ys:o+y*ys+16], prev[o+y*ys:o+y*ys+16]
					for i := range a {
						sad += iabs(int(a[i]) - int(b[i]))
					}
				}
				c = min(c, sad)
			}
			total += float64(2 * c)
		}
	}
	return math.Max(total, float64((w/16)*(h/16)))
}
