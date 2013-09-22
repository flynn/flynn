package main

import (
	"github.com/flynn/rpcplus"
	"github.com/flynn/sampi/types"
)

type Scheduler struct {
	state State
}

// Scheduler Methods

func (s *Scheduler) State(arg struct{}, ret *map[string]types.Host) error {
	*ret = s.state.Get()
	return nil
}

func (s *Scheduler) Schedule(req *types.ScheduleReq, res *types.ScheduleRes) error {
	s.state.Begin()
	*res = types.ScheduleRes{}
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

func (s *Scheduler) RegisterHost(hostID *string, host *types.Host, send func(*types.Job) error) error {
	s.state.Begin()
	// TODO: error if host.ID is duplicate or empty
	jobs := make(chan *types.Job)
	s.state.AddHost(host, jobs)
	s.state.Commit()

	var stopping bool
	for job := range jobs {
		if err := send(job); err != nil {
			if !stopping {
				stopping = true
				// This needs to be done asynchronously so that we don't deadlock
				go func() {
					s.state.Begin()
					s.state.RemoveHost(host.ID)
					s.state.Commit()
					close(jobs)
				}()
			}
		}
	}
	return nil
}

func (s *Scheduler) RemoveJobs(hostID *string, jobIDs []string, res *struct{}) error {
	s.state.Begin()
	s.state.RemoveJobs(*hostID, jobIDs...)
	s.state.Commit()
	return nil
}

func init() {
	rpcplus.Register(&Scheduler{*NewState()})
}
