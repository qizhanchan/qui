//go:build unix

package typeset

import (
	"os"
	"syscall"
)

// mapFontFile maps a font file read-only. The pages are file-backed and
// clean, so the OS loads only what is touched and can drop them under
// pressure — unlike os.ReadFile, which copies the whole file into the Go
// heap for the life of the font. The mapping is never unmapped: fonts live
// for the process, and sfnt keeps slices into the bytes.
//
// Only for files that do not change under us (the read-only system font
// directories): truncating a mapped file faults the next read with SIGBUS.
func mapFontFile(path string) ([]byte, bool) {
	f, err := os.Open(path)
	if err != nil {
		return nil, false
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil || st.Size() <= 0 || int64(int(st.Size())) != st.Size() {
		return nil, false
	}
	data, err := syscall.Mmap(int(f.Fd()), 0, int(st.Size()), syscall.PROT_READ, syscall.MAP_SHARED)
	if err != nil {
		return nil, false
	}
	return data, true
}

func unmapFontFile(data []byte) { _ = syscall.Munmap(data) }
