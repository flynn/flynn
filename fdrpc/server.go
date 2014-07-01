package fdrpc

import (
	"bufio"
	"encoding/gob"
	"net"
	"net/rpc"
	"syscall"
)

type FDWriter struct {
	conn    *net.UnixConn
	fds     []int
	fdCount int
}

func NewFDWriter(conn *net.UnixConn) *FDWriter {
	return &FDWriter{conn: conn}
}

func (w *FDWriter) Close() error {
	return w.conn.Close()
}

func (w *FDWriter) Read(b []byte) (int, error) {
	return w.conn.Read(b)
}

func (w *FDWriter) Write(b []byte) (int, error) {
	if len(w.fds) == 0 {
		return w.conn.Write(b)
	} else {
		rights := syscall.UnixRights(w.fds...)
		n, _, err := w.conn.WriteMsgUnix(b, rights, nil)
		w.fds = nil
		return n, err
	}
}

func (w *FDWriter) AddFD(fd int) int {
	w.fds = append(w.fds, fd)
	res := w.fdCount
	w.fdCount++
	return res
}

type gobServerCodec struct {
	fdWriter *FDWriter
	dec      *gob.Decoder
	enc      *gob.Encoder
	encBuf   *bufio.Writer
}

func (c *gobServerCodec) ReadRequestHeader(r *rpc.Request) error {
	return c.dec.Decode(r)
}

func (c *gobServerCodec) ReadRequestBody(body interface{}) error {
	return c.dec.Decode(body)
}

func (c *gobServerCodec) WriteResponse(r *rpc.Response, body interface{}) (err error) {
	if fd, ok := body.(*FD); ok {
		fd.FD = c.fdWriter.AddFD(fd.FD)
	}

	if err = c.enc.Encode(r); err != nil {
		return
	}
	if err = c.enc.Encode(body); err != nil {
		return
	}
	return c.encBuf.Flush()
}

func (c *gobServerCodec) Close() error {
	return c.fdWriter.Close()
}

func ServeConn(conn *net.UnixConn) {
	fdWriter := NewFDWriter(conn)
	buf := bufio.NewWriter(fdWriter)
	srv := &gobServerCodec{fdWriter, gob.NewDecoder(fdWriter), gob.NewEncoder(buf), buf}
	rpc.ServeCodec(srv)
}
