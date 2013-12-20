package fdrpc

import (
	"bufio"
	"encoding/gob"
	"fmt"
	"net"
	"net/rpc"
	"syscall"
)

type RpcFD struct {
	Fd int
}

type FdReader struct {
	conn    *net.UnixConn
	Fds     map[int]int
	fdCount int
}

func NewFdReader(conn *net.UnixConn) *FdReader {
	return &FdReader{conn, make(map[int]int), 0}
}

func (r *FdReader) Close() error {
	return r.conn.Close()
}

func (r *FdReader) Read(b []byte) (int, error) {
	oob := make([]byte, 32)
	n, oobn, _, _, err := r.conn.ReadMsgUnix(b, oob)
	if err != nil {
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

				r.Fds[r.fdCount] = fd
				r.fdCount++
			}
		}
	}

	return n, nil
}

func (r *FdReader) Write(b []byte) (int, error) {
	return r.conn.Write(b)
}

func (r *FdReader) GetFd(index int) (int, error) {
	fd, ok := r.Fds[index]
	if !ok {
		return -1, fmt.Errorf("No recieved FD with index %d\n", index)
	}
	delete(r.Fds, index)
	return fd, nil
}

type gobClientCodec struct {
	fdReader *FdReader
	dec      *gob.Decoder
	enc      *gob.Encoder
	encBuf   *bufio.Writer
}

func (c *gobClientCodec) WriteRequest(r *rpc.Request, body interface{}) (err error) {
	if err = c.enc.Encode(r); err != nil {
		return
	}
	if err = c.enc.Encode(body); err != nil {
		return
	}
	return c.encBuf.Flush()
}

func (c *gobClientCodec) ReadResponseHeader(r *rpc.Response) error {
	return c.dec.Decode(r)
}

func (c *gobClientCodec) ReadResponseBody(body interface{}) error {
	if err := c.dec.Decode(body); err != nil {
		return err
	}
	if fd, ok := body.(*RpcFD); ok {
		index := fd.Fd
		newFd, err := c.fdReader.GetFd(index)
		if err != nil {
			return err
		}
		fd.Fd = newFd
	}
	return nil
}

func (c *gobClientCodec) Close() error {
	return c.fdReader.Close()
}

func NewClient(conn *net.UnixConn) *rpc.Client {
	fdReader := NewFdReader(conn)
	encBuf := bufio.NewWriter(fdReader)
	client := &gobClientCodec{fdReader, gob.NewDecoder(fdReader), gob.NewEncoder(encBuf), encBuf}
	return rpc.NewClientWithCodec(client)
}
