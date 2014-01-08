package main

import (
	"errors"
	"net/http"

	"github.com/flynn/rpcplus"
	rpc "github.com/flynn/rpcplus/comborpc"

	"./types"
)

func serveHTTP(host *Host, attach *attachHandler) {
	rpc.Register(host)
	rpc.HandleHTTP()
	http.Handle("/attach", attach)
	http.ListenAndServe(":1113", nil)
}

type dockerHostClient interface {
	StopContainer(string, uint) error
}

type Host struct {
	state  *State
	docker dockerHostClient
}

const stopTimeout = 1

func (h *Host) JobList(arg struct{}, res *map[string]lorne.Job) error {
	*res = h.state.Get()
	return nil
}

func (h *Host) GetJob(id string, res *lorne.Job) error {
	job := h.state.GetJob(id)
	if job != nil {
		*res = *job
	}
	return nil
}

func (h *Host) StopJob(id string, res *struct{}) error {
	job := h.state.GetJob(id)
	if job == nil {
		return errors.New("lorne: unknown job")
	}
	if job.Status != lorne.StatusRunning {
		return errors.New("lorne: job is not running")
	}
	return h.docker.StopContainer(job.ContainerID, stopTimeout)
}

func (h *Host) Stream(id string, stream rpcplus.Stream) error {
	ch := make(chan lorne.Event)
	h.state.AddListener(id, ch)
	defer func() {
		go func() {
			// drain to prevent deadlock while removing the listener
			for _ = range ch {
			}
		}()
		h.state.RemoveListener(id, ch)
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
