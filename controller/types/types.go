package types

import (
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/flynn/flynn/host/resource"
	"github.com/flynn/flynn/host/types"
	"github.com/flynn/flynn/pkg/tlscert"
	"github.com/flynn/flynn/router/types"
)

const RouteParentRefPrefix = "controller/apps/"

type ExpandedFormation struct {
	App           *App                         `json:"app,omitempty"`
	Release       *Release                     `json:"release,omitempty"`
	ImageArtifact *Artifact                    `json:"artifact,omitempty"`
	FileArtifacts []*Artifact                  `json:"file_artifacts,omitempty"`
	Processes     map[string]int               `json:"processes,omitempty"`
	Tags          map[string]map[string]string `json:"tags,omitempty"`
	UpdatedAt     time.Time                    `json:"updated_at,omitempty"`
}

type App struct {
	ID            string            `json:"id,omitempty"`
	Name          string            `json:"name,omitempty"`
	Meta          map[string]string `json:"meta"`
	Strategy      string            `json:"strategy,omitempty"`
	ReleaseID     string            `json:"release,omitempty"`
	DeployTimeout int32             `json:"deploy_timeout,omitempty"`
	CreatedAt     *time.Time        `json:"created_at,omitempty"`
	UpdatedAt     *time.Time        `json:"updated_at,omitempty"`
}

func (a *App) System() bool {
	v, ok := a.Meta["flynn-system-app"]
	return ok && v == "true"
}

// Critical apps cannot be completely scaled down by the scheduler
func (a *App) Critical() bool {
	v, ok := a.Meta["flynn-system-critical"]
	return ok && v == "true"
}

type Release struct {
	ID          string                 `json:"id,omitempty"`
	ArtifactIDs []string               `json:"artifacts,omitempty"`
	Env         map[string]string      `json:"env,omitempty"`
	Meta        map[string]string      `json:"meta,omitempty"`
	Processes   map[string]ProcessType `json:"processes,omitempty"`
	CreatedAt   *time.Time             `json:"created_at,omitempty"`

	// LegacyArtifactID is to support old clients which expect releases
	// to have a single ArtifactID
	LegacyArtifactID string `json:"artifact,omitempty"`
}

func (r *Release) ImageArtifactID() string {
	if len(r.ArtifactIDs) > 0 {
		return r.ArtifactIDs[0]
	}
	return ""
}

func (r *Release) SetImageArtifactID(id string) {
	if len(r.ArtifactIDs) == 0 {
		r.ArtifactIDs = []string{id}
	}
	r.ArtifactIDs[0] = id
}

func (r *Release) FileArtifactIDs() []string {
	if len(r.ArtifactIDs) < 1 {
		return nil
	}
	return r.ArtifactIDs[1:len(r.ArtifactIDs)]
}

func (r *Release) IsGitDeploy() bool {
	return r.Meta["git"] == "true"
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
	ID        string            `json:"id,omitempty"`
	Type      host.ArtifactType `json:"type,omitempty"`
	URI       string            `json:"uri,omitempty"`
	Meta      map[string]string `json:"meta,omitempty"`
	CreatedAt *time.Time        `json:"created_at,omitempty"`
}

func (a *Artifact) HostArtifact() *host.Artifact {
	return &host.Artifact{
		URI:  a.URI,
		Type: a.Type,
	}
}

type Formation struct {
	AppID     string                       `json:"app,omitempty"`
	ReleaseID string                       `json:"release,omitempty"`
	Processes map[string]int               `json:"processes,omitempty"`
	Tags      map[string]map[string]string `json:"tags,omitempty"`
	CreatedAt *time.Time                   `json:"created_at,omitempty"`
	UpdatedAt *time.Time                   `json:"updated_at,omitempty"`
}

type Key struct {
	ID        string     `json:"fingerprint,omitempty"`
	Key       string     `json:"key,omitempty"`
	Comment   string     `json:"comment,omitempty"`
	CreatedAt *time.Time `json:"created_at,omitempty"`
}

type Job struct {
	// ID is the job's full cluster ID (i.e. hostID-UUID) and can be empty
	// if the job is pending
	ID string `json:"id,omitempty"`

	// UUID is the uuid part of the job's full cluster ID and is the
	// primary key field in the database (so it is always set)
	UUID string `json:"uuid"`

	// HostID is the host ID part of the job's full cluster ID and can be
	// empty if the job is pending
	HostID string `json:"host_id,omitempty"`

	AppID      string            `json:"app,omitempty"`
	ReleaseID  string            `json:"release,omitempty"`
	Type       string            `json:"type,omitempty"`
	State      JobState          `json:"state,omitempty"`
	Cmd        []string          `json:"cmd,omitempty"`
	Meta       map[string]string `json:"meta,omitempty"`
	ExitStatus *int32            `json:"exit_status,omitempty"`
	HostError  *string           `json:"host_error,omitempty"`
	RunAt      *time.Time        `json:"run_at,omitempty"`
	Restarts   *int32            `json:"restarts,omitempty"`
	CreatedAt  *time.Time        `json:"created_at,omitempty"`
	UpdatedAt  *time.Time        `json:"updated_at,omitempty"`
}

