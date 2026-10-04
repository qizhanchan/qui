package webview

// PreInitialize forces backend runtime initialization before any
// WebView widget is opened. This is primarily useful on macOS when
// callers want to control initialization order relative to other UI
// frameworks.
func PreInitialize() error {
	b := activeBackend()
	if b == nil {
		return ErrNotSupported
	}
	if p, ok := b.(interface{ preInitialize() error }); ok {
		return p.preInitialize()
	}
	return nil
}
