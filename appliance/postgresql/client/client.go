package pgmanager

import (
	"fmt"
	"net"
	"net/http"
	"time"

	"github.com/flynn/flynn/appliance/postgresql/state"
	"github.com/flynn/flynn/pkg/httpclient"
)

type PostgresInfo struct {
	Config   *state.PgConfig `json:"config"`
	Running  bool            `json:"running"`
	XLog     string          `json:"xlog,omitempty"`
	Replicas []*Replica      `json:"replicas,omitempty"`
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