type JobState string

const (
	JobStatePending  JobState = "pending"
	JobStateStarting JobState = "starting"
	JobStateUp       JobState = "up"
	JobStateStopping JobState = "stopping"
	JobStateDown     JobState = "down"

	// JobStateCrashed and JobStateFailed are no longer valid job states,
	// but we still need to handle them in case they are set by old
	// schedulers still using the legacy code.
	JobStateCrashed JobState = "crashed"
	JobStateFailed  JobState = "failed"
)

type DomainMigration struct {
	ID         string        `json:"id"`
	OldTLSCert *tlscert.Cert `json:"old_tls_cert,omitempty"`
	TLSCert    *tlscert.Cert `json:"tls_cert,omitempty"`
	OldDomain  string        `json:"old_domain"`
	Domain     string        `json:"domain"`
	CreatedAt  *time.Time    `json:"created_at,omitempty"`
	FinishedAt *time.Time    `json:"finished_at,omitempty"`
}

func (e *Job) IsDown() bool {
	return e.State == JobStateDown || e.State == JobStateCrashed || e.State == JobStateFailed
}

type JobEvents map[string]map[JobState]int

func (j JobEvents) Count() int {
	var n int
	for _, procs := range j {
		for _, i := range procs {
			n += i
		}
	}
	return n
}

func (j JobEvents) Equals(other JobEvents) bool {
	for typ, events := range j {
		diff, ok := other[typ]
		if !ok {
			return false
		}
		for state, count := range events {
			if diff[state] != count {
				return false
			}
		}
	}
	return true
}

func JobUpEvents(count int) map[JobState]int {
	return map[JobState]int{JobStateUp: count}
}

func JobDownEvents(count int) map[JobState]int {
	return map[JobState]int{JobStateDown: count}
}

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

const DefaultDeployTimeout = 120 // seconds

type Deployment struct {
	ID            string         `json:"id,omitempty"`
	AppID         string         `json:"app,omitempty"`
	OldReleaseID  string         `json:"old_release,omitempty"`
	NewReleaseID  string         `json:"new_release,omitempty"`
	Strategy      string         `json:"strategy,omitempty"`
	Status        string         `json:"status,omitempty"`
	Processes     map[string]int `json:"processes,omitempty"`
	DeployTimeout int32          `json:"deploy_timeout,omitempty"`
	CreatedAt     *time.Time     `json:"created_at,omitempty"`
	FinishedAt    *time.Time     `json:"finished_at,omitempty"`
}

type DeployID struct {
	ID string
}

type DeploymentEvent struct {
	AppID        string   `json:"app,omitempty"`
	DeploymentID string   `json:"deployment,omitempty"`
	ReleaseID    string   `json:"release,omitempty"`
	Status       string   `json:"status,omitempty"`
	JobType      string   `json:"job_type,omitempty"`
	JobState     JobState `json:"job_state,omitempty"`
	Error        string   `json:"error,omitempty"`
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

type NotFoundError struct {
	Resource string `json:"field"`
}

func (n NotFoundError) Error() string {
	return fmt.Sprintf("resource not found: %s", n.Resource)
}

// SSELogChunk is used as a data wrapper for the `GET /apps/:apps_id/log` SSE stream
type SSELogChunk struct {
	Event string          `json:"event,omitempty"`
	Data  json.RawMessage `json:"data,omitempty"`
}

type LogOpts struct {
	Follow      bool
	JobID       string
	Lines       *int
	ProcessType *string
}

type EventType string

const (
	EventTypeApp                 EventType = "app"
	EventTypeAppDeletion         EventType = "app_deletion"
	EventTypeAppRelease          EventType = "app_release"
	EventTypeDeployment          EventType = "deployment"
	EventTypeJob                 EventType = "job"
	EventTypeScale               EventType = "scale"
	EventTypeRelease             EventType = "release"
	EventTypeArtifact            EventType = "artifact"
	EventTypeProvider            EventType = "provider"
	EventTypeResource            EventType = "resource"
	EventTypeResourceDeletion    EventType = "resource_deletion"
	EventTypeResourceAppDeletion EventType = "resource_app_deletion"
	EventTypeKey                 EventType = "key"
	EventTypeKeyDeletion         EventType = "key_deletion"
	EventTypeRoute               EventType = "route"
	EventTypeRouteDeletion       EventType = "route_deletion"
	EventTypeDomainMigration     EventType = "domain_migration"
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

type Scale struct {
	PrevProcesses map[string]int `json:"prev_processes,omitempty"`
	Processes     map[string]int `json:"processes"`
	ReleaseID     string         `json:"release"`
}

type AppRelease struct {
	PrevRelease *Release `json:"prev_release,omitempty"`
	Release     *Release `json:"release"`
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

type DomainMigrationEvent struct {
	DomainMigration *DomainMigration `json:"domain_migration"`
	Error           string           `json:"error,omitempty"`
}
