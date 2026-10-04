//go:build !darwin || !cgo || !webview_cef

package webview

// On any platform / build configuration that lacks a real webview
// backend (the default), currentBackend stays nil. Public API surface
// in webview.go consults activeBackend() and returns ErrNotSupported
// when nil. No init() registration here.
