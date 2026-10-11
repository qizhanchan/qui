//go:build (arm64 || amd64) && !purego

package h264

const haveAvgAsm = true

// avgRows averages h rows of w bytes (w is 8 or 16): dst = (a+b+1)>>1.
//
//go:noescape
func avgRows(dst *byte, ds int, a *byte, as int, b *byte, bs int, w, h int)
