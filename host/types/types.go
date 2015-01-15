package host

import (
	"time"
)

type Job struct {
	ID string `json:"id,omitempty"`

	Metadata map[string]string `json:"metadata,omitempty"`

	Artifact  Artifact     `json:"artifact,omitempty"`
	Resources JobResources `json:"resources,omitempty"`

	Config ContainerConfig `json:"config,omitempty"`
}

func (j *Job) Dup() *Job {
	job := *j

	dupMap := func(m map[string]string) (res map[string]string) {
		if m != nil {
			res = make(map[string]string, len(m))
		}
		for k, v := range m {
			res[k] = v
		}
		return res
	}
	dupSlice := func(s []string) (res []string) {
		if s != nil {
			res = make([]string, len(s))
		}
		for i, v := range s {
			res[i] = v
		}
		return res
	}
	job.Metadata = dupMap(j.Metadata)
	job.Config.Entrypoint = dupSlice(j.Config.Entrypoint)
	job.Config.Cmd = dupSlice(j.Config.Cmd)
	job.Config.Env = dupMap(j.Config.Env)
	if j.Config.Ports != nil {
		job.Config.Ports = make([]Port, len(j.Config.Ports))
		for i, p := range j.Config.Ports {
			job.Config.Ports[i] = p
		}
	}
	if j.Config.Mounts != nil {
		job.Config.Mounts = make([]Mount, len(j.Config.Mounts))
		for i, m := range j.Config.Mounts {
			job.Config.Mounts[i] = m
		}
	}

	return &job
}

type JobResources struct {
	Memory int `json:"memory,omitempty"` // in KiB
}

type ContainerConfig struct {
	TTY         bool              `json:"tty,omitempty"`
	Stdin       bool              `json:"stdin,omitempty"`
	Data        bool              `json:"data,omitempty"`
	Entrypoint  []string          `json:"entry_point,omitempty"`
	Cmd         []string          `json:"cmd,omitempty"`
	Env         map[string]string `json:"env,omitempty"`
	Mounts      []Mount           `json:"mounts,omitempty"`
	Volumes     []VolumeBinding   `json:"volumes,omitempty"`
	Ports       []Port            `json:"ports,omitempty"`
	WorkingDir  string            `json:"working_dir,omitempty"`
	Uid         int               `json:"uid,omitempty"`
	HostNetwork bool              `json:"host_network,omitempty"`
}

type Port struct {
	Port     int    `json:"port,omitempty"`
	Proto    string `json:"proto,omitempty"`
	RangeEnd int    `json:"range_end,omitempty"`
}

type Mount struct {
	Location  string `json:"location,omitempty"`
	Target    string `json:"target,omitempty"`
	Writeable bool   `json:"writeable,omitempty"`
}

type VolumeBinding struct {
	// Target defines the filesystem path inside the container where the volume will be mounted.
	Target string
	// VolumeID can be thought of as the source path if this were a simple bind-mount.  It is resolved by a VolumeManager.
	VolumeID  string
	Writeable bool
}

type Artifact struct {
	URI  string `json:"url,omitempty"`
	Type string `json:"type,omitempty"`
}

type Host struct {
	ID string `json:"id,omitempty"`

	Jobs     []*Job            `json:"jobs,omitempty"`
	Metadata map[string]string `json:"metadata,omitempty"`
}

type Event struct {
	Event string     `json:"event,omitempty"`
	JobID string     `json:"job_id,omitempty"`
	Job   *ActiveJob `json:"job,omitempty"`
}

type HostEvent struct {
	Event  string `json:"event,omitempty"`
	HostID string `json:"host_id,omitempty"`
}

type ActiveJob struct {
	Job         *Job      `json:"job,omitempty"`
	HostID      string    `json:"host_id,omitempty"`
	ContainerID string    `json:"container_id,omitempty"`
	InternalIP  string    `json:"internal_ip,omitempty"`
	ForceStop   bool      `json:"force_stop,omitempty"`
	Status      JobStatus `json:"status,omitempty"`
	StartedAt   time.Time `json:"started_at,omitempty"`
	EndedAt     time.Time `json:"ended_at,omitempty"`
	ExitStatus  int       `json:"exit_status,omitempty"`
	Error       *string   `json:"error,omitempty"`
	ManifestID  string    `json:"manifest_id,omitempty"`
}

type AttachReq struct {
	JobID  string     `json:"job_id,omitempty"`
	Flags  AttachFlag `json:"flags,omitempty"`
	Height uint16     `json:"height,omitempty"`
	Width  uint16     `json:"width,omitempty"`
}

type AttachFlag uint8

const (
	AttachFlagStdout AttachFlag = 1 << iota
	AttachFlagStderr
	AttachFlagStdin
	AttachFlagLogs
	AttachFlagStream
)

type JobStatus uint8

func (s JobStatus) String() string {
	return map[JobStatus]string{
		StatusStarting: "starting",
		StatusRunning:  "running",
		StatusDone:     "done",
		StatusCrashed:  "crashed",
		StatusFailed:   "failed",
	}[s]
}

const (
	StatusStarting JobStatus = iota
	StatusRunning
	StatusDone
	StatusCrashed
	StatusFailed
)

const (
	AttachSuccess byte = iota
	AttachWaiting
	AttachError
	AttachData
	AttachSignal
	AttachExit
	AttachResize
)
