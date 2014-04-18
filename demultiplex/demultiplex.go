// Package demultiplex demultiplexes Docker attach streams
package demultiplex

import (
	"bytes"
	"encoding/binary"
	"io"
)

func Streams(r io.Reader) (stdout, stderr io.Reader) {
	outr, outw := io.Pipe()
	errr, errw := io.Pipe()
	go func() {
		read := frameReader(r)
		for {
			typ, data, err := read()
			if typ == frameTypeStderr {
				if _, err := errw.Write(data); err != nil {
					outw.Close()
					return
				}
			} else {
				if _, err := outw.Write(data); err != nil {
					errw.Close()
					return
				}
			}
			if err != nil {
				outw.CloseWithError(err)
				errw.CloseWithError(err)
				return
			}
		}
	}()
	return outr, errr
}

type frameType byte

const (
	frameTypeStdin frameType = iota
	frameTypeStdout
	frameTypeStderr
)

func frameReader(r io.Reader) func() (frameType, []byte, error) {
	var buf bytes.Buffer
	var header [8]byte
	return func() (frameType, []byte, error) {
		buf.Reset()
		if _, err := io.ReadFull(r, header[:]); err != nil {
			return 0, nil, err
		}
		ft := frameType(header[0])
		length := int(binary.BigEndian.Uint32(header[4:]))
		buf.Grow(length)
		data := buf.Bytes()[:length]
		n, err := io.ReadFull(r, data)
		return ft, data[:n], err
	}
}

func Clean(r io.Reader) io.Reader {
	pr, pw := io.Pipe()
	go func() {
		read := frameReader(r)
		for {
			_, data, err := read()
			if _, err := pw.Write(data); err != nil {
				return
			}
			if err != nil {
				pw.CloseWithError(err)
				return
			}
		}
	}()
	return pr
}

func Copy(stdout, stderr io.Writer, r io.Reader) error {
	read := frameReader(r)
	for {
		t, data, err := read()
		var ew error
		if len(data) > 0 {
			if stderr != nil && t == frameTypeStderr {
				_, ew = stderr.Write(data)
			} else {
				_, ew = stdout.Write(data)
			}
			if ew != nil {
				return ew
			}
		}
		if err != nil {
			if err == io.EOF {
				err = nil
			}
			return err
		}
	}
}
