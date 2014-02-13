package types

type ExpandedFormation struct {
	App       *App
	Release   *Release
	Processes map[string]int
}

type App struct {
	ID   string `json:"id,omitempty"`
	Name string `json:"name,omitempty"`
}

type Release struct {
	ID          string                 `json:"id,omitempty"`
	ArtifactID  string                 `json:"artifact,omitempty"`
	Environment map[string]string      `json:"environment,omitempty"`
	Processes   map[string]ProcessType `json:"processes,omitempty"`
}

type ProcessType struct {
	Cmd   []string     `json:"cmd,omitempty"`
	Ports ProcessPorts `json:"ports,omitempty"`
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
