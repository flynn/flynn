package main

import (
	"errors"
	"net/http"

	"github.com/flynn/lorne/types"
	"github.com/flynn/rpcplus"
	rpc "github.com/flynn/rpcplus/comborpc"
)

func server() {
	rpc.Register(&Host{})
	rpc.HandleHTTP()
	http.HandleFunc("/attach", attachHandler)
	http.ListenAndServe(":1113", nil)
}

type Host struct{}

const stopTimeout = 1

func (h *Host) JobList(arg struct{}, res *map[string]lorne.Job) error {
	*res = state.Get()
	return nil
}

func (h *Host) GetJob(id string, res *lorne.Job) error {
	job := state.GetJob(id)
	if job != nil {
		*res = *job
	}
	return nil
}

func (h *Host) StopJob(id string, res *struct{}) error {
	job := state.GetJob(id)
	if job == nil {
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
