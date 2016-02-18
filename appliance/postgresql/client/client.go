package pgmanager

import (
	"errors"
	"fmt"
	"net"
	"net/http"
	"time"

	"github.com/flynn/flynn/discoverd/client"
	"github.com/flynn/flynn/pkg/httpclient"
	"github.com/flynn/flynn/pkg/sirenia/state"
)

type PostgresInfo struct {
	Config           *state.Config       `json:"config"`
	Running          bool                `json:"running"`
	SyncedDownstream *discoverd.Instance `json:"synced_downstream"`
	XLog             string              `json:"xlog,omitempty"`
	UserExists       bool                `json:"user_exists,omitempty"`
	Replicas         []*Replica          `json:"replicas,omitempty"`
	ReadWrite        bool                `json:"read_write,omitempty"`
}

type Replica struct {
	ID             string    `json:"id"`
	Addr           string    `json:"addr"`
	Start          time.Time `json:"start"`
	State          string    `json:"state"`
	Sync           bool      `json:"sync"`
	SentLocation   string    `json:"sent_location"`
	WriteLocation  string    `json:"write_location"`
	FlushLocation  string    `json:"flush_location"`
	ReplayLocation string    `json:"replay_location"`
}

type Status struct {
	Peer     *state.PeerInfo `json:"peer"`
	Postgres *PostgresInfo   `json:"postgres"`
}

type Client struct {
	c *httpclient.Client
}

func NewClient(addr string) *Client {
	// remove port, if any
	host, _, _ := net.SplitHostPort(addr)
	if host == "" {
		host = addr
	}

	return &Client{
		c: &httpclient.Client{
			URL:  fmt.Sprintf("http://%s:5433", host),
			HTTP: http.DefaultClient,
		},
	}
}

func (c *Client) Status() (*Status, error) {
	res := &Status{}
	return res, c.c.Get("/status", res)
}

func (c *Client) Stop() error {
	return c.c.Post("/stop", nil, nil)
}

func (c *Client) WaitForReplSync(downstream *discoverd.Instance, timeout time.Duration) error {
	return c.waitFor(func(status *Status) bool {
		return status.Postgres.SyncedDownstream != nil && status.Postgres.SyncedDownstream.ID == downstream.ID
	}, timeout)
}

func (c *Client) WaitForReadWrite(timeout time.Duration) error {
	return c.waitFor(func(status *Status) bool {
		return status.Postgres.ReadWrite
	}, timeout)
}

var ErrTimeout = errors.New("timeout waiting for expected status")

func (c *Client) waitFor(expected func(*Status) bool, timeout time.Duration) error {
	start := time.Now()
	for {
		status, err := c.Status()
		if err != nil {
			return err
		} else if expected(status) {
			return nil
		}
		time.Sleep(100 * time.Millisecond)
		if time.Now().Sub(start) > timeout {
			return ErrTimeout
		}
	}
}
