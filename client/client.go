package client

import (
	ct "github.com/flynn/flynn-controller/types"
	"github.com/flynn/rpcplus"
)

func New(addr string) (*Client, error) {
	c, err := rpcplus.DialHTTP("tcp", addr)
	return &Client{c}, err
}

type Client struct {
	client *rpcplus.Client
}

func (c *Client) StreamFormations(ch chan<- *ct.ExpandedFormation) *error {
	return &c.client.StreamGo("Controller.StreamFormations", struct{}{}, ch).Error
}
