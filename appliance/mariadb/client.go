package mariadb

import (
	"errors"
	"fmt"
	"net"
	"net/http"
	"time"

	"github.com/flynn/flynn/discoverd/client"
	"github.com/flynn/flynn/pkg/httpclient"
)

var ErrTimeout = errors.New("timeout waiting for expected status")

type Client struct {
	c *httpclient.Client
}

func NewClient(addr string) *Client {
	// Remove port, if any
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
		return status.Process.SyncedDownstream != nil && status.Process.SyncedDownstream.ID == downstream.ID
	}, timeout)
}

func (c *Client) waitFor(fn func(*Status) bool, timeout time.Duration) error {
	timer := time.NewTimer(timeout)
	defer timer.Stop()

	ticker := time.NewTimer(100 * time.Millisecond)
	defer ticker.Stop()

	for {
		// Retrieve status.
		status, err := c.Status()
		if err != nil {
			return err
		} else if fn(status) {
			return nil
		}

		// Wait for next tick or timeout.
		select {
		case <-timer.C:
			return ErrTimeout
		case <-ticker.C:
		}
	}
}

type Status struct {
	// Peer    *PeerInfo    `json:"peer"`
	Process *ProcessInfo `json:"process"`
}
