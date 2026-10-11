//go:build !darwin || !cgo

package audioout

// open returns ErrNotSupported when no native audio sink is compiled in
// (CGO disabled, or a non-macOS platform without a sink yet).
func open(format Format) (Sink, error) { return nil, ErrNotSupported }

func available() bool { return false }
