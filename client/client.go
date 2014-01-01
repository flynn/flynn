package client

import (
	"errors"

	"github.com/flynn/go-discoverd"
	"github.com/flynn/rpcplus"
	"github.com/flynn/strowger/types"
)

func New() (*Client, error) {
	disc, err := discoverd.NewClient()
	if err != nil {
		return nil, err
	}
	services, err := disc.Services("flynn-strowger-rpc")
	if err != nil {
		return nil, err
	}
	if len(services) == 0 {
		return nil, errors.New("strowger: no servers found")
	}
	c, err := rpcplus.DialHTTP("tcp", services[0].Addr)
	return &Client{c}, err
}

type Client struct {
	c *rpcplus.Client
}

func (c *Client) AddFrontend(config *strowger.Config) error {
	return c.c.Call("Router.AddFrontend", config, &struct{}{})
}
