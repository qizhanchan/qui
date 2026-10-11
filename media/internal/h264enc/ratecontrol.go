package h264enc

import "math"

// Picture-level rate control, after x264's one-pass model:
//
//   - CQP: P pictures at Config.QP, I pictures ipOffset lower, B pictures
//     pbOffset higher.
//   - CRF: qscale = blurredComplexity^(1-qcomp) / rateFactorConstant, so
//     quality is constant and bits follow content complexity.
//   - ABR: the same complexity model with a rate factor that is re-fit to
//     the bits actually spent, plus an overflow correction.
//   - VBV: a leaky-bucket model predicts each picture's size from a
//     per-type bits≈coef·complexity/qscale predictor and raises QP so the
//     buffer cannot underflow; a picture that still overflows is re-encoded
//     at a higher QP (codePicture).
//
// Complexity is a cheap pre-encode estimate (estimateComplexity): per
// macroblock, the smaller of the zero-motion SAD against the previous source
// and the intra mean absolute deviation, summed over the picture.

const (
	ipOffset = 3 // I pictures: QP - 3 (x264 ipratio 1.4)
	pbOffset = 2 // B pictures: QP + 2 (x264 pbratio 1.3)
	qcomp    = 0.6
	qpStep   = 4 // max QP change between consecutive pictures of a type (ABR)
)

func qp2qscale(qp float64) float64 { return 0.85 * math.Pow(2, (qp-12)/6) }
func qscale2qp(q float64) float64  { return 12 + 6*math.Log2(q/0.85) }

type predictor struct{ coef, count float64 }

func (p *predictor) predict(cplx, qscale float64) float64 {
	return p.coef * cplx / qscale
}

func (p *predictor) update(cplx, qscale, bits float64) {
	if cplx <= 0 {
		return
	}
	p.count = p.count*0.5 + 1
	n := bits * qscale / cplx
	p.coef = p.coef + (n-p.coef)/p.count
}

type rateControl struct {
	mode int // 0 CQP, 1 CRF, 2 ABR
	fps  float64

	cplxSum, cplxCount float64 // blurred complexity
	rfConst            float64 // CRF rate factor
	wantedWindow       float64 // ABR: bits wanted so far (rate-factor window)
	cplxrSum           float64 // ABR: bits*qscale/cplx^(1-qcomp) so far
	totalBits          float64
	wantedBits         float64
	lastQP             [3]float64 // per picture type
	haveLast           [3]bool

	// VBV
	vbvRate, vbvSize float64
	vbvFill          float64
	pred             [3]predictor

	// Complexity of the picture being coded, and of the last I picture.
	cplx, iCplx float64
}

func (r *rateControl) init(e *Encoder) {
	c := &e.cfg
	r.fps = float64(c.FPSNum) / float64(c.FPSDen)
	mbs := float64(e.mbW * e.mbH)
	switch {
	case c.Bitrate > 0:
		r.mode = 2
	case c.CRF > 0:
		r.mode = 1
	}
	// Calibration: a typical complexity of cplxPerMB per macroblock maps
	// CRF n to QP n for P pictures.
	const cplxPerMB = 250
	crf := c.CRF
	if r.mode == 2 {
		crf = float64(c.QP)
	}
	r.rfConst = math.Pow(mbs*cplxPerMB, 1-qcomp) / qp2qscale(crf)
	if c.VBVMaxRate > 0 && c.VBVBuffer > 0 {
		r.vbvRate, r.vbvSize = float64(c.VBVMaxRate), float64(c.VBVBuffer)
		r.vbvFill = r.vbvSize * 0.9
	}
	for i := range r.pred {
		r.pred[i] = predictor{coef: 1.5, count: 1}
	}
	if r.mode == 2 {
		// Seed the ABR window with one second at the starting QP.
		r.wantedWindow = float64(c.Bitrate) / r.fps
		r.cplxrSum = r.wantedWindow / r.rfConst
	}
}

// pictureQP returns the QP of the next picture of type typ.
func (r *rateControl) pictureQP(typ int, e *Encoder) int {
	c := &e.cfg
	r.cplx = e.estimateComplexity(typ)
	if typ == sliceI {
		r.iCplx = r.cplx
	}
	if r.mode == 0 && r.vbvSize == 0 {
		return clampQP(c.QP + typeOffset(typ))
	}
	var qp float64
	switch r.mode {
	case 0:
		qp = float64(c.QP + typeOffset(typ))
	default:
		r.cplxSum = r.cplxSum*0.5 + r.cplx
		r.cplxCount = r.cplxCount*0.5 + 1
		blurred := r.cplxSum / r.cplxCount
		rf := r.rfConst
		if r.mode == 2 {
			rf = r.wantedWindow / r.cplxrSum
		}
		q := math.Pow(blurred, 1-qcomp) / rf
		if r.mode == 2 && r.wantedBits > 0 {
			// Overflow compensation over a ~2 s buffer.
			buf := 2 * float64(c.Bitrate)
			over := 1 + (r.totalBits-r.wantedBits)/buf
			q *= math.Min(math.Max(over, 0.5), 2)
		}
		qp = qscale2qp(q) + float64(typeOffset(typ))
		if r.mode == 2 {
			t := typeIdx(typ)
			if r.haveLast[t] {
				qp = math.Min(math.Max(qp, r.lastQP[t]-qpStep), r.lastQP[t]+qpStep)
			}
		}
	}
	qp = r.vbvClamp(typ, qp, e)
	return clampQP(int(math.Round(qp)))
}

