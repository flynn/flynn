package types

import (
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/flynn/flynn/host/resource"
	"github.com/flynn/flynn/host/types"
	"github.com/flynn/flynn/router/types"
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
	Meta      map[string]string `json:"meta,omitempty"`
	Strategy  string            `json:"strategy,omitempty"`
	ReleaseID string            `json:"release_id,omitempty"`
	CreatedAt *time.Time        `json:"created_at,omitempty"`
	UpdatedAt *time.Time        `json:"updated_at,omitempty"`
}

func (a *App) System() bool {
	v, ok := a.Meta["flynn-system-app"]
	return ok && v == "true"
}

type Release struct {
	ID         string                 `json:"id,omitempty"`
	ArtifactID string                 `json:"artifact,omitempty"`
	Env        map[string]string      `json:"env,omitempty"`
	Processes  map[string]ProcessType `json:"processes,omitempty"`
	CreatedAt  *time.Time             `json:"created_at,omitempty"`
}

type ProcessType struct {
	Cmd         []string           `json:"cmd,omitempty"`
	Entrypoint  []string           `json:"entrypoint,omitempty"`
	Env         map[string]string  `json:"env,omitempty"`
	Ports       []Port             `json:"ports,omitempty"`
	Data        bool               `json:"data,omitempty"`
	Omni        bool               `json:"omni,omitempty"` // omnipresent - present on all hosts
	HostNetwork bool               `json:"host_network,omitempty"`
	Service     string             `json:"service,omitempty"`
	Resurrect   bool               `json:"resurrect,omitempty"`
	Resources   resource.Resources `json:"resources,omitempty"`
}

type Port struct {
	Port    int           `json:"port"`
	Proto   string        `json:"proto"`
	Service *host.Service `json:"service,omitempty"`
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
	JobID     string `json:"job_id,omitempty"`
	AppID     string `json:"app,omitempty"`
	ReleaseID string `json:"release,omitempty"`
	Type      string `json:"type,omitempty"`
	State     string `json:"state,omitempty"`
}

func (e *JobEvent) IsDown() bool {
	return e.State == "failed" || e.State == "crashed" || e.State == "down"
}

type JobEvents map[string]map[string]int

type NewJob struct {
	ReleaseID  string             `json:"release,omitempty"`
	ReleaseEnv bool               `json:"release_env,omitempty"`
	Cmd        []string           `json:"cmd,omitempty"`
	Entrypoint []string           `json:"entrypoint,omitempty"`
	Env        map[string]string  `json:"env,omitempty"`
	Meta       map[string]string  `json:"meta,omitempty"`
	TTY        bool               `json:"tty,omitempty"`
	Columns    int                `json:"tty_columns,omitempty"`
	Lines      int                `json:"tty_lines,omitempty"`
	DisableLog bool               `json:"disable_log,omitempty"`
	Resources  resource.Resources `json:"resources,omitempty"`
}

type Deployment struct {
	ID           string         `json:"id,omitempty"`
	AppID        string         `json:"app,omitempty"`
	OldReleaseID string         `json:"old_release,omitempty"`
	NewReleaseID string         `json:"new_release,omitempty"`
	Strategy     string         `json:"strategy,omitempty"`
	Processes    map[string]int `json:"processes,omitempty"`
	CreatedAt    *time.Time     `json:"created_at,omitempty"`
	FinishedAt   *time.Time     `json:"finished_at,omitempty"`
}

type DeployID struct {
	ID string
}

type DeploymentEvent struct {
	AppID        string `json:"app,omitempty"`
	DeploymentID string `json:"deployment,omitempty"`
	ReleaseID    string `json:"release,omitempty"`
	Status       string `json:"status,omitempty"`
	JobType      string `json:"job_type,omitempty"`
	JobState     string `json:"job_state,omitempty"`
	Error        string `json:"error,omitempty"`
}

func (e *DeploymentEvent) Err() error {
	if e.Error == "" {
		return nil
	}
	return errors.New(e.Error)
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
	ProviderID string            `json:"provider,omitempty"`
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

type LogOpts struct {
	Follow      bool
	JobID       string
	Lines       *int
	ProcessType *string
}

type EventType string

const (
	EventTypeApp              EventType = "app"
	EventTypeAppDeletion      EventType = "app_deletion"
	EventTypeDeployment       EventType = "deployment"
	EventTypeJob              EventType = "job"
	EventTypeScale            EventType = "scale"
	EventTypeRelease          EventType = "release"
	EventTypeArtifact         EventType = "artifact"
	EventTypeProvider         EventType = "provider"
	EventTypeResource         EventType = "resource"
	EventTypeResourceDeletion EventType = "resource_deletion"
	EventTypeKey              EventType = "key"
	EventTypeKeyDeletion      EventType = "key_deletion"
	EventTypeRoute            EventType = "route"
	EventTypeRouteDeletion    EventType = "route_deletion"
)

type Event struct {
	ID         int64           `json:"id,omitempty"`
	AppID      string          `json:"app,omitempty"`
	ObjectType EventType       `json:"object_type,omitempty"`
	ObjectID   string          `json:"object_id,omitempty"`
	UniqueID   string          `json:"-"`
	Data       json.RawMessage `json:"data,omitempty"`
	CreatedAt  *time.Time      `json:"created_at,omitempty"`
}

type AppDeletion struct {
	AppID            string          `json:"app"`
	DeletedRoutes    []*router.Route `json:"deleted_routes"`
	DeletedResources []*Resource     `json:"deleted_resources"`
}

type AppDeletionEvent struct {
	AppDeletion *AppDeletion `json:"app_deletion"`
	Error       string       `json:"error"`
}
