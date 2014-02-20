package types

type ExpandedFormation struct {
	App       *App
	Release   *Release
	Artifact  *Artifact
	Processes map[string]int
}

type App struct {
	ID   string `json:"id,omitempty"`
	Name string `json:"name,omitempty"`
}

type Release struct {
	ID         string                 `json:"id,omitempty"`
	ArtifactID string                 `json:"artifact,omitempty"`
	Env        map[string]string      `json:"env,omitempty"`
	Processes  map[string]ProcessType `json:"processes,omitempty"`
}

type ProcessType struct {
	Cmd   []string          `json:"cmd,omitempty"`
	Env   map[string]string `json:"env,omitempty"`
	Ports ProcessPorts      `json:"ports,omitempty"`
}

type ProcessPorts struct {
	TCP int `json:"tcp,omitempty"`
	UDP int `json:"udp,omitempty"`
}

type Artifact struct {
	ID     string `json:"id,omitempty"`
	Type   string `json:"type,omitempty"`
	BaseID string `json:"base,omitempty"`
	URI    string `json:"uri,omitempty"`
}

type Formation struct {
	AppID     string         `json:"app,omitempty"`
	ReleaseID string         `json:"release,omitempty"`
	Processes map[string]int `json:"processes,omitempty"`
}

type Key struct {
	ID      string `json:"fingerprint,omitempty"`
	Comment string `json:"comment,omitempty"`
	Key     string `json:"key,omitempty"`
}

type Job struct {
	ID        string   `json:"id,omitempty"`
	Type      string   `json:"type,omitempty"`
	ReleaseID string   `json:"release,omitempty"`
	Cmd       []string `json:"cmd,omitempty"`
}
