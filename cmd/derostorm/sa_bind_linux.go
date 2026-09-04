//go:build linux && amd64

package main

import "github.com/ebitengine/purego"

// Linux needs libffi's portable native-call bridge. Windows has one ABI on
// amd64 and can use syscall.SyscallN directly; see sa_bind_windows.go.
func bindSAFunc(dst any, addr uintptr) {
	purego.RegisterFunc(dst, addr)
}
