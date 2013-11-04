package client

import (
	"errors"

	"github.com/flynn/go-discover/discover"
	"github.com/flynn/lorne/types"
	"github.com/flynn/rpcplus"
)

func New(id string) (*Client, error) {
	disc, err := discover.NewClient()
	if err != nil {
		return nil, err
	}
	services, err := disc.Services("flynn-lorne-rpc." + id)
	if err != nil {
		return nil, err
	}
	addrs := services.OnlineAddrs()
	if len(addrs) == 0 {
		return nil, errors.New("lorne: no servers found")
	}
	c, err := rpcplus.DialHTTP("tcp", addrs[0])
	return &Client{c}, err
}

type Client struct {
	c *rpcplus.Client
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
