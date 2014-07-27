package fdrpc

import (
	"bufio"
	"encoding/gob"
	"log"
	"net"
	"syscall"

	"github.com/flynn/rpcplus"
)

type ClosingFD FD

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

func (c *gobServerCodec) ReadRequestHeader(r *rpcplus.Request) error {
	return c.dec.Decode(r)
}

func (c *gobServerCodec) ReadRequestBody(body interface{}) error {
	return c.dec.Decode(body)
}

func (c *gobServerCodec) WriteResponse(r *rpcplus.Response, body interface{}, last bool) (err error) {
	switch f := body.(type) {
	case *FD:
		f.FD = c.fdWriter.AddFD(f.FD)
	case *[]FD:
		for i, fd := range *f {
			(*f)[i].FD = c.fdWriter.AddFD(fd.FD)
		}
	case *ClosingFD:
		defer syscall.Close(f.FD)
		body = &FD{c.fdWriter.AddFD(f.FD)}
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

func ListenAndServe(path string) error {
	listener, err := net.ListenUnix("unix", &net.UnixAddr{Net: "unix", Name: path})
	if err != nil {
		return err
	}
	for {
		conn, err := listener.AcceptUnix()
		if err != nil {
			log.Printf("rpc socket accept error: %s", err)
		}
		go func() {
			defer conn.Close()
			ServeConn(conn)
		}()
	}
}

func ServeConn(conn *net.UnixConn) {
	fdWriter := NewFDWriter(conn)
	buf := bufio.NewWriter(fdWriter)
	srv := &gobServerCodec{fdWriter, gob.NewDecoder(fdWriter), gob.NewEncoder(buf), buf}
	rpcplus.ServeCodec(srv)
}
