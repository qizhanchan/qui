//go:build !unix

package typeset

// mapFontFile reports false where mmap is not wired up; callers read the
// file into memory instead.
func mapFontFile(path string) ([]byte, bool) { return nil, false }

func unmapFontFile(data []byte) {}
