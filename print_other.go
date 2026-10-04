//go:build !darwin || !cgo

package qui

// printPDF has no native backend off macOS (or without cgo). Callers can
// still produce PDF files via PDFDoc.WritePDFFile and print them externally.
func printPDF(pdf []byte, opts PrintOptions) error {
	_ = pdf
	_ = opts
	return ErrPrintNotSupported
}
