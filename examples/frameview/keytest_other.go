//go:build !darwin || !cgo

package main

const (
	nsCmd  = 0
	nsCtrl = 0
	nsAlt  = 0
)

func postSyntheticKey(int, string, uint, bool) bool { return false }
