package volume

import (
	"errors"
	"io"
	"time"
)

var ErrNoSuchVolume = errors.New("no such volume")

/*
	A Volume is a persistent and sharable filesystem.  Unlike most of the filesystem in a job's
	container, which is ephemeral and is discarded after job termination, Volumes can be used to
	store data and may be reconnected to a later job (or to multiple jobs).

	Volumes may also support additional features for their section of the filesystem, such
	storage quotas, read-only mounts, snapshotting operation, etc.

	The Flynn host service maintains a locally persistent knowledge
	of mounts, and supplies this passively to the orchestration API.
	The host service does *not* perform services such as garbage collection of unmounted
	volumes (how is it to know whether you still want that data preserved for a future job?)
	or transport and persistence of volumes between hosts (that should be orchestrated via
	the API from a higher level service).
*/
type Volume interface {
	Info() *Info

	Provider() Provider

	// Location returns the path to this volume's mount.  To use the volume in a job, bind mount this into the container's filesystem.
	Location() string

	IsSnapshot() bool
}

/*
	`volume.Info` names and describes info about a volume.
	It is a serializable structure intended for API use.
*/
type Info struct {
	ID        string            `json:"id"`
	Type      VolumeType        `json:"type"`
	Meta      map[string]string `json:"meta,omitempty"`
	CreatedAt time.Time         `json:"created_at"`
}

type VolumeType string

const (
	VolumeTypeData     VolumeType = "data"
	VolumeTypeSquashfs VolumeType = "squashfs"
	VolumeTypeExt2     VolumeType = "ext2"
)

var VolumeTypes = []VolumeType{
	VolumeTypeData,
	VolumeTypeSquashfs,
	VolumeTypeExt2,
}

type Event struct {
	Type   EventType `json:"type"`
	Volume *Info     `json:"volume"`
}

type EventType string

const (
	EventTypeCreate  EventType = "create"
	EventTypeDestroy EventType = "destroy"
)

type Filesystem struct {
	ID         string            `json:"id"`
	Data       io.Reader         `json:"-"`
	Size       int64             `json:"size"`
	Type       VolumeType        `json:"type"`
	MountFlags uintptr           `json:"flags"`
	Meta       map[string]string `json:"meta,omitempty"`
}

func (f *Filesystem) Info() *Info {
	return &Info{
		ID:   f.ID,
		Type: f.Type,
		Meta: f.Meta,
	}
}
