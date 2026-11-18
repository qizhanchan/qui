package vp9

import "os"

func init() { debugPanic = os.Getenv("VP9_PANIC") != "" }
