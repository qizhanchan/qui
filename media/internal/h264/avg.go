package h264

// avg2 sets dst = (a + b + 1) >> 1 over a w×h block. The 8- and 16-wide
// cases — every luma partition and the larger chroma ones — run the
// assembly kernel where one exists (avg_arm64.s, avg_amd64.s).
func avg2(dst []byte, ds int, a []byte, as int, b []byte, bs int, w, h int) {
	if h <= 0 {
		return
	}
	if haveAvgAsm && (w == 16 || w == 8) {
		// The kernel does no bounds checks; do them once here.
		if len(dst) < (h-1)*ds+w || len(a) < (h-1)*as+w || len(b) < (h-1)*bs+w {
			panic("h264: avg2 out of range")
		}
		avgRows(&dst[0], ds, &a[0], as, &b[0], bs, w, h)
		return
	}
	for j := 0; j < h; j++ {
		d := dst[j*ds : j*ds+w]
		sa, sb := a[j*as:j*as+w], b[j*bs:j*bs+w]
		for i := range d {
			d[i] = byte((int(sa[i]) + int(sb[i]) + 1) >> 1)
		}
	}
}
