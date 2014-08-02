package docker

import (
	"time"
)

type APIContainers struct {
	ID         string `json:"Id"`
	Image      string
	Command    string
	Created    int64
	Status     string
	Ports      []APIPort
	SizeRw     int64
	SizeRootFs int64
	Names      []string
}

type APIPort struct {
	PrivatePort int64
	PublicPort  int64
	Type        string
	IP          string
}

type Container struct {
	ID string

	Created time.Time

	Path string
	Args []string

	Config *Config
	State  State
	Image  string

	NetworkSettings *NetworkSettings

	SysInitPath    string
	ResolvConfPath string
	HostnamePath   string
	HostsPath      string
	Name           string
	Driver         string

	Volumes   map[string]string
	VolumesRW map[string]bool
}

type PortMapping map[string]string

type NetworkSettings struct {
	IPAddress   string
	IPPrefixLen int
	Gateway     string
	Bridge      string
	PortMapping map[string]PortMapping // Deprecated
	Ports       map[string][]PortBinding
}

type State struct {
	Running   bool
	Pid       int
	ExitCode  int
	StartedAt time.Time
	Ghost     bool
}

type Config struct {
	Hostname        string
	Domainname      string
	User            string
	Memory          int64 // Memory limit (in bytes)
	MemorySwap      int64 // Total memory usage (memory + swap); set `-1' to disable swap
	CpuShares       int64 // CPU shares (relative weight vs. other containers)
	AttachStdin     bool
	AttachStdout    bool
	AttachStderr    bool
	PortSpecs       []string // Deprecated - Can be in the format of 8080/tcp
	ExposedPorts    map[string]struct{}
	Tty             bool // Attach standard streams to a tty, including stdin if it is not closed.
	OpenStdin       bool // Open stdin
	StdinOnce       bool // If true, close stdin after the 1 attached client disconnects.
	Env             []string
	Cmd             []string
	Dns             []string
	Image           string // Name of the image as it was passed by the operator (eg. could be symbolic)
	Volumes         map[string]struct{}
	VolumesFrom     string
	WorkingDir      string
	Entrypoint      []string
	NetworkDisabled bool
	Name            string `json:"-"`
}

type HostConfig struct {
	Binds           []string
	ContainerIDFile string
	LxcConf         []KeyValuePair
	Privileged      bool
	PortBindings    map[string][]PortBinding
	Links           []string
	PublishAllPorts bool
}

type PortBinding struct {
	HostIp   string
	HostPort string
}

type KeyValuePair struct {
	Key   string
	Value string
}

type Image struct {
	ID              string    `json:"id"`
	Parent          string    `json:"parent,omitempty"`
	Comment         string    `json:"comment,omitempty"`
	Created         time.Time `json:"created"`
	Container       string    `json:"container,omitempty"`
	ContainerConfig Config    `json:"container_config,omitempty"`
	DockerVersion   string    `json:"docker_version,omitempty"`
	Author          string    `json:"author,omitempty"`
	Config          *Config   `json:"config,omitempty"`
	Architecture    string    `json:"architecture,omitempty"`
	Size            int64
}

type APIVersion struct {
	Version   string
	GitCommit string `json:",omitempty"`
	GoVersion string `json:",omitempty"`
}

type APIInfo struct {
	Debug              bool
	Containers         int
	Images             int
	NFd                int    `json:",omitempty"`
	NGoroutines        int    `json:",omitempty"`
	MemoryLimit        bool   `json:",omitempty"`
	SwapLimit          bool   `json:",omitempty"`
	IPv4Forwarding     bool   `json:",omitempty"`
	LXCVersion         string `json:",omitempty"`
	NEventsListener    int    `json:",omitempty"`
	KernelVersion      string `json:",omitempty"`
	IndexServerAddress string `json:",omitempty"`
}

type APIImages struct {
	ID          string   `json:"Id"`
	RepoTags    []string `json:",omitempty"`
	Created     int64
	Size        int64
	VirtualSize int64
	ParentId    string `json:",omitempty"`
}

type EventErr struct {
	Code    int    `json:"code,omitempty"`
	Message string `json:"message,omitempty"`
}

type Event struct {
	Status   string    `json:"status,omitempty"`
	Progress string    `json:"progress,omitempty"`
	ID       string    `json:"id,omitempty"`
	From     string    `json:"from,omitempty"`
	Time     int64     `json:"time,omitempty"`
	Error    *EventErr `json:"errorDetail,omitempty"`
}
