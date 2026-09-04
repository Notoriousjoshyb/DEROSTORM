//go:build windows

package main

// The suffix sort and paired SHA are called once per hash. purego's general
// libffi bridge handles arbitrary signatures on every supported platform, but
// that generality allocates argument state on each call. Windows amd64 has one
// native calling convention for C and system DLLs, so SyscallN can invoke the
// same addresses directly with a stack-resident argument vector.

import (
	"runtime"
	"syscall"
	"unsafe"
)

func bindSAFunc(dst any, addr uintptr) {
	switch fn := dst.(type) {
	case *func(*byte, *int32, int32, int32) int32:
		*fn = func(text *byte, sa *int32, n, fs int32) int32 {
			r, _, _ := syscall.SyscallN(addr,
				uintptr(unsafe.Pointer(text)), uintptr(unsafe.Pointer(sa)),
				uintptr(n), uintptr(fs))
			runtime.KeepAlive(text)
			runtime.KeepAlive(sa)
			return int32(r)
		}
	case *func(*byte, int32):
		*fn = func(buf *byte, n int32) {
			syscall.SyscallN(addr, uintptr(unsafe.Pointer(buf)), uintptr(n))
			runtime.KeepAlive(buf)
		}
	case *func() int32:
		*fn = func() int32 {
			r, _, _ := syscall.SyscallN(addr)
			return int32(r)
		}
	case *func(*byte, int32, *byte, *byte, int32, *byte):
		*fn = func(a *byte, na int32, outA *byte,
			b *byte, nb int32, outB *byte) {
			syscall.SyscallN(addr,
				uintptr(unsafe.Pointer(a)), uintptr(na), uintptr(unsafe.Pointer(outA)),
				uintptr(unsafe.Pointer(b)), uintptr(nb), uintptr(unsafe.Pointer(outB)))
			runtime.KeepAlive(a)
			runtime.KeepAlive(outA)
			runtime.KeepAlive(b)
			runtime.KeepAlive(outB)
		}
	default:
		panic("unsupported suffix-library function signature")
	}
}
