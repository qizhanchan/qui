// Package cocoa is the native macOS backend: AppKit directly, with our own
// NSApplication event pump, NSWindow/NSView (QuiView, an NSTextInputClient)
// and an NSOpenGLContext per window. Gestures are ordinary responder
// methods and scroll events carry their real modifiers and phase.
//
// It is the default backend on darwin+cgo builds (QUI_PLATFORM=cocoa).
package cocoa
