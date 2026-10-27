//go:build darwin && cgo

package pdf

import (
	"errors"

	"github.com/qizhanchan/qui/internal/platform/macos"
)

func printPDF(pdf []byte, opts PrintOptions) error {
	if len(pdf) == 0 {
		return errors.New("qui: empty PDF")
	}
	title := opts.JobTitle
	if title == "" {
		title = "qui"
	}
	rc := macos.PrintPDF(pdf, title, opts.ShowPanel)
	switch rc {
	case 0:
		return nil
	case 1:
		return errors.New("qui: invalid PDF for printing")
	default:
		return errors.New("qui: print operation failed or was cancelled")
	}
}
