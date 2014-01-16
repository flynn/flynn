package client

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"net"
	"net/http"
	"net/http/httputil"

	"github.com/flynn/go-discoverd"
	"github.com/flynn/lorne/types"
	"github.com/flynn/rpcplus"
)

var ErrNoServers = errors.New("lorne: no servers found")

func New(id string) (*Client, error) {
	services, err := discoverd.NewServiceSet("flynn-lorne")
	services.Filter(map[string]string{"id": id})
	if err != nil {
		return nil, err
	}
	addrs := services.Addrs()
	if len(addrs) == 0 {
		return nil, ErrNoServers
	}
	c, err := rpcplus.DialHTTP("tcp", addrs[0])
	return &Client{c, services}, err
}

type Client struct {
	c *rpcplus.Client

	service discoverd.ServiceSet
}

func (c *Client) JobList() (map[string]lorne.Job, error) {
	var jobs map[string]lorne.Job
	err := c.c.Call("Host.JobList", struct{}{}, &jobs)
	return jobs, err
}

func (c *Client) GetJob(id string) (*lorne.Job, error) {
	var res lorne.Job
	err := c.c.Call("Host.GetJob", id, &res)
	return &res, err
}

func (c *Client) StopJob(id string) error {
	return c.c.Call("Host.StopJob", id, &struct{}{})
}

var ErrWouldWait = errors.New("lorne: attach would wait")

func (c *Client) Attach(req *lorne.AttachReq, wait bool) (ReadWriteCloser, func() error, error) {
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
	conn, err := net.Dial("tcp", addrs[0])
	if err != nil {
		return nil, nil, err
	}
	clientconn := httputil.NewClientConn(conn, nil)
	res, err := clientconn.Do(httpReq)
	if err != nil && err != httputil.ErrPersistEOF {
		return nil, nil, err
	}
	if res.StatusCode != 200 {
		return nil, nil, fmt.Errorf("lorne: unexpected status %d", res.StatusCode)
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
		case lorne.AttachSuccess:
			return nil
		case lorne.AttachError:
			errBytes, err := ioutil.ReadAll(rwc)
			rwc.Close()
			if err != nil {
				return err
			}
			return errors.New(string(errBytes))
		default:
			rwc.Close()
			return fmt.Errorf("lorne: unknown attach state: %d", attachState)
		}
	}

	if attachState[0] == lorne.AttachWaiting {
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

type ReadWriteCloser interface {
	io.ReadWriteCloser
	CloseWrite() error
}

type writeCloser interface {
	io.WriteCloser
	CloseWrite() error
}
