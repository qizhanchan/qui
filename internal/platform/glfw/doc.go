// Package glfw is the GLFW 3.3 backend. It is compiled in only on Windows,
// where no native backend exists yet, or anywhere with -tags qui_glfw (for
// comparing against the native backends). On macOS it adds a bridge that
// swizzles gesture and exact-scroll handling into GLFW's content view.
package glfw
