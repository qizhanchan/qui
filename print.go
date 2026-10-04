package qui

import "errors"

// PrintOptions configures a native print job. Page size and orientation flow
// from the PDF's own MediaBox, so they are not repeated here.
type PrintOptions struct {
	// JobTitle names the job in the print queue (defaults to "qui").
	JobTitle string
	// ShowPanel presents the interactive print panel (printer selection,
	// copies, and the system "PDF ▸ Save as PDF" menu). When false the job
	// goes straight to the default printer with no UI.
	ShowPanel bool
}

// ErrPrintNotSupported is returned by Print on platforms without a native
// print backend.
var ErrPrintNotSupported = errors.New("qui: printing not supported on this platform")

// Print sends a PDF document to the platform's native print system. On macOS
// this shows the standard print panel (when opts.ShowPanel is true), which
// also offers "Save as PDF". The pdf argument is a complete PDF file, e.g.
// from PDFDoc.Bytes().
//
// Must be called on the main goroutine (like the native file dialogs) — the
// panel runs modally and blocks the frame loop until dismissed. From another
// goroutine, dispatch via Window.PostJob.
func Print(pdf []byte, opts PrintOptions) error {
	return printPDF(pdf, opts)
}
