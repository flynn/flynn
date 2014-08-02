package main

import (
	"github.com/flynn/flynn-host/sampi"
	"github.com/flynn/flynn-host/types"
	"github.com/flynn/go-flynn/cluster"
	"github.com/flynn/rpcplus"
)

func NewLocalClient(host string, c *sampi.Cluster) cluster.LocalClient {
	return &localClient{c: c, host: host}
}

type localClient struct {
	c    *sampi.Cluster
	host string
}

func (c *localClient) ListHosts() (map[string]host.Host, error) {
	res := make(map[string]host.Host)
	return res, c.c.ListHosts(struct{}{}, &res)
}

func (c *localClient) AddJobs(req *host.AddJobsReq) (*host.AddJobsRes, error) {
	res := &host.AddJobsRes{}
	return res, c.c.AddJobs(req, res)
}

func (c *localClient) RegisterHost(h *host.Host, jobs chan *host.Job) *error {
	ch := make(chan interface{})
	stream := rpcplus.Stream{Send: ch}
	var err error
	go func() {
		err = c.c.RegisterHost(&c.host, h, stream)
		close(ch)
	}()
	go func() {
		for job := range ch {
			jobs <- job.(*host.Job)
		}
		close(jobs)
	}()
	return &err
}

func (c *localClient) RemoveJobs(jobs []string) error {
	return c.c.RemoveJobs(&c.host, jobs, nil)
}
