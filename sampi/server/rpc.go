package main

import (
	"github.com/flynn/sampi/types"
)

type Scheduler struct {
	state State
}

func (s *Scheduler) State(arg struct{}, ret *map[string]types.Host) error {
	*ret = s.state.Get()
	return nil
}

func (s *Scheduler) Schedule(req *types.ScheduleReq, res *types.ScheduleRes) error {
	s.state.Begin()
	*res = types.ScheduleRes{}
	for host, jobs := range req.HostJobs {
		for _, job := range jobs {
			if !s.state.Add(host, job) {
				if req.Incremental {
					res.RemainingJobs = append(res.RemainingJobs, job)
				} else {
					res.State = s.state.Rollback()
					return nil
				}
			}
		}
	}
	res.Success = true
	res.State = s.state.Commit()
	return nil
}
