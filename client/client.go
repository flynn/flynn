package client

import (
	"github.com/flynn/flynn-host/types"
	"github.com/flynn/go-flynn/cluster"
)

type Client struct {
	*cluster.Client
}

func New() (*Client, error) {
	c, err := cluster.NewClient()
	return &Client{c}, err
}

func (c *Client) ConnectHost(host *host.Host, jobs chan *host.Job) *error {
	client, err := c.RPCClient()
	if err != nil {
		return &err
	}
	return &client.StreamGo("Cluster.ConnectHost", host, jobs).Error
}

func (c *Client) RemoveJobs(jobIDs []string) error {
	client, err := c.RPCClient()
	if err != nil {
		return err
	}
	return client.Call("Cluster.RemoveJobs", jobIDs, &struct{}{})
}
