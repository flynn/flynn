package types

import (
	"encoding/json"
	"time"
)

type ExpandedFormation struct {
	App       *App           `json:"app,omitempty"`
	Release   *Release       `json:"release,omitempty"`
	Artifact  *Artifact      `json:"artifact,omitempty"`
	Processes map[string]int `json:"processes,omitempty"`
}

type App struct {
	ID        string            `json:"id,omitempty"`
	Name      string            `json:"name,omitempty"`
	Protected bool              `json:"protected"`
	Meta      map[string]string `json:"meta,omitempty"`
	CreatedAt *time.Time        `json:"created_at,omitempty"`
	UpdatedAt *time.Time        `json:"updated_at,omitempty"`
}

type Release struct {
	ID         string                 `json:"id,omitempty"`
	ArtifactID string                 `json:"artifact,omitempty"`
	Env        map[string]string      `json:"env,omitempty"`
	Processes  map[string]ProcessType `json:"processes,omitempty"`
	CreatedAt  *time.Time             `json:"created_at,omitempty"`
}

type ProcessType struct {
	Cmd   []string          `json:"cmd,omitempty"`
	Env   map[string]string `json:"env,omitempty"`
	Ports ProcessPorts      `json:"ports,omitempty"`
	Data  bool              `json:"data,omitempty"`
}

type ProcessPorts struct {
	TCP int `json:"tcp,omitempty"`
	UDP int `json:"udp,omitempty"`
}

type Artifact struct {
	ID        string     `json:"id,omitempty"`
	Type      string     `json:"type,omitempty"`
	URI       string     `json:"uri,omitempty"`
	CreatedAt *time.Time `json:"created_at,omitempty"`
}

type Formation struct {
	AppID     string         `json:"app,omitempty"`
	ReleaseID string         `json:"release,omitempty"`
	Processes map[string]int `json:"processes,omitempty"`
	CreatedAt *time.Time     `json:"created_at,omitempty"`
	UpdatedAt *time.Time     `json:"updated_at,omitempty"`
}

type Key struct {
	ID        string     `json:"fingerprint,omitempty"`
	Key       string     `json:"key,omitempty"`
	Comment   string     `json:"comment,omitempty"`
	CreatedAt *time.Time `json:"created_at,omitempty"`
}

type Job struct {
	ID        string   `json:"id,omitempty"`
	Type      string   `json:"type,omitempty"`
	ReleaseID string   `json:"release,omitempty"`
	Cmd       []string `json:"cmd,omitempty"`
}

type NewJob struct {
	ReleaseID string            `json:"release,omitempty"`
	Cmd       []string          `json:"cmd,omitempty"`
	Env       map[string]string `json:"env,omitempty"`
	TTY       bool              `json:"tty,omitempty"`
	Columns   int               `json:"tty_columns,omitempty"`
	Lines     int               `json:"tty_lines,omitempty"`
}

type Frontend struct {
	Type       string `json:"type,omitempty"`
	HTTPDomain string `json:"http_domain,omitempty"`
	Service    string `json:"service,omitempty"`
}

type Provider struct {
	ID        string     `json:"id,omitempty"`
	URL       string     `json:"url,omitempty"`
	Name      string     `json:"name,omitempty"`
	CreatedAt *time.Time `json:"created_at,omitempty"`
	UpdatedAt *time.Time `json:"updated_at,omitempty"`
}

type Resource struct {
	ID         string            `json:"id,omitempty"`
	ProviderID string            `json:"provider_id,omitempty"`
	ExternalID string            `json:"external_id,omitempty"`
	Env        map[string]string `json:"env,omitempty"`
	Apps       []string          `json:"apps,omitempty"`
	CreatedAt  *time.Time        `json:"created_at,omitempty"`
}

type ResourceReq struct {
	ProviderID string           `json:"-"`
	Apps       []string         `json:"apps,omitempty"`
	Config     *json.RawMessage `json:"config"`
}
