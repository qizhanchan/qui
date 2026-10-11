//go:build !(darwin && cgo) && !(linux && cgo) && !(windows && (amd64 || arm64))

package audioout

// open returns ErrNotSupported when no native audio sink is compiled in
// (CGO disabled on macOS / Linux, or a platform without a sink).
func open(format Format) (Sink, error) { return nil, ErrNotSupported }

func available() bool { return false }
