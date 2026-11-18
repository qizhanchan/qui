//go:build !darwin || !cgo

package typeset

import "github.com/qizhanchan/qui/internal/gfx"

func systemTextAdvance(text []rune, name string, spec gfx.Font) (float32, bool) { return 0, false }
