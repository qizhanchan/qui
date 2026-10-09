//go:build !darwin || !cgo

package main

import "github.com/qizhanchan/qui/media"

type cvSource struct{}

func newCVSource(w, h int) *cvSource                { return nil }
func (s *cvSource) push(fv *media.FrameView, n int) {}

const haveCV = false
