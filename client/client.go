package client

import (
	"errors"

	"github.com/flynn/go-discoverd"
	"github.com/flynn/rpcplus"
	"github.com/flynn/strowger/types"
)

func New() (*Client, error) {
	services, err := discoverd.Services("flynn-strowger-rpc", discoverd.DefaultTimeout)
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

func (c *Client) Close() error {
	return c.c.Close()
}
