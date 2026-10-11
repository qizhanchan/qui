//go:build !arm64 || purego

package h264enc

const haveNEON = false

func neonSAD(a *byte, as int, b *byte, bs int, w, h int, out *int)  { panic("unreachable") }
func neonSATD(a *byte, as int, b *byte, bs int, w, h int, out *int) { panic("unreachable") }

func neonDCTQuant8(src *byte, ss int, pred *byte, ps int, mf *[4][8]uint16, bias, qbits int, out *[64]int32, mask *int) {
	panic("unreachable")
}
