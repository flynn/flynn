package fdrpc

import (
	"bufio"
	"encoding/gob"
	"fmt"
	"net"
	"syscall"

	"github.com/flynn/rpcplus"
)

type FD struct {
	FD int
}

type FDReader struct {
	conn    *net.UnixConn
	FDs     map[int]int
	fdCount int
}

func NewFDReader(conn *net.UnixConn) *FDReader {
	return &FDReader{conn, make(map[int]int), 0}
}

func (r *FDReader) Close() error {
	return r.conn.Close()
}

func (r *FDReader) Read(b []byte) (int, error) {
	oob := make([]byte, 32)
	n, oobn, _, _, err := r.conn.ReadMsgUnix(b, oob)
	if err != nil {
		if n < 0 {
			n = 0
		}
		return n, err
	}
	if oobn > 0 {
		messages, err := syscall.ParseSocketControlMessage(oob[:oobn])
		if err != nil {
			return n, err
		}
		for _, m := range messages {
			fds, err := syscall.ParseUnixRights(&m)
			if err != nil {
				return n, err
			}

			// Set the CLOEXEC flag on the FDs so they won't be leaked into future forks
			for _, fd := range fds {
				if _, _, errno := syscall.Syscall(syscall.SYS_FCNTL, uintptr(fd), syscall.F_SETFD, syscall.FD_CLOEXEC); errno != 0 {
					return n, errno
				}

				r.FDs[r.fdCount] = fd
				r.fdCount++
			}
		}
	}

	return n, nil
}

func (r *FDReader) Write(b []byte) (int, error) {
	return r.conn.Write(b)
}

func (r *FDReader) GetFD(index int) (int, error) {
	fd, ok := r.FDs[index]
	if !ok {
		return -1, fmt.Errorf("No received FD with index %d\n", index)
	}
	delete(r.FDs, index)
	return fd, nil
}

type gobClientCodec struct {
	fdReader *FDReader
	dec      *gob.Decoder
	enc      *gob.Encoder
	encBuf   *bufio.Writer
}

func (c *gobClientCodec) WriteRequest(r *rpcplus.Request, body interface{}) (err error) {
	if err = c.enc.Encode(r); err != nil {
		return
	}
	if err = c.enc.Encode(body); err != nil {
		return
	}
	return c.encBuf.Flush()
}

func (c *gobClientCodec) ReadResponseHeader(r *rpcplus.Response) error {
	return c.dec.Decode(r)
}

func (c *gobClientCodec) ReadResponseBody(body interface{}) error {
	if err := c.dec.Decode(body); err != nil {
		return err
	}

	var err error
	switch f := body.(type) {
	case *FD:
		f.FD, err = c.fdReader.GetFD(f.FD)
		if err != nil {
			return err
		}
	case *[]FD:
		for i, fd := range *f {
			(*f)[i].FD, err = c.fdReader.GetFD(fd.FD)
			if err != nil {
				return err
			}
		}
	}

	return nil
}

func (c *gobClientCodec) Close() error {
	return c.fdReader.Close()
}

func NewClient(conn *net.UnixConn) *rpcplus.Client {
	fdReader := NewFDReader(conn)
	encBuf := bufio.NewWriter(fdReader)
	client := &gobClientCodec{fdReader, gob.NewDecoder(fdReader), gob.NewEncoder(encBuf), encBuf}
	return rpcplus.NewClientWithCodec(client)
}

func Dial(path string) (*rpcplus.Client, error) {
	conn, err := net.DialUnix("unix", nil, &net.UnixAddr{Net: "unix", Name: path})
	if err != nil {
		return nil, err
	}
	return NewClient(conn), nil
}
