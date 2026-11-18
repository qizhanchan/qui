//go:build darwin && cgo

package typeset

import "github.com/qizhanchan/qui/internal/platform/macos"

func platformHyphenPoints(word, language string) []int {
	return macos.HyphenPoints(word, language)
}
