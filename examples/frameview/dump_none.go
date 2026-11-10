//go:build !cgo || js

package main

import (
	"sync/atomic"

	"github.com/qizhanchan/qui"
	"github.com/qizhanchan/qui/widgets"
)

// setupDump needs WebGPU (a post-render capture); without it there is
// nothing to dump.
func setupDump(*qui.Window, *widgets.Label, *atomic.Bool) {}
