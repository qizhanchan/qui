// Package cocoa is the native macOS backend: AppKit directly, with our own
// NSApplication event pump, NSWindow/NSView (QuiView, an NSTextInputClient)
// presenting either CPU frames as layer contents or through a CAMetalLayer
// (the WebGPU renderer). Gestures are ordinary responder methods and
// scroll events carry their real modifiers and phase.
//
// It is the default backend on darwin+cgo builds (QUI_PLATFORM=cocoa).
package cocoa
