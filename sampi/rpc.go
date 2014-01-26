package sampi

import (
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
		for _, job := range jobs {
			if s.state.AddJob(host, job) {
				if req.Incremental {
					s.state.SendJob(host, job)
				}
			} else {
				if req.Incremental {
					res.RemainingJobs = append(res.RemainingJobs, job)
				} else {
					res.State = s.state.Rollback()
					return nil
				}
			}
		}
	}
	if !req.Incremental {
		for host, jobs := range req.HostJobs {
			for _, job := range jobs {
				s.state.SendJob(host, job)
			}
		}
	}
	res.Success = true
	res.State = s.state.Commit()
	return nil
}

// Host Service methods

func (s *Cluster) ConnectHost(hostID *string, h *host.Host, stream rpcplus.Stream) error {
	*hostID = h.ID
	s.state.Begin()
	// TODO: error if host.ID is duplicate or empty
	jobs := make(chan *host.Job)
	s.state.AddHost(h, jobs)
	s.state.Commit()

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
