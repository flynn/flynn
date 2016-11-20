package types

import (
	"crypto/sha512"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/flynn/flynn/host/resource"
	"github.com/flynn/flynn/host/types"
	"github.com/flynn/flynn/pkg/tlscert"
	"github.com/flynn/flynn/router/types"
	"github.com/jtacoma/uritemplates"
	"github.com/opencontainers/runc/libcontainer/configs"
	"github.com/tent/canonical-json-go"
)

const RouteParentRefPrefix = "controller/apps/"

type ExpandedFormation struct {
	App       *App                         `json:"app,omitempty"`
	Release   *Release                     `json:"release,omitempty"`
	Artifacts []*Artifact                  `json:"artifacts,omitempty"`
	Processes map[string]int               `json:"processes,omitempty"`
	Tags      map[string]map[string]string `json:"tags,omitempty"`
	UpdatedAt time.Time                    `json:"updated_at,omitempty"`
	Deleted   bool                         `json:"deleted,omitempty"`

	// DeprecatedImageArtifact is for creating backwards compatible cluster
	// backups (the restore process used to require the ImageArtifact field
	// to be set).
	DeprecatedImageArtifact *Artifact `json:"artifact,omitempty"`
}

func (e *ExpandedFormation) Formation() *Formation {
	return &Formation{
		AppID:     e.App.ID,
		ReleaseID: e.Release.ID,
		Processes: e.Processes,
		Tags:      e.Tags,
		UpdatedAt: &e.UpdatedAt,
	}
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

func (a *App) RedisAppliance() bool {
	return a.System() && strings.HasPrefix(a.Name, "redis-")
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

func (r *Release) IsGitDeploy() bool {
	return r.Meta["git"] == "true"
}

func (r *Release) IsDockerReceiveDeploy() bool {
	return r.Meta["docker-receive"] == "true"
}

type ProcessType struct {
	Args              []string           `json:"args,omitempty"`
	Env               map[string]string  `json:"env,omitempty"`
	Ports             []Port             `json:"ports,omitempty"`
	Volumes           []VolumeReq        `json:"volumes,omitempty"`
	Omni              bool               `json:"omni,omitempty"` // omnipresent - present on all hosts
	HostNetwork       bool               `json:"host_network,omitempty"`
	Service           string             `json:"service,omitempty"`
	Resurrect         bool               `json:"resurrect,omitempty"`
	Resources         resource.Resources `json:"resources,omitempty"`
	Mounts            []host.Mount       `json:"mounts,omitempty"`
	LinuxCapabilities []string           `json:"linux_capabilities,omitempty"`
	AllowedDevices    []*configs.Device  `json:"allowed_devices,omitempty"`
	WriteableCgroups  bool               `json:"writeable_cgroups,omitempty"`

	// Entrypoint and Cmd are DEPRECATED: use Args instead
	DeprecatedCmd        []string `json:"cmd,omitempty"`
	DeprecatedEntrypoint []string `json:"entrypoint,omitempty"`

	// Data is DEPRECATED: populate Volumes instead
	DeprecatedData bool `json:"data,omitempty"`
}

type Port struct {
	Port    int           `json:"port"`
	Proto   string        `json:"proto"`
	Service *host.Service `json:"service,omitempty"`
}

type VolumeReq struct {
	Path         string `json:"path"`
	DeleteOnStop bool   `json:"delete_on_stop"`
}

type ArtifactType string

const (
	// ArtifactTypeFlynn is the type of artifact which references a Flynn
	// image manifest
	ArtifactTypeFlynn ArtifactType = "flynn"

	// DeprecatedArtifactTypeFile is a deprecated artifact type which was
	// used to reference slugs when they used to be tarballs stored in the
	// blobstore (they are now squashfs based Flynn images)
	DeprecatedArtifactTypeFile ArtifactType = "file"

	// DeprecatedArtifactTypeDocker is a deprecated artifact type which
	// used to reference a pinkerton-compatible Docker URI used to pull
	// Docker images from a Docker registry (they are now converted to
	// squashfs based Flynn images either at build time or at push time by
	// docker-receive)
	DeprecatedArtifactTypeDocker ArtifactType = "docker"
)

type Artifact struct {
	ID               string            `json:"id,omitempty"`
	Type             ArtifactType      `json:"type,omitempty"`
	URI              string            `json:"uri,omitempty"`
	Meta             map[string]string `json:"meta,omitempty"`
	RawManifest      json.RawMessage   `json:"manifest,omitempty"`
	Hashes           map[string]string `json:"hashes,omitempty"`
	Size             int64             `json:"size,omitempty"`
	LayerURLTemplate string            `json:"layer_url_template,omitempty"`
	CreatedAt        *time.Time        `json:"created_at,omitempty"`

	manifest     *ImageManifest
	manifestOnce sync.Once
}

func (a *Artifact) Manifest() *ImageManifest {
	a.manifestOnce.Do(func() {
		a.manifest = &ImageManifest{}
		json.Unmarshal(a.RawManifest, a.manifest)
	})
	return a.manifest
}

func (a *Artifact) LayerURL(layer *ImageLayer) string {
	tmpl, err := uritemplates.Parse(a.LayerURLTemplate)
	if err != nil {
		return ""
	}
	values := map[string]interface{}{"id": layer.ID}
	expanded, _ := tmpl.Expand(values)
	return expanded
}

func (a *Artifact) Blobstore() bool {
	return a.Meta["blobstore"] == "true"
}

type Formation struct {
	AppID     string                       `json:"app,omitempty"`
	ReleaseID string                       `json:"release,omitempty"`
	Processes map[string]int               `json:"processes,omitempty"`
	Tags      map[string]map[string]string `json:"tags,omitempty"`
	CreatedAt *time.Time                   `json:"created_at,omitempty"`
	UpdatedAt *time.Time                   `json:"updated_at,omitempty"`
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
	Args       []string          `json:"args,omitempty"`
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

type PartitionType string

const (
	PartitionTypeBackground PartitionType = "background"
	PartitionTypeSystem     PartitionType = "system"
	PartitionTypeUser       PartitionType = "user"
)

type NewJob struct {
	ReleaseID   string             `json:"release,omitempty"`
	ArtifactIDs []string           `json:"artifacts,omitempty"`
	ReleaseEnv  bool               `json:"release_env,omitempty"`
	Args        []string           `json:"args,omitempty"`
	Env         map[string]string  `json:"env,omitempty"`
	Meta        map[string]string  `json:"meta,omitempty"`
	TTY         bool               `json:"tty,omitempty"`
	Columns     int                `json:"tty_columns,omitempty"`
	Lines       int                `json:"tty_lines,omitempty"`
	DisableLog  bool               `json:"disable_log,omitempty"`
	Resources   resource.Resources `json:"resources,omitempty"`
	Data        bool               `json:"data,omitempty"`
	Partition   PartitionType      `json:"partition,omitempty"`

	// Entrypoint and Cmd are DEPRECATED: use Args instead
	DeprecatedCmd        []string `json:"cmd,omitempty"`
	DeprecatedEntrypoint []string `json:"entrypoint,omitempty"`

	// Artifact is DEPRECATED: use Artifacts instead
	DeprecatedArtifact string `json:"artifact,omitempty"`
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

type EventType string

const (
	EventTypeApp                  EventType = "app"
	EventTypeAppDeletion          EventType = "app_deletion"
	EventTypeAppRelease           EventType = "app_release"
	EventTypeDeployment           EventType = "deployment"
	EventTypeJob                  EventType = "job"
	EventTypeScale                EventType = "scale"
	EventTypeRelease              EventType = "release"
	EventTypeReleaseDeletion      EventType = "release_deletion"
	EventTypeArtifact             EventType = "artifact"
	EventTypeProvider             EventType = "provider"
	EventTypeResource             EventType = "resource"
	EventTypeResourceDeletion     EventType = "resource_deletion"
	EventTypeResourceAppDeletion  EventType = "resource_app_deletion"
	EventTypeKey                  EventType = "key"
	EventTypeKeyDeletion          EventType = "key_deletion"
	EventTypeRoute                EventType = "route"
	EventTypeRouteDeletion        EventType = "route_deletion"
	EventTypeDomainMigration      EventType = "domain_migration"
	EventTypeClusterBackup        EventType = "cluster_backup"
	EventTypeAppGarbageCollection EventType = "app_garbage_collection"
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
	DeletedReleases  []*Release      `json:"deleted_releases"`
}

type AppDeletionEvent struct {
	AppDeletion *AppDeletion `json:"app_deletion"`
	Error       string       `json:"error"`
}

type DomainMigrationEvent struct {
	DomainMigration *DomainMigration `json:"domain_migration"`
	Error           string           `json:"error,omitempty"`
}

const (
	ClusterBackupStatusRunning  string = "running"
	ClusterBackupStatusComplete string = "complete"
	ClusterBackupStatusError    string = "error"
)

type ClusterBackup struct {
	ID          string     `json:"id,omitempty"`
	Status      string     `json:"status"`
	SHA512      string     `json:"sha512,omitempty"`
	Size        int64      `json:"size,omitempty"`
	Error       string     `json:"error,omitempty"`
	CreatedAt   *time.Time `json:"created_at,omitempty"`
	UpdatedAt   *time.Time `json:"updated_at,omitempty"`
	CompletedAt *time.Time `json:"completed_at,omitempty"`
}

type ReleaseDeletion struct {
	AppID         string   `json:"app"`
	ReleaseID     string   `json:"release"`
	RemainingApps []string `json:"remaining_apps"`
	DeletedFiles  []string `json:"deleted_files"`
}

type ReleaseDeletionEvent struct {
	ReleaseDeletion *ReleaseDeletion `json:"release_deletion"`
	Error           string           `json:"error"`
}

type JobWatcher interface {
	WaitFor(expected JobEvents, timeout time.Duration, callback func(*Job) error) error
	Close() error
}

type ListEventsOptions struct {
	AppID       string
	ObjectTypes []EventType
	ObjectID    string
	BeforeID    *int64
	SinceID     *int64
	Count       int
}

type StreamEventsOptions struct {
	AppID       string
	ObjectTypes []EventType
	ObjectID    string
	Past        bool
	Count       int
}

type AppGarbageCollection struct {
	AppID           string   `json:"app_id"`
	DeletedReleases []string `json:"deleted_releases"`
}

type AppGarbageCollectionEvent struct {
	AppGarbageCollection *AppGarbageCollection `json:"app_garbage_collection"`
	Error                string                `json:"error"`
}

type ImageManifestType string

const ImageManifestTypeV1 ImageManifestType = "application/vnd.flynn.image.manifest.v1+json"

type ImageManifest struct {
	Type        ImageManifestType           `json:"_type"`
	Meta        map[string]string           `json:"meta,omitempty"`
	Entrypoints map[string]*ImageEntrypoint `json:"entrypoints,omitempty"`
	Rootfs      []*ImageRootfs              `json:"rootfs,omitempty"`

	hashes     map[string]string
	hashesOnce sync.Once
}

func (i *ImageManifest) ID() string {
	return i.Hashes()["sha512_256"]
}

func (i ImageManifest) RawManifest() json.RawMessage {
	data, _ := cjson.Marshal(i)
	return data
}

func (i *ImageManifest) Hashes() map[string]string {
	i.hashesOnce.Do(func() {
		digest := sha512.Sum512_256(i.RawManifest())
		i.hashes = map[string]string{"sha512_256": hex.EncodeToString(digest[:])}
	})
	return i.hashes
}

func (m *ImageManifest) DefaultEntrypoint() *ImageEntrypoint {
	return m.Entrypoints["_default"]
}

type ImageEntrypoint struct {
	Env               map[string]string `json:"env,omitempty"`
	WorkingDir        string            `json:"cwd,omitempty"`
	Args              []string          `json:"args,omitempty"`
	LinuxCapabilities []string          `json:"linux_capabilities,omitempty"`
	Uid               *uint32           `json:"uid,omitempty"`
	Gid               *uint32           `json:"gid,omitempty"`
}

type ImageRootfs struct {
	Platform *ImagePlatform `json:"platform,omitempty"`
	Layers   []*ImageLayer  `json:"layers,omitempty"`
}

var DefaultImagePlatform = &ImagePlatform{
	Architecture: "amd64",
	OS:           "linux",
}

type ImagePlatform struct {
	Architecture string `json:"architecture,omitempty"`
	OS           string `json:"os,omitempty"`
}

type ImageLayerType string

const ImageLayerTypeSquashfs ImageLayerType = "application/vnd.flynn.image.squashfs.v1"

type ImageLayer struct {
	ID     string            `json:"id,omitempty"`
	Type   ImageLayerType    `json:"type,omitempty"`
	Length int64             `json:"length,omitempty"`
	Hashes map[string]string `json:"hashes,omitempty"`
}

type ImagePullInfo struct {
	Name     string        `json:"name"`
	Type     ImagePullType `json:"type"`
	Artifact *Artifact     `json:"artifact"`
	Layer    *ImageLayer   `json:"layer"`
}

type ImagePullType string

const (
	ImagePullTypeImage ImagePullType = "image"
	ImagePullTypeLayer ImagePullType = "layer"
)
