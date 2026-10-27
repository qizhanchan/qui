//go:build !darwin || !cgo

package typeset

// Non-macOS / no-cgo build: no default emoji provider. Apps can still
// register their own (e.g., Twemoji PNG atlas) via SetEmojiProvider.
