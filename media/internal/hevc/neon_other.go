//go:build !arm64 || purego

package hevc

const haveNEON = false

func neonFilter8H(dst *int16, ds int, src *byte, ss int, w, h int, f *int16, off int) {
	panic("unreachable")
}

func neonFilter4H(dst *int16, ds int, src *byte, ss int, w, h int, f *int16, off int) {
	panic("unreachable")
}

func neonFilter8V(dst *int16, ds int, src *byte, ss int, w, h int, f *int16, off int) {
	panic("unreachable")
}

func neonFilter4V(dst *int16, ds int, src *byte, ss int, w, h int, f *int16, off int) {
	panic("unreachable")
}

func neonFilter8V16(dst *int16, ds int, src *int16, ss int, w, h int, f *int16) { panic("unreachable") }
func neonFilter4V16(dst *int16, ds int, src *int16, ss int, w, h int, f *int16) { panic("unreachable") }
func neonPutUni(dst *byte, ds int, src *int16, w, h int)                        { panic("unreachable") }
func neonPutBi(dst *byte, ds int, a, b *int16, w, h int)                        { panic("unreachable") }

func neonSAOEdge(dst, src *byte, stride, aoff, boff, w, h int, tbl *[16]int8) { panic("unreachable") }
