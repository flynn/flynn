package client

import (
	"errors"

	"github.com/flynn/go-discover/discover"
	"github.com/flynn/rpcplus"
	"github.com/flynn/sampi/types"
)

func New() (*Client, error) {
	disc, err := discover.NewClient()
	if err != nil {
		return nil, err
	}
	services, err := disc.Services("flynn-sampi")
	if err != nil {
		return nil, err
	}
	addrs := services.OnlineAddrs()
	if len(addrs) == 0 {
		return nil, errors.New("sampi: no servers found")
	}
	c, err := rpcplus.DialHTTP("tcp", addrs[0])
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
