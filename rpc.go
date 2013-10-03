package main

import (
	"github.com/flynn/lorne/types"
	"github.com/flynn/rpcplus"
)

type Host struct{}

func (h *Host) JobList(arg struct{}, res *map[string]lorne.Job) error {
	*res = state.Get()
	return nil
}

func (h *Host) GetJob(id string, res *lorne.Job) error {
	*res = state.GetJob(id)
	return nil
}

func (h *Host) SignalJob(sig *lorne.JobSignal, res *struct{}) error {
	return nil
}

func (h *Host) Stream(arg struct{}, stream rpcplus.Stream) error {
	return nil
}
