//go:build !(arm64 || amd64) || purego

package h264

const haveAvgAsm = false

func avgRows(dst *byte, ds int, a *byte, as int, b *byte, bs int, w, h int) {
	panic("unreachable")
}
