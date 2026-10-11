//go:build !arm64 || purego

package h264

const haveNEON = false

func neonLumaH(dst *byte, ds int, src *byte, ss int, w, h int)                  { panic("unreachable") }
func neonLumaV(dst *byte, ds int, src *byte, ss int, w, h int)                  { panic("unreachable") }
func neonLumaHV(dst *byte, ds int, src *byte, ss int, w, h int, tmp *int16)     { panic("unreachable") }
func neonChroma(dst *byte, ds int, src *byte, ss int, w, h int, a, b, c, d int) { panic("unreachable") }

func neonLumaEdge(pix *byte, stride int, vert int, alpha, beta int, tc0 *int8, strong int) {
	panic("unreachable")
}

func neonChromaEdge(cb, cr *byte, stride, vert int, prm *[48]int8, strong int, gather, scatter *[64]byte) {
	panic("unreachable")
}

func neonIdct4Add(dst *byte, stride int, blk *[16]int32) { panic("unreachable") }
