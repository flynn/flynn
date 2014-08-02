package cluster

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"net/http/httputil"

	"github.com/flynn/flynn-host/types"
)

type ReadWriteCloser interface {
	io.ReadWriteCloser
	CloseWrite() error
}

type writeCloser interface {
	io.WriteCloser
	CloseWrite() error
}

var ErrWouldWait = errors.New("cluster: attach would wait")

func (c *hostClient) Attach(req *host.AttachReq, wait bool) (ReadWriteCloser, func() error, error) {
	data, err := json.Marshal(req)
	if err != nil {
		return nil, nil, err
	}
	httpReq, err := http.NewRequest("POST", "/attach", bytes.NewBuffer(data))
	if err != nil {
		return nil, nil, err
	}
	addrs := c.service.Addrs()
	if len(addrs) == 0 {
		return nil, nil, ErrNoServers
	}
	conn, err := c.dial("tcp", addrs[0])
	if err != nil {
		return nil, nil, err
	}
	clientconn := httputil.NewClientConn(conn, nil)
	res, err := clientconn.Do(httpReq)
	if err != nil && err != httputil.ErrPersistEOF {
		return nil, nil, err
	}
	if res.StatusCode != 200 {
		return nil, nil, fmt.Errorf("cluster: unexpected status %d", res.StatusCode)
	}
	var rwc io.ReadWriteCloser
	var buf *bufio.Reader
	rwc, buf = clientconn.Hijack()
	if buf.Buffered() > 0 {
		rwc = struct {
			io.Reader
			writeCloser
		}{
			io.MultiReader(io.LimitReader(buf, int64(buf.Buffered())), rwc),
			rwc.(writeCloser),
		}
	}

	attachState := make([]byte, 1)
	if _, err := rwc.Read(attachState); err != nil {
		rwc.Close()
		return nil, nil, err
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
			return errors.New(string(errBytes))
		default:
			rwc.Close()
			return fmt.Errorf("cluster: unknown attach state: %d", attachState)
		}
	}

	if attachState[0] == host.AttachWaiting {
		if !wait {
			rwc.Close()
			return nil, nil, ErrWouldWait
		}
		return rwc.(ReadWriteCloser), func() error {
			if _, err := rwc.Read(attachState); err != nil {
				rwc.Close()
				return err
			}
			return handleState()
		}, nil
	}

	return rwc.(ReadWriteCloser), nil, handleState()
}
