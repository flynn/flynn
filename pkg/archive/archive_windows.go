// +build windows

package archive

import (
	"errors"
	"io"
)

func Unpack(decompressedArchive io.Reader, dest string, lchown bool) error {
	return errors.New("archive.Unpack is not supported on Windows")
}