// vbvClamp raises qp until the predicted picture size leaves the VBV
// buffer at least 10% full and, ahead of the next IDR picture, with room for
// that picture given the refill until then.
func (r *rateControl) vbvClamp(typ int, qp float64, e *Encoder) float64 {
	if r.vbvSize == 0 {
		return qp
	}
	p := &r.pred[typeIdx(typ)]
	reserve := 0.1 * r.vbvSize
	if typ != sliceI && r.pred[0].count > 1 {
		until := float64(e.cfg.KeyInt - e.sinceIDR)
		iq := qp2qscale(math.Min(qp-ipOffset, 51))
		need := r.pred[0].predict(r.iCplx, iq) - until*r.vbvRate/r.fps
		reserve = math.Max(reserve, math.Min(need, 0.8*r.vbvSize))
	}
	for qp < 51 && r.vbvFill-p.predict(r.cplx, qp2qscale(qp)) < reserve {
		qp++
	}
	return qp
}

// vbvOverflow reports whether a picture of bits would underflow the VBV.
func (r *rateControl) vbvOverflow(bits int) bool {
	return r.vbvSize > 0 && r.vbvFill-float64(bits) < 0
}

// update accounts a coded picture.
func (r *rateControl) update(typ, qp, bits int, e *Encoder) {
	t := typeIdx(typ)
	b := float64(bits)
	q := qp2qscale(float64(qp))
	r.pred[t].update(r.cplx, q, b)
	r.lastQP[t], r.haveLast[t] = float64(qp), true
	r.totalBits += b
	if e.cfg.Bitrate > 0 {
		r.wantedBits += float64(e.cfg.Bitrate) / r.fps
		qb := qp2qscale(float64(qp - typeOffset(typ)))
		blurred := r.cplxSum / math.Max(r.cplxCount, 1)
		r.cplxrSum += b * qb / math.Pow(math.Max(blurred, 1), 1-qcomp)
		r.wantedWindow += float64(e.cfg.Bitrate) / r.fps
		// Forget slowly so the model tracks content changes.
		r.cplxrSum *= 0.995
		r.wantedWindow *= 0.995
	}
	if r.vbvSize > 0 {
		r.vbvFill = math.Min(r.vbvFill-b+r.vbvRate/r.fps, r.vbvSize)
	}
}

func typeOffset(typ int) int {
	switch typ {
	case sliceI:
		return -ipOffset
	case sliceB:
		return pbOffset
	}
	return 0
}

func typeIdx(typ int) int {
	switch typ {
	case sliceI:
		return 0
	case sliceB:
		return 2
	}
	return 1
}

func clampQP(qp int) int { return min(max(qp, 0), 51) }

// estimateComplexity returns a cheap complexity estimate of the source
// picture about to be coded: per macroblock, min(zero-motion SAD against the
// previous source, intra mean absolute deviation) — the intra term only
// for I pictures. Rows are subsampled by two.
func (e *Encoder) estimateComplexity(typ int) float64 {
	src := e.curSrc
	if src == nil {
		return 1
	}
	prev := e.prevSrc
	if typ == sliceI {
		prev = nil
	}
	ys := src.ys
	var total float64
	for my := 0; my < e.mbH; my++ {
		for mx := 0; mx < e.mbW; mx++ {
			o := my*16*ys + mx*16
			sum := 0
			for y := 0; y < 16; y += 2 {
				for _, v := range src.y[o+y*ys : o+y*ys+16] {
					sum += int(v)
				}
			}
			mean := (sum + 64) >> 7
			mad := 0
			for y := 0; y < 16; y += 2 {
				for _, v := range src.y[o+y*ys : o+y*ys+16] {
					d := int(v) - mean
					if d < 0 {
						d = -d
					}
					mad += d
				}
			}
			c := mad
			if prev != nil {
				sad := 0
				for y := 0; y < 16; y += 2 {
					a, b := src.y[o+y*ys:o+y*ys+16], prev.y[o+y*ys:o+y*ys+16]
					for i := range a {
						d := int(a[i]) - int(b[i])
						if d < 0 {
							d = -d
						}
						sad += d
					}
				}
				c = min(c, sad)
			}
			total += float64(2 * c)
		}
	}
	return math.Max(total, float64(e.mbW*e.mbH))
}
