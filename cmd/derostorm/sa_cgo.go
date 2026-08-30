//go:build cgo && (darwin || (linux && arm64))

package main

import (
	"github.com/deroproject/derohe/astrobwt/astrobwtv3"
	"github.com/notoriousjoshyb/derostorm/internal/sacgo"
)

func tryNativeSA() (string, bool) {
	if !sacgo.Available() {
		return "", false
	}
	astrobwtv3.SuffixSort = sacgo.SuffixArray
	note := "suffix sort: descriptor + libsais"
	if v := sacgo.Version(); v != "" {
		note += " " + v
	}
	if sacgo.PairAvailable() {
		astrobwtv3.SHA256Pair = sacgo.Pair
		nativePairOK = true
		note += ", paired SHA-256"
	}
	return note, true
}
