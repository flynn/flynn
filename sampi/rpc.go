package sampi

import (
	"io"

	"github.com/flynn/rpcplus"
	"github.com/flynn/sampi/types"
)

type Scheduler struct {
	state State
}

// Scheduler Methods

func (s *Scheduler) State(arg struct{}, ret *map[string]sampi.Host) error {
	*ret = s.state.Get()
	return nil
}

func (s *Scheduler) Schedule(req *sampi.ScheduleReq, res *sampi.ScheduleRes) error {
	s.state.Begin()
	*res = sampi.ScheduleRes{}
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

func (s *Scheduler) RegisterHost(hostID *string, host *sampi.Host, stream rpcplus.Stream) error {
	*hostID = host.ID
	s.state.Begin()
	// TODO: error if host.ID is duplicate or empty
	jobs := make(chan *sampi.Job)
	s.state.AddHost(host, jobs)
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
	s.state.RemoveHost(host.ID)
	s.state.Commit()
	if err == io.EOF {
		err = nil
	}
	return err
}

func (s *Scheduler) RemoveJobs(hostID *string, jobIDs []string, res *struct{}) error {
	s.state.Begin()
	s.state.RemoveJobs(*hostID, jobIDs...)
	s.state.Commit()
	return nil
}
