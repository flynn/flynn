package host

import (
	"time"

	"github.com/flynn/flynn/host/resource"
)

type Job struct {
	ID string `json:"id,omitempty"`

	Metadata map[string]string `json:"metadata,omitempty"`

	Artifact  Artifact           `json:"artifact,omitempty"`
	Resources resource.Resources `json:"resources,omitempty"`

	Config ContainerConfig `json:"config,omitempty"`

	// If Resurrect is true, the host service will attempt to start the job when
	// starting after stopping (via crash or shutdown) with the job running.
	Resurrect bool `json:"resurrect,omitempty"`
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
	DisableLog  bool              `json:"disable_log,omitempty"`
}

// Apply 'y' to 'x', returning a new structure.  'y' trumps.
func (x ContainerConfig) Merge(y ContainerConfig) ContainerConfig {
	x.TTY = x.TTY || y.TTY
	x.Stdin = x.Stdin || y.Stdin
	x.Data = x.Data || y.Data
	if y.Entrypoint != nil {
		x.Entrypoint = y.Entrypoint
	}
	if y.Cmd != nil {
		x.Cmd = y.Cmd
	}
	env := make(map[string]string, len(x.Env)+len(y.Env))
	for k, v := range x.Env {
		env[k] = v
	}
	for k, v := range y.Env {
		env[k] = v
	}
	x.Env = env
	mounts := make([]Mount, 0, len(x.Mounts)+len(y.Mounts))
	mounts = append(mounts, x.Mounts...)
	mounts = append(mounts, y.Mounts...)
	x.Mounts = mounts
	volumes := make([]VolumeBinding, 0, len(x.Volumes)+len(y.Volumes))
	volumes = append(volumes, x.Volumes...)
	volumes = append(volumes, y.Volumes...)
	x.Volumes = volumes
	ports := make([]Port, 0, len(x.Ports)+len(y.Ports))
	ports = append(ports, x.Ports...)
	ports = append(ports, y.Ports...)
	x.Ports = ports
	if y.WorkingDir != "" {
		x.WorkingDir = y.WorkingDir
	}
	if y.Uid != 0 {
		x.Uid = y.Uid
	}
	x.HostNetwork = x.HostNetwork || y.HostNetwork
	return x
}

type Port struct {
	Port    int      `json:"port,omitempty"`
	Proto   string   `json:"proto,omitempty"`
	Service *Service `json:"service,omitempty"`
}

type Service struct {
	Name string `json:"name,omitempty"`
	// Create the service in service discovery
	Create bool         `json:"create,omitempty"`
	Check  *HealthCheck `json:"check,omitempty"`
}

type HealthCheck struct {
	// Type is one of tcp, http, https
	Type string `json:"type,omitempty"`
	// Interval is the time to wait between checks after the service has been
	// marked as up. It defaults to two seconds.
	Interval time.Duration `json:"interval,omitempty"`
	// Threshold is the number of consecutive checks of the same status before
	// a service will be marked as up or down after coming up for the first
	// time. It defaults to 2.
	Threshold int `json:"threshold,omitempty"`
	// If KillDown is true, the job will be killed if the service goes down (or
	// does not come up)
	KillDown bool `json:"kill_down,omitempty"`
	// StartTimeout is the maximum duration that a service can take to come up
	// for the first time if KillDown is true. It defaults to ten seconds.
	StartTimeout time.Duration `json:"start_timeout,omitempty"`

	// Extra optional config fields for http/https checks
	Path   string `json:"path,omitempty"`
	Host   string `json:"host,omitempty"`
	Match  string `json:"match,omitempty"`
	Status int    `json:"status.omitempty"`
}

type Mount struct {
	Location  string `json:"location,omitempty"`
	Target    string `json:"target,omitempty"`
	Writeable bool   `json:"writeable,omitempty"`
}

type VolumeBinding struct {
	// Target defines the filesystem path inside the container where the volume will be mounted.
	Target string `json:"target"`
	// VolumeID can be thought of as the source path if this were a simple bind-mount.  It is resolved by a VolumeManager.
	VolumeID  string `json:"volume"`
	Writeable bool   `json:"writeable"`
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
	AttachFlagInitLog
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
