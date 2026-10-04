//go:build !darwin || !cgo

package qui

// Non-macOS / no-cgo build: no default emoji provider. Apps can still
// register their own (e.g., Twemoji PNG atlas) via SetEmojiProvider.
