package qui

import (
	"fmt"
	"os/exec"
	"runtime"
)

// OpenURL opens url in the user's default web browser. It shells out to the
// platform's standard launcher (open / xdg-open / rundll32) and returns
// immediately without waiting for the browser. Pure Go, no cgo — safe to call
// from any goroutine.
func OpenURL(url string) error {
	if url == "" {
		return fmt.Errorf("qui: OpenURL: empty url")
	}
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", url)
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	default: // linux, bsd, ...
		cmd = exec.Command("xdg-open", url)
	}
	return cmd.Start()
}
