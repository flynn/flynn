package cluster

import (
	"bufio"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"sync"

	"github.com/flynn/flynn/host/types"
)

// ErrWouldWait is returned when the Attach should not wait, but the job is not
// running.
var ErrWouldWait = errors.New("cluster: attach would wait")

// Attach attaches to the job specified in req and returns an attach client. If
// wait is true, the client will wait for the job to start before returning the
// first bytes. If wait is false and the job is not running, ErrWouldWait is
// returned.
func (c *Host) Attach(req *host.AttachReq, wait bool) (AttachClient, error) {
	rwc, err := c.c.Hijack("POST", "/attach", http.Header{"Upgrade": {"flynn-attach/0"}}, req)
	if err != nil {
		return nil, err
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

// NewAttachClient wraps conn in an implementation of AttachClient.
func NewAttachClient(conn io.ReadWriteCloser) AttachClient {
	return &attachClient{conn: conn, w: bufio.NewWriter(conn)}
}

// An AttachClient provides access to the stdin/stdout/stderr streams of a job
// and allows sending UNIX signals to it.
type AttachClient interface {
	// Conn returns the underlying transport stream for the client.
	Conn() io.ReadWriteCloser

	// Receive reads stdout/stderr frames from the connection and writes them to
	// stdout and stderr. If the job exits, the return int will be set to the
	// exit code.
	Receive(stdout, stderr io.Writer) (int, error)

	// Wait waits for the job to start. It may optionally be called before
	// calling Receive.
	Wait() error

	// Signal sends a Unix signal to the job.
	Signal(int) error

	// ResizeTTY resizes the job's TTY.
	ResizeTTY(height, width uint16) error

	// CloseWrite sends an EOF to the stdin stream.
	CloseWrite() error

	// Writer allows writing to the stdin stream.
	io.Writer

	// Closer allows closing the underlying transport connection.
	io.Closer
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
			return -1, err
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
			return -1, err
		}
		switch frameType {
		case host.AttachData:
			stream, err := r.ReadByte()
			if err != nil {
				return -1, err
			}
			var out *io.Writer
			switch stream {
			case 1:
				if stdout == nil {
					return -1, errors.New("attach: got frame for stdout, but no writer available")
				}
				out = &stdout
			case 2, 3:
				if stderr == nil {
					return -1, errors.New("attach: got frame for stderr / initLog, but no writer available")
				}
				out = &stderr
			default:
				return -1, fmt.Errorf("attach: unknown stream %d", stream)
			}
			if _, err := io.ReadFull(r, buf[:]); err != nil {
				return -1, err
			}
			length := int64(binary.BigEndian.Uint32(buf[:]))
			if length == 0 {
				*out = nil
				continue
			}
			if _, err := io.CopyN(*out, r, length); err != nil {
				return -1, err
			}
		case host.AttachExit:
			if _, err := io.ReadFull(r, buf[:]); err != nil {
				return -1, err
			}
			return int(binary.BigEndian.Uint32(buf[:])), nil
		case host.AttachError:
			if _, err := io.ReadFull(r, buf[:]); err != nil {
				return -1, err
			}
			length := int64(binary.BigEndian.Uint32(buf[:]))
			errBytes := make([]byte, length)
			if _, err := io.ReadFull(r, errBytes); err != nil {
				return -1, err
			}
			return -1, errors.New(string(errBytes))
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
