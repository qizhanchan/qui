//go:build !darwin || !cgo

package typeset

func platformHyphenPoints(word, language string) []int { return nil }
