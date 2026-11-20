//go:build !darwin || !cgo

package typeset

func platformHasFontLookup() bool { return false }

func platformFontForRune(r rune) (path, postScript string, ok bool) { return "", "", false }

func platformCJKFont() (path, postScript string, ok bool) { return "", "", false }

func platformFamilyFaces(family string) []systemFace { return nil }

func platformFontFamilies() ([]FontFamilyInfo, bool) { return nil, false }
