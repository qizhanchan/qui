//go:build !purego

package hevc

//go:generate go run neon_gen.go

// haveNEON selects the NEON kernels (neon_arm64.s); -tags purego and
// other architectures use the Go versions.
const haveNEON = true

//go:noescape
func neonFilter8H(dst *int16, ds int, src *byte, ss int, w, h int, f *int16, off int)

//go:noescape
func neonFilter4H(dst *int16, ds int, src *byte, ss int, w, h int, f *int16, off int)

//go:noescape
func neonFilter8V(dst *int16, ds int, src *byte, ss int, w, h int, f *int16, off int)

//go:noescape
func neonFilter4V(dst *int16, ds int, src *byte, ss int, w, h int, f *int16, off int)

//go:noescape
func neonFilter8V16(dst *int16, ds int, src *int16, ss int, w, h int, f *int16)

//go:noescape
func neonFilter4V16(dst *int16, ds int, src *int16, ss int, w, h int, f *int16)

//go:noescape
func neonPutUni(dst *byte, ds int, src *int16, w, h int)

//go:noescape
func neonPutBi(dst *byte, ds int, a, b *int16, w, h int)

//go:noescape
func neonSAOEdge(dst, src *byte, stride, aoff, boff, w, h int, tbl *[16]int8)

//go:noescape
func neonPutWUni(dst *byte, ds int, src *int16, w, h, wt, rnd, shift, off int)

//go:noescape
func neonPutWBi(dst *byte, ds int, a, b *int16, w, h, w0, w1, add, shift int)

//go:noescape
func neonCopyPred(dst *int16, src *byte, ss, w, h int)
