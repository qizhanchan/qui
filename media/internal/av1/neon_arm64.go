//go:build !purego

package av1

//go:generate go run neon_gen.go

// haveNEON selects the NEON kernels (neon_arm64.s); -tags purego and
// other architectures use the Go versions.
const haveNEON = true

//go:noescape
func neonMcH(dst *int16, src *byte, ss, w, rows int, f *int16)

//go:noescape
func neonMcV(dst *int32, src *int16, w, h int, f *int16, shift int)
