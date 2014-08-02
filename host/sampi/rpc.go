package sampi

import (
	"errors"
	"io"

	"github.com/flynn/flynn-host/types"
	"github.com/flynn/rpcplus"
)

type Cluster struct {
	state *State
}

func NewCluster(state *State) *Cluster {
	return &Cluster{state}
}

// Scheduler Methods

func (s *Cluster) ListHosts(arg struct{}, ret *map[string]host.Host) error {
	*ret = s.state.Get()
	return nil
}

func (s *Cluster) AddJobs(req *host.AddJobsReq, res *host.AddJobsRes) error {
	s.state.Begin()
	*res = host.AddJobsRes{}
	for host, jobs := range req.HostJobs {
		if err := s.state.AddJobs(host, jobs); err != nil {
			s.state.Rollback()
			return err
		}
	}
	res.State = s.state.Commit()

	for host, jobs := range req.HostJobs {
		for _, job := range jobs {
			s.state.SendJob(host, job)
		}
	}

	return nil
}

// Host Service methods

func (s *Cluster) RegisterHost(hostID *string, h *host.Host, stream rpcplus.Stream) error {
	*hostID = h.ID
	if *hostID == "" {
		return errors.New("sampi: host id must not be blank")
	}

	s.state.Begin()

	if s.state.HostExists(*hostID) {
		s.state.Rollback()
		return errors.New("sampi: host exists")
	}

	jobs := make(chan *host.Job)
	s.state.AddHost(h, jobs)
	s.state.Commit()
	go s.state.sendEvent(h.ID, "add")

	var err error
outer:
	for {
		select {
		case job := <-jobs:
			// make sure we don't deadlock if there is an error while we're sending
			select {
			case stream.Send <- job:
			case err = <-stream.Error:
				break outer
			}
		case err = <-stream.Error:
			break outer
		}
	}

	s.state.Begin()
	s.state.RemoveHost(h.ID)
	s.state.Commit()
	go s.state.sendEvent(h.ID, "remove")
	if err == io.EOF {
		err = nil
	}
	return err
}

func (s *Cluster) RemoveJobs(hostID *string, jobIDs []string, res *struct{}) error {
	s.state.Begin()
	s.state.RemoveJobs(*hostID, jobIDs...)
	s.state.Commit()
	return nil
}

func (s *Cluster) StreamHostEvents(arg struct{}, stream rpcplus.Stream) error {
	ch := make(chan host.HostEvent)
	s.state.AddListener(ch)
	defer func() {
		go func() {
			// drain to prevent deadlock while removing the listener
			for _ = range ch {
			}
		}()
		s.state.RemoveListener(ch)
		close(ch)
	}()
	for {
		select {
		case event := <-ch:
			select {
			case stream.Send <- event:
			case <-stream.Error:
				return nil
			}
		case <-stream.Error:
			return nil
		}
	}
}
