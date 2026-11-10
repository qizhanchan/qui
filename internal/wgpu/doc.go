// Package wgpu is qui's binding to the WebGPU C API (webgpu.h), backed by
// Dawn — Chromium's WebGPU implementation, which runs on Metal, D3D12 and
// Vulkan. See docs/rfc/0003-webgpu.zh.md.
//
// Nothing links against Dawn at build time: Dawn's webgpu.h
// (dawn_webgpu.h, kept beside the Go files so `go mod vendor` copies it)
// is compiled in, every wgpu* entry point is a forwarder (procs_gen.c), and
// Load opens the Dawn shared library at run time. A machine without it
// still builds and runs qui; Load then fails and windows present CPU
// frames.
//
// The Go surface covers what qui and its GPU widgets use (textures,
// buffers, WGSL shaders, render pipelines, render passes, surfaces, and on
// macOS IOSurface import), not the whole API. All objects belong to one
// process-wide device (Shared), so windows, offscreen targets and widgets
// share textures freely. Every call must be made on the UI thread.
package wgpu

//go:generate go run gen/genproc.go
//go:generate go run gen/genstub.go

import "errors"

// ErrUnavailable is returned by Load in builds without cgo (no WebGPU).
var ErrUnavailable = errors.New("wgpu: WebGPU is not available in this build")

// DawnRevision is the Dawn commit dawn_webgpu.h was taken from. The shared
// library Load opens must come from the same revision: webgpu.h's
// extension structs and enums are not ABI-stable across Dawn revisions.
const DawnRevision = "66f899221d41dbc6d6257350c4b654a9b375c7f2"

// DawnRelease is the github.com/google/dawn release tag that ships
// binaries of DawnRevision (cmd/qui-dawn downloads it).
const DawnRelease = "v20261106.204713"
