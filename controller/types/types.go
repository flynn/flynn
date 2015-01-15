package types

import (
	"encoding/json"
	"fmt"
	"time"
)

type ExpandedFormation struct {
	App       *App           `json:"app,omitempty"`
	Release   *Release       `json:"release,omitempty"`
	Artifact  *Artifact      `json:"artifact,omitempty"`
	Processes map[string]int `json:"processes,omitempty"`
	UpdatedAt time.Time      `json:"updated_at,omitempty"`
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
	Cmd         []string          `json:"cmd,omitempty"`
	Entrypoint  []string          `json:"entrypoint,omitempty"`
	Env         map[string]string `json:"env,omitempty"`
	Ports       []Port            `json:"ports,omitempty"`
	Data        bool              `json:"data,omitempty"`
	Omni        bool              `json:"omni,omitempty"` // omnipresent - present on all hosts
	HostNetwork bool              `json:"host_network,omitempty"`
}

type Port struct {
	Port     int    `json:"port"`
	Proto    string `json:"proto"`
	RangeEnd int    `json:"range_end"`
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
	ID        string            `json:"id,omitempty"`
	AppID     string            `json:"app,omitempty"`
	ReleaseID string            `json:"release,omitempty"`
	Type      string            `json:"type,omitempty"`
	State     string            `json:"state,omitempty"`
	Cmd       []string          `json:"cmd,omitempty"`
	Meta      map[string]string `json:"meta,omitempty"`
	CreatedAt *time.Time        `json:"created_at,omitempty"`
	UpdatedAt *time.Time        `json:"updated_at,omitempty"`
}

type JobEvent struct {
	Job
	ID    int64  `json:"id"`
	JobID string `json:"job_id,omitempty"`
}

type NewJob struct {
	ReleaseID  string            `json:"release,omitempty"`
	Cmd        []string          `json:"cmd,omitempty"`
	Entrypoint []string          `json:"entrypoint,omitempty"`
	Env        map[string]string `json:"env,omitempty"`
	Meta       map[string]string `json:"meta,omitempty"`
	TTY        bool              `json:"tty,omitempty"`
	Columns    int               `json:"tty_columns,omitempty"`
	Lines      int               `json:"tty_lines,omitempty"`
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

type ValidationError struct {
	Field   string `json:"field,omitempty"`
	Message string `json:"message"`
}

func (v ValidationError) Error() string {
	return fmt.Sprintf("validation error: %s %s", v.Field, v.Message)
}
