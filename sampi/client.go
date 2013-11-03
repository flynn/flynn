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
	addrs := disc.Services("flynn-sampi").OnlineAddrs()
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
