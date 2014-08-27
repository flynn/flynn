package main

import (
	"github.com/flynn/flynn/host/sampi"
	"github.com/flynn/flynn/host/types"
	"github.com/flynn/flynn/pkg/cluster"
	"github.com/flynn/flynn/pkg/rpcplus"
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

type localStream struct {
	stream rpcplus.Stream
	err    error
}

func (s localStream) Close() error {
	close(s.stream.Error)
	return nil
}

func (s localStream) Err() error {
	return s.err
}

func (c *localClient) RegisterHost(h *host.Host, jobs chan *host.Job) cluster.Stream {
	ch := make(chan interface{})
	err := make(chan error)
	s := localStream{stream: rpcplus.Stream{Send: ch, Error: err}}
	go func() {
		s.err = c.c.RegisterHost(&c.host, h, s.stream)
		close(ch)
	}()
	go func() {
		for job := range ch {
			jobs <- job.(*host.Job).Dup()
		}
		close(jobs)
	}()
	return s
}

func (c *localClient) RemoveJobs(jobs []string) error {
	return c.c.RemoveJobs(&c.host, jobs, nil)
}
