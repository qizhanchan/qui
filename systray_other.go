//go:build !darwin

package qui

import "image"

// StatusItem is the non-darwin stub. All operations are no-ops; the
// constructor returns ErrSystrayNotSupported. Windows / Linux land
// in follow-up phases.
type StatusItem struct{}

// NewStatusItem returns ErrSystrayNotSupported on non-darwin builds.
// The signature matches the darwin implementation so callers can
// program against one API across platforms.
func (a *App) NewStatusItem(opts StatusItemOptions) (*StatusItem, error) {
	_ = opts
	return nil, ErrSystrayNotSupported
}

func (s *StatusItem) SetIcon(src VectorSource)  { _ = src }
func (s *StatusItem) SetImage(img image.Image)  { _ = img }
func (s *StatusItem) ClearImage()               {}
func (s *StatusItem) SetTitle(title string)     { _ = title }
func (s *StatusItem) SetTooltip(tooltip string) { _ = tooltip }
func (s *StatusItem) SetMenu(menu *NativeMenu)  { _ = menu }
func (s *StatusItem) SetVisible(visible bool)   { _ = visible }
func (s *StatusItem) Destroy()                  {}
