package main

import (
	"errors"

	"github.com/flynn/lorne/types"
	"github.com/flynn/rpcplus"
)

type Host struct{}

const stopTimeout = 10

func (h *Host) JobList(arg struct{}, res *map[string]lorne.Job) error {
	*res = state.Get()
	return nil
}

func (h *Host) GetJob(id string, res *lorne.Job) error {
	*res = state.GetJob(id)
	return nil
}

func (h *Host) StopJob(id string, res *struct{}) error {
	job := state.GetJob(id)
	if job.Job == nil {
		return errors.New("lorne: unknown job")
	}
	if job.Status != lorne.StatusRunning {
		return errors.New("lorne: job is not running")
	}
	return Docker.StopContainer(job.ContainerID, stopTimeout)
}

func (h *Host) Stream(id string, stream rpcplus.Stream) error {
	ch := make(chan lorne.Event)
	state.AddListener(id, ch)
	defer func() {
		go func() {
			// drain to prevent deadlock while removing the listener
			for _ = range ch {
			}
		}()
		state.RemoveListener(id, ch)
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
