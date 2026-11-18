//go:build darwin && cgo

package typeset

import (
	"github.com/qizhanchan/qui/internal/gfx"
	"github.com/qizhanchan/qui/internal/platform/macos"
)

func systemTextAdvance(text []rune, name string, spec gfx.Font) (float32, bool) {
	if len(text) > 0 && (len(spec.Features) > 0 || spec.LetterSpacing != 0 || spec.WordSpacing != 0 || spec.SmallCaps) {
		return 0, false
	}
	weight := float32(effectiveWeight(spec))
	optical := spec.Size
	if v, ok := spec.Variations["wght"]; ok {
		weight = v
	}
	if v, ok := spec.Variations["opsz"]; ok {
		optical = v
	}
	return macos.SystemTextAdvance(text, name, spec.Size, weight, optical, spec.Italic)
}
