package cluster

import (
	"bufio"
	"bytes"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"net/http/httputil"
	"sync"

	"github.com/flynn/flynn/host/types"
)

var ErrWouldWait = errors.New("cluster: attach would wait")

func (c *hostClient) Attach(req *host.AttachReq, wait bool) (AttachClient, error) {
	data, err := json.Marshal(req)
	if err != nil {
		return nil, err
	}
	httpReq, err := http.NewRequest("POST", "/attach", bytes.NewBuffer(data))
	if err != nil {
		return nil, err
	}
	conn, err := c.dial("tcp", c.addr)
	if err != nil {
		return nil, err
	}
	clientconn := httputil.NewClientConn(conn, nil)
	res, err := clientconn.Do(httpReq)
	if err != nil && err != httputil.ErrPersistEOF {
		return nil, err
	}
	if res.StatusCode != 200 {
		return nil, fmt.Errorf("cluster: unexpected status %d", res.StatusCode)
	}
	var rwc io.ReadWriteCloser
	var buf *bufio.Reader
	rwc, buf = clientconn.Hijack()
	if buf.Buffered() > 0 {
		rwc = struct {
			io.Reader
			io.WriteCloser
		}{
			io.MultiReader(io.LimitReader(buf, int64(buf.Buffered())), rwc),
			rwc,
		}
	}

	attachState := make([]byte, 1)
	if _, err := rwc.Read(attachState); err != nil {
		rwc.Close()
		return nil, err
	}

	handleState := func() error {
		switch attachState[0] {
		case host.AttachSuccess:
			return nil
		case host.AttachError:
			errBytes, err := ioutil.ReadAll(rwc)
			rwc.Close()
			if err != nil {
				return err
			}
			if len(errBytes) >= 4 {
				errBytes = errBytes[4:]
			}
			return errors.New(string(errBytes))
		default:
			rwc.Close()
			return fmt.Errorf("cluster: unknown attach state: %d", attachState)
		}
	}

	if attachState[0] == host.AttachWaiting {
		if !wait {
			rwc.Close()
			return nil, ErrWouldWait
		}
		wait := func() error {
			if _, err := rwc.Read(attachState); err != nil {
				rwc.Close()
				return err
			}
			return handleState()
		}
		c := &attachClient{
			conn: rwc,
			wait: wait,
			w:    bufio.NewWriter(rwc),
		}
		c.mtx.Lock()
		return c, nil
	}

	return NewAttachClient(rwc), handleState()
}

func NewAttachClient(conn io.ReadWriteCloser) AttachClient {
	return &attachClient{conn: conn, w: bufio.NewWriter(conn)}
}

type AttachClient interface {
	Conn() io.ReadWriteCloser
	Receive(stdout, stderr io.Writer) (int, error)
	Wait() error
	Signal(int) error
	ResizeTTY(height, width uint16) error
	CloseWrite() error
	io.WriteCloser
}

type attachClient struct {
	conn io.ReadWriteCloser
	wait func() error

	mtx sync.Mutex
	w   *bufio.Writer
	buf [4]byte
}

func (c *attachClient) Conn() io.ReadWriteCloser {
	return c.conn
}

func (c *attachClient) Wait() error {
	if c.wait == nil {
		return nil
	}
	return c.wait()
}

func (c *attachClient) Receive(stdout, stderr io.Writer) (int, error) {
	if c.wait != nil {
		if err := c.wait(); err != nil {
			return 0, err
		}
		c.mtx.Unlock()
	}
	r := bufio.NewReader(c.conn)
	var buf [4]byte
	for {
		frameType, err := r.ReadByte()
		if err != nil {
			if err == io.EOF && stdout == nil && stderr == nil {
				err = nil
			}
			return 0, err
		}
		switch frameType {
		case host.AttachData:
			stream, err := r.ReadByte()
			if err != nil {
				return 0, err
			}
			var out *io.Writer
			switch stream {
			case 1:
				if stdout == nil {
					return 0, errors.New("attach: got frame for stdout, but no writer available")
				}
				out = &stdout
			case 2:
				if stderr == nil {
					return 0, errors.New("attach: got frame for stderr, but no writer available")
				}
				out = &stderr
			default:
				return 0, fmt.Errorf("attach: unknown stream %d", stream)
			}
			if _, err := io.ReadFull(r, buf[:]); err != nil {
				return 0, err
			}
			length := int64(binary.BigEndian.Uint32(buf[:]))
			if length == 0 {
				*out = nil
				continue
			}
			if _, err := io.CopyN(*out, r, length); err != nil {
				return 0, err
			}
		case host.AttachExit:
			if _, err := io.ReadFull(r, buf[:]); err != nil {
				return 0, err
			}
			return int(binary.BigEndian.Uint32(buf[:])), nil
		}
	}
}

func (c *attachClient) Write(p []byte) (int, error) {
	c.mtx.Lock()
	defer c.mtx.Unlock()

	if len(p) == 0 {
		return 0, nil
	}

	c.w.WriteByte(host.AttachData)
	c.w.WriteByte(0) // stdin stream
	binary.BigEndian.PutUint32(c.buf[:], uint32(len(p)))
	c.w.Write(c.buf[:])
	n, _ := c.w.Write(p)
	return n, c.w.Flush()
}

func (c *attachClient) Signal(sig int) error {
	c.mtx.Lock()
	defer c.mtx.Unlock()

	c.w.WriteByte(host.AttachSignal)
	binary.BigEndian.PutUint32(c.buf[:], uint32(sig))
	c.w.Write(c.buf[:])
	return c.w.Flush()
}

func (c *attachClient) ResizeTTY(height, width uint16) error {
	c.mtx.Lock()
	defer c.mtx.Unlock()

	c.w.WriteByte(host.AttachResize)
	binary.BigEndian.PutUint16(c.buf[:], height)
	binary.BigEndian.PutUint16(c.buf[2:], width)
	c.w.Write(c.buf[:])
	return c.w.Flush()
}

func (c *attachClient) CloseWrite() error {
	c.mtx.Lock()
	defer c.mtx.Unlock()

	c.w.WriteByte(host.AttachData)
	c.w.WriteByte(0) // stdin stream
	binary.BigEndian.PutUint32(c.buf[:], 0)
	c.w.Write(c.buf[:])
	return c.w.Flush()
}

func (c *attachClient) Close() error {
	c.mtx.Lock()
	defer c.mtx.Unlock()
	return c.conn.Close()
}
