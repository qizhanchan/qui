//go:build !darwin || !cgo

package qui

func registerPlatformHotKey(int, Key, Modifiers) (func(), error) {
	return nil, ErrGlobalHotKeyUnsupported
}
