package main

import (
	"errors"
	"net"
	"net/http"

	"github.com/flynn/flynn/host/types"
	"github.com/flynn/flynn/pkg/rpcplus"
	rpc "github.com/flynn/flynn/pkg/rpcplus/comborpc"
)

func serveHTTP(host *Host, attach *attachHandler, sh *shutdownHandler) error {
	if err := rpc.Register(host); err != nil {
		return err
	}
	rpc.Register(host)
	rpc.HandleHTTP()
	http.Handle("/attach", attach)

	l, err := net.Listen("tcp", ":1113")
	if err != nil {
		return err
	}
	sh.BeforeExit(func() { l.Close() })
	go http.Serve(l, nil)
	return nil
}

type Host struct {
	state   *State
	backend Backend
}

func (h *Host) ListJobs(arg struct{}, res *map[string]host.ActiveJob) error {
	*res = h.state.Get()
	return nil
}

func (h *Host) GetJob(id string, res *host.ActiveJob) error {
	job := h.state.GetJob(id)
	if job != nil {
		*res = *job
	}
	return nil
}

func (h *Host) StopJob(id string, res *struct{}) error {
	job := h.state.GetJob(id)
	if job == nil {
		return errors.New("host: unknown job")
	}
	if job.Status != host.StatusRunning {
		return errors.New("host: job is not running")
	}
	return h.backend.Stop(id)
}

func (h *Host) StreamEvents(id string, stream rpcplus.Stream) error {
	ch := make(chan host.Event)
	h.state.AddListener(id, ch)
	defer func() {
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
