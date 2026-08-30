//go:build !windows && !(linux && amd64) && !cgo

package main

func tryNativeSA() (string, bool) { return "", false }
