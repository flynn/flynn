// Copyright 2018 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// +build windows

package filelock

import (
	"os"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

type lockType uint32

const (
	readLock  lockType = 0
	writeLock lockType = 0x2
)

const (
	reserved = 0
	allBytes = ^uint32(0)
)

var (
	modkernel32      = syscall.NewLazyDLL("kernel32.dll")
	procLockFileEx   = modkernel32.NewProc("LockFileEx")
	procUnlockFileEx = modkernel32.NewProc("UnlockFileEx")
)

// Do the interface allocations only once for common
// Errno values.
const errnoERROR_IO_PENDING = 997

var errERROR_IO_PENDING error = syscall.Errno(errnoERROR_IO_PENDING)

// errnoErr returns common boxed Errno values, to prevent
// allocations at runtime.
func errnoErr(e syscall.Errno) error {
	switch e {
	case 0:
		return nil
	case errnoERROR_IO_PENDING:
		return errERROR_IO_PENDING
	}
	// TODO: add more here, after collecting data on the common
	// error values see on Windows. (perhaps when running
	// all.bat?)
	return e
}

func lockFileEx(file syscall.Handle, flags uint32, reserved uint32, bytesLow uint32, bytesHigh uint32, overlapped *syscall.Overlapped) (err error) {
	r1, _, e1 := syscall.Syscall6(procLockFileEx.Addr(), 6, uintptr(file), uintptr(flags), uintptr(reserved), uintptr(bytesLow), uintptr(bytesHigh), uintptr(unsafe.Pointer(overlapped)))
	if r1 == 0 {
		if e1 != 0 {
			err = errnoErr(e1)
		} else {
			err = syscall.EINVAL
		}
	}
	return
}

func unlockFileEx(file syscall.Handle, reserved uint32, bytesLow uint32, bytesHigh uint32, overlapped *syscall.Overlapped) (err error) {
	r1, _, e1 := syscall.Syscall6(procUnlockFileEx.Addr(), 5, uintptr(file), uintptr(reserved), uintptr(bytesLow), uintptr(bytesHigh), uintptr(unsafe.Pointer(overlapped)), 0)
	if r1 == 0 {
		if e1 != 0 {
			err = errnoErr(e1)
		} else {
			err = syscall.EINVAL
		}
	}
	return
}

func lock(f File, lt lockType) error {
	// Per https://golang.org/issue/19098, “Programs currently expect the Fd
	// method to return a handle that uses ordinary synchronous I/O.”
	// However, LockFileEx still requires an OVERLAPPED structure,
	// which contains the file offset of the beginning of the lock range.
	// We want to lock the entire file, so we leave the offset as zero.
	ol := new(syscall.Overlapped)

	err := lockFileEx(syscall.Handle(f.Fd()), uint32(lt), reserved, allBytes, allBytes, ol)
	if err != nil {
		return &os.PathError{
			Op:   lt.String(),
			Path: f.Name(),
			Err:  err,
		}
	}
	return nil
}

func unlock(f File) error {
	ol := new(syscall.Overlapped)
	err := unlockFileEx(syscall.Handle(f.Fd()), reserved, allBytes, allBytes, ol)
	if err != nil {
		return &os.PathError{
			Op:   "Unlock",
			Path: f.Name(),
			Err:  err,
		}
	}
	return nil
}

func isNotSupported(err error) bool {
	switch err {
	case windows.ERROR_NOT_SUPPORTED, windows.ERROR_CALL_NOT_IMPLEMENTED, ErrNotSupported:
		return true
	default:
		return false
	}
}
