//go:build !darwin || !cgo

package pdf

// printPDF has no native backend off macOS (or without cgo). Callers can
// still produce PDF files via Doc.WriteFile and print them externally.
func printPDF(pdf []byte, opts PrintOptions) error {
	_ = pdf
	_ = opts
	return ErrPrintNotSupported
}
