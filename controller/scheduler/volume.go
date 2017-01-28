package main

import (
	"sync"

	ct "github.com/flynn/flynn/controller/types"
	"github.com/flynn/flynn/host/volume"
)

type Volume struct {
	ct.Volume

	// stateMtx protects the State field from concurrent access by both
	// the main scheduler loop and the StartJob goroutine which is
	// potentially creating the volume on the host
	stateMtx sync.RWMutex
}

func (v *Volume) GetState() ct.VolumeState {
	v.stateMtx.RLock()
	defer v.stateMtx.RUnlock()
	return v.State
}

func (v *Volume) SetState(state ct.VolumeState) {
	v.stateMtx.Lock()
	defer v.stateMtx.Unlock()
	v.State = state
}

func (v *Volume) Info() *volume.Info {
	return &volume.Info{
		ID:   v.ID,
		Type: v.Type,
		Meta: v.Meta,
	}
}

func NewVolume(info *volume.Info, state ct.VolumeState, hostID string) *Volume {
	return &Volume{
		Volume: ct.Volume{
			VolumeReq: ct.VolumeReq{
				Path:         info.Meta["flynn-controller.path"],
				DeleteOnStop: info.Meta["flynn-controller.delete_on_stop"] == "true",
			},
			ID:        info.ID,
			HostID:    hostID,
			Type:      info.Type,
			State:     state,
			AppID:     info.Meta["flynn-controller.app"],
			ReleaseID: info.Meta["flynn-controller.release"],
			JobType:   info.Meta["flynn-controller.type"],
			Meta:      info.Meta,
			CreatedAt: &info.CreatedAt,
		},
	}
}

type VolumeEvent struct {
	Type   VolumeEventType
	Volume *Volume
}

type VolumeEventType string

const (
	VolumeEventTypeCreate     VolumeEventType = "create"
	VolumeEventTypeDestroy    VolumeEventType = "destroy"
	VolumeEventTypeController VolumeEventType = "controller"
)
