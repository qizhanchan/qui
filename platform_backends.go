package qui

// Compiled-in platform backends. Each registers itself with
// internal/platform/contract from init; the highest-priority one is the
// default (platform_select.go). Per-OS imports live in the
// platform_backends_*.go files beside this one.

import _ "github.com/qizhanchan/qui/internal/platform/headless"
