package main

import "github.com/flynn/flynn/host/volume"

type Volume struct {
	*volume.Info

	AppID     string
	ReleaseID string
	JobType   string
	Path      string
	HostID    string
	JobID     string
}

func NewVolume(info *volume.Info, hostID string) *Volume {
	return &Volume{
		Info:      info,
		AppID:     info.Meta["flynn-controller.app"],
		ReleaseID: info.Meta["flynn-controller.release"],
		JobType:   info.Meta["flynn-controller.type"],
		Path:      info.Meta["flynn-controller.path"],
		HostID:    hostID,
	}
}

type VolumeEvent struct {
	Type   volume.EventType
	Volume *Volume
}
