//go:build !arm64 || purego

package av1

const haveNEON = false

func neonMcH(dst *int16, src *byte, ss, w, rows int, f *int16) { panic("unreachable") }

func neonMcV(dst *int32, src *int16, w, h int, f *int16, shift int) { panic("unreachable") }
