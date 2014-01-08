package client

import (
	"bytes"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"io"
	"time"

	"github.com/flynn/go-discoverd"
	"github.com/flynn/rpcplus"
	"github.com/flynn/sampi/types"
)

func New() (*Client, error) {
	services, err := discoverd.NewServiceSet("flynn-sampi")
	if err != nil {
		return nil, err
	}
	select {
	case <-services.Watch(true, true):
	case <-time.After(time.Second):
		return nil, errors.New("sampi: no servers found")
	}
	c, err := rpcplus.DialHTTP("tcp", services.Leader().Addr)
	return &Client{c}, err
}

type Client struct {
	c *rpcplus.Client
}

func (c *Client) State() (map[string]sampi.Host, error) {
	var state map[string]sampi.Host
	err := c.c.Call("Scheduler.State", struct{}{}, &state)
	return state, err
}

func (c *Client) Schedule(req *sampi.ScheduleReq) (*sampi.ScheduleRes, error) {
	var res sampi.ScheduleRes
	err := c.c.Call("Scheduler.Schedule", req, &res)
	return &res, err
}

func (c *Client) RegisterHost(host *sampi.Host, stream chan *sampi.Job) *error {
	return &c.c.StreamGo("Scheduler.RegisterHost", host, stream).Error
}

func (c *Client) RemoveJobs(jobIDs []string) error {
	return c.c.Call("Scheduler.RemoveJobs", jobIDs, &struct{}{})
}

func RandomJobID(prefix string) string { return prefix + randomID() }

func randomID() string {
	b := make([]byte, 16)
	enc := make([]byte, 24)
	_, err := io.ReadFull(rand.Reader, b)
	if err != nil {
		panic(err) // This shouldn't ever happen, right?
	}
	base64.URLEncoding.Encode(enc, b)
	return string(bytes.TrimRight(enc, "="))
}
