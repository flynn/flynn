// +build windows

package term

import (
	"os"
	"syscall"
)

func IsANSI(f *os.File) bool {
	return false
}

// IsTerminal returns false on Windows.
func IsTerminal(f *os.File) bool {
	ft, _ := syscall.GetFileType(syscall.Handle(f.Fd()))
	return ft == syscall.FILE_TYPE_CHAR
}

// MakeRaw is a no-op on windows. It returns nil.
func MakeRaw(f *os.File) error {
	return nil
}

// Restore is a no-op on windows. It returns nil.
func Restore(f *os.File) error {
	return nil
}

// Cols returns 80 on Windows.
func Cols() (int, error) {
	return 80, nil
}

// Lines returns 24 on Windows.
func Lines() (int, error) {
	return 24, nil
}
