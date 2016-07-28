package graphqltypes

import (
	"encoding/json"
	"time"

	ct "github.com/flynn/flynn/controller/types"
	"github.com/flynn/flynn/router/types"
)

type App struct {
	ID            string            `json:"id"`
	Name          string            `json:"name"`
	Meta          map[string]string `json:"meta"`
	Strategy      string            `json:"strategy"`
	Release       *Release          `json:"current_release"`
	Releases      []*Release        `json:"releases"`
	Formations    []*Formation      `json:"formations"`
	Deployments   []*Deployment     `json:"deployments"`
	Resources     []*Resource       `json:"resources"`
	Routes        []*Route          `json:"routes"`
	Jobs          []*Job            `json:"jobs"`
	DeployTimeout int32             `json:"deploy_timeout"`
	CreatedAt     *time.Time        `json:"created_at"`
	UpdatedAt     *time.Time        `json:"updated_at"`
}

func (a *App) ToStandardType() *ct.App {
	var releaseID string
	if a.Release != nil {
		releaseID = a.Release.ID
	}
	return &ct.App{
		ID:            a.ID,
		Name:          a.Name,
		Meta:          a.Meta,
		Strategy:      a.Strategy,
		ReleaseID:     releaseID,
		DeployTimeout: a.DeployTimeout,
		CreatedAt:     a.CreatedAt,
		UpdatedAt:     a.UpdatedAt,
	}
}

type Release struct {
	ID        string                    `json:"id"`
	Artifacts []*ct.Artifact            `json:"artifacts"`
	Env       map[string]string         `json:"env"`
	Meta      map[string]string         `json:"meta"`
	Processes map[string]ct.ProcessType `json:"processes"`
	CreatedAt *time.Time                `json:"created_at"`
}

func (r *Release) ToStandardType() *ct.Release {
	var legacyArtifactID string
	var artifactIDs []string
	if len(r.Artifacts) > 0 {
		artifactIDs = make([]string, len(r.Artifacts))
		for ai, a := range r.Artifacts {
			artifactIDs[ai] = a.ID
		}
		legacyArtifactID = artifactIDs[0]
	}
	return &ct.Release{
		ID:               r.ID,
		ArtifactIDs:      artifactIDs,
		LegacyArtifactID: legacyArtifactID,
		Env:              r.Env,
		Meta:             r.Meta,
		Processes:        r.Processes,
		CreatedAt:        r.CreatedAt,
	}
}

type ReleaseDeletion struct {
	App           *App     `json:"app"`
	Release       *Release `json:"release"`
	RemainingApps []*App   `json:"remaining_apps"`
	DeletedFiles  []string `json:"deleted_files"`
}

func (d *ReleaseDeletion) ToStandardType() *ct.ReleaseDeletion {
	var appID, releaseID string
	if d.App != nil {
		appID = d.App.ID
	}
	if d.Release != nil {
		releaseID = d.Release.ID
	}
	var remainingApps []string
	if len(d.RemainingApps) > 0 {
		remainingApps = make([]string, len(d.RemainingApps))
		for i, app := range d.RemainingApps {
			remainingApps[i] = app.ID
		}
	}
	var deletedFiles []string
	if len(d.DeletedFiles) > 0 {
		deletedFiles = d.DeletedFiles
	}
	return &ct.ReleaseDeletion{
		AppID:         appID,
		ReleaseID:     releaseID,
		RemainingApps: remainingApps,
		DeletedFiles:  deletedFiles,
	}
}

type ReleaseDeletionEvent struct {
	ReleaseDeletion *ReleaseDeletion `json:"release_deletion"`
	Error           string           `json:"error"`
}

func (e *ReleaseDeletionEvent) ToStandardType() *ct.ReleaseDeletionEvent {
	return &ct.ReleaseDeletionEvent{
		ReleaseDeletion: e.ReleaseDeletion.ToStandardType(),
		Error:           e.Error,
	}
}

type AppRelease struct {
	PrevRelease *Release `json:"prev_release,omitempty"`
	Release     *Release `json:"release"`
}

func (r *AppRelease) ToStandardType() *ct.AppRelease {
	var prevRelease, release *ct.Release
	if r.PrevRelease != nil {
		prevRelease = r.PrevRelease.ToStandardType()
	}
	if r.Release != nil {
		release = r.Release.ToStandardType()
	}
	return &ct.AppRelease{
		PrevRelease: prevRelease,
		Release:     release,
	}
}

type AppDeletion struct {
	App              *App            `json:"app"`
	DeletedRoutes    []*router.Route `json:"deleted_routes"`
	DeletedResources []*Resource     `json:"deleted_resources"`
	DeletedReleases  []*Release      `json:"deleted_releases"`
}

func (d *AppDeletion) ToStandardType() *ct.AppDeletion {
	var appID string
	if d.App != nil {
		appID = d.App.ID
	}
	deletedResources := make([]*ct.Resource, len(d.DeletedResources))
	for i, r := range d.DeletedResources {
		deletedResources[i] = r.ToStandardType()
	}
	deletedReleases := make([]*ct.Release, len(d.DeletedReleases))
	for i, r := range d.DeletedReleases {
		deletedReleases[i] = r.ToStandardType()
	}
	return &ct.AppDeletion{
		AppID:            appID,
		DeletedRoutes:    d.DeletedRoutes,
		DeletedResources: deletedResources,
		DeletedReleases:  deletedReleases,
	}
}

type Formation struct {
	App       *App                         `json:"app"`
	Release   *Release                     `json:"release"`
	Processes map[string]int               `json:"processes"`
	Tags      map[string]map[string]string `json:"tags"`
	CreatedAt *time.Time                   `json:"created_at"`
	UpdatedAt *time.Time                   `json:"updated_at"`
}

func (f *Formation) ToStandardType() *ct.Formation {
	var appID string
	if f.App != nil {
		appID = f.App.ID
	}
	var releaseID string
	if f.Release != nil {
		releaseID = f.Release.ID
	}
	return &ct.Formation{
		AppID:     appID,
		ReleaseID: releaseID,
		Processes: f.Processes,
		Tags:      f.Tags,
		CreatedAt: f.CreatedAt,
		UpdatedAt: f.UpdatedAt,
	}
}

type ExpandedFormation struct {
	App           *App                         `json:"app"`
	Release       *Release                     `json:"release"`
	ImageArtifact *ct.Artifact                 `json:"image_artifact"`
	FileArtifacts []*ct.Artifact               `json:"file_artifacts"`
	Processes     map[string]int               `json:"processes"`
	Tags          map[string]map[string]string `json:"tags"`
	UpdatedAt     time.Time                    `json:"updated_at"`
}

func (f *ExpandedFormation) ToStandardType() *ct.ExpandedFormation {
	return &ct.ExpandedFormation{
		App:           f.App.ToStandardType(),
		Release:       f.Release.ToStandardType(),
		ImageArtifact: f.ImageArtifact,
		FileArtifacts: f.FileArtifacts,
		Processes:     f.Processes,
		Tags:          f.Tags,
		UpdatedAt:     f.UpdatedAt,
	}
}

type Deployment struct {
	ID            string         `json:"id"`
	App           *App           `json:"app"`
	OldRelease    *Release       `json:"old_release"`
	NewRelease    *Release       `json:"new_release"`
	Strategy      string         `json:"strategy"`
	Status        string         `json:"status"`
	Processes     map[string]int `json:"processes"`
	DeployTimeout int32          `json:"deploy_timeout"`
	CreatedAt     *time.Time     `json:"created_at"`
	FinishedAt    *time.Time     `json:"finished_at"`
}

func (d *Deployment) ToStandardType() *ct.Deployment {
	var appID string
	if d.App != nil {
		appID = d.App.ID
	}
	var oldReleaseID string
	if d.OldRelease != nil {
		oldReleaseID = d.OldRelease.ID
	}
	var newReleaseID string
	if d.NewRelease != nil {
		newReleaseID = d.NewRelease.ID
	}
	return &ct.Deployment{
		ID:            d.ID,
		AppID:         appID,
		OldReleaseID:  oldReleaseID,
		NewReleaseID:  newReleaseID,
		Strategy:      d.Strategy,
		Status:        d.Status,
		Processes:     d.Processes,
		DeployTimeout: d.DeployTimeout,
		CreatedAt:     d.CreatedAt,
		FinishedAt:    d.FinishedAt,
	}
}

type DeploymentEvent struct {
	App        *App        `json:"app,omitempty"`
	Deployment *Deployment `json:"deployment,omitempty"`
	Release    *Release    `json:"release,omitempty"`
	Status     string      `json:"status,omitempty"`
	JobType    string      `json:"job_type,omitempty"`
	JobState   ct.JobState `json:"job_state,omitempty"`
	Error      string      `json:"error,omitempty"`
}

func (e *DeploymentEvent) ToStandardType() *ct.DeploymentEvent {
	var appID, deploymentID, releaseID string
	if e.App != nil {
		appID = e.App.ID
	}
	if e.Deployment != nil {
		deploymentID = e.Deployment.ID
	}
	if e.Release != nil {
		releaseID = e.Release.ID
	}
	return &ct.DeploymentEvent{
		AppID:        appID,
		DeploymentID: deploymentID,
		ReleaseID:    releaseID,
		Status:       e.Status,
		JobType:      e.JobType,
		JobState:     e.JobState,
		Error:        e.Error,
	}
}

type Provider struct {
	ID        string      `json:"id"`
	URL       string      `json:"url"`
	Name      string      `json:"name"`
	Resources []*Resource `json:"resources"`
	CreatedAt *time.Time  `json:"created_at"`
	UpdatedAt *time.Time  `json:"updated_at"`
}

func (p *Provider) ToStandardType() *ct.Provider {
	return &ct.Provider{
		ID:        p.ID,
		URL:       p.URL,
		Name:      p.Name,
		CreatedAt: p.CreatedAt,
		UpdatedAt: p.UpdatedAt,
	}
}

type Resource struct {
	ID         string            `json:"id"`
	Provider   *Provider         `json:"provider"`
	ExternalID string            `json:"external_id"`
	Env        map[string]string `json:"env"`
	Apps       []*App            `json:"apps"`
	CreatedAt  *time.Time        `json:"created_at"`
}

func (r *Resource) ToStandardType() *ct.Resource {
	var providerID string
	if r.Provider != nil {
		providerID = r.Provider.ID
	}
	var appIDs []string
	if r.Apps != nil {
		appIDs = make([]string, len(r.Apps))
		for i, a := range r.Apps {
			appIDs[i] = a.ID
		}
	}
	return &ct.Resource{
		ID:         r.ID,
		ProviderID: providerID,
		ExternalID: r.ExternalID,
		Env:        r.Env,
		Apps:       appIDs,
		CreatedAt:  r.CreatedAt,
	}
}

type Route struct {
	Type        string              `json:"type"`
	ID          string              `json:"id"`
	ParentRef   string              `json:"parent_ref"`
	Service     string              `json:"service"`
	Leader      bool                `json:"leader"`
	CreatedAt   time.Time           `json:"created_at"`
	UpdatedAt   time.Time           `json:"updated_at"`
	Domain      string              `json:"domain"`
	Certificate *router.Certificate `json:"certificate"`
	Sticky      bool                `json:"sticky"`
	Path        string              `json:"path"`
	Port        int32               `json:"port"`
	App         *App                `json:"app"`
}

func (r *Route) ToStandardType() *router.Route {
	return &router.Route{
		Type:        r.Type,
		ID:          r.ID,
		ParentRef:   r.ParentRef,
		Service:     r.Service,
		Leader:      r.Leader,
		CreatedAt:   r.CreatedAt,
		UpdatedAt:   r.UpdatedAt,
		Domain:      r.Domain,
		Certificate: r.Certificate,
		Sticky:      r.Sticky,
		Path:        r.Path,
		Port:        r.Port,
	}
}

type Job struct {
	ID         string            `json:"id"`
	UUID       string            `json:"uuid"`
	HostID     string            `json:"host_id"`
	App        *App              `json:"app"`
	Release    *Release          `json:"release"`
	Type       string            `json:"type"`
	State      ct.JobState       `json:"state"`
	Args       []string          `json:"args"`
	Meta       map[string]string `json:"meta"`
	ExitStatus *int32            `json:"exit_status"`
	HostError  *string           `json:"host_error"`
	RunAt      *time.Time        `json:"run_at"`
	Restarts   *int32            `json:"restarts"`
	CreatedAt  *time.Time        `json:"created_at"`
	UpdatedAt  *time.Time        `json:"updated_at"`
}

func (j *Job) ToStandardType() *ct.Job {
	var appID string
	if j.App != nil {
		appID = j.App.ID
	}
	var releaseID string
	if j.Release != nil {
		releaseID = j.Release.ID
	}
	return &ct.Job{
		ID:         j.ID,
		UUID:       j.UUID,
		HostID:     j.HostID,
		AppID:      appID,
		ReleaseID:  releaseID,
		Type:       j.Type,
		State:      j.State,
		Args:       j.Args,
		Meta:       j.Meta,
		ExitStatus: j.ExitStatus,
		HostError:  j.HostError,
		RunAt:      j.RunAt,
		Restarts:   j.Restarts,
		CreatedAt:  j.CreatedAt,
		UpdatedAt:  j.UpdatedAt,
	}
}

type Event struct {
	ID         int64           `json:"id,omitempty"`
	App        *App            `json:"app,omitempty"`
	ObjectType ct.EventType    `json:"object_type,omitempty"`
	ObjectID   string          `json:"object_id,omitempty"`
	Data       json.RawMessage `json:"data,omitempty"`
	CreatedAt  *time.Time      `json:"created_at,omitempty"`
}

func (e *Event) ToStandardType() *ct.Event {
	var appID string
	if e.App != nil {
		appID = e.App.ID
	}
	data := e.Data
	if e.ObjectType == ct.EventTypeRelease {
		var release *Release
		if err := json.Unmarshal(data, &release); err != nil {
			// TODO(jvatic): Refactor to allow this method to return an error
			panic(err)
		}
		var err error
		data, err = json.Marshal(release.ToStandardType())
		if err != nil {
			// TODO(jvatic): Ditto on returning error
			panic(err)
		}
	}
	if e.ObjectType == ct.EventTypeAppRelease {
		var appRelease *AppRelease
		if err := json.Unmarshal(data, &appRelease); err != nil {
			// TODO(jvatic): Refactor to allow this method to return an error
			panic(err)
		}
		var err error
		data, err = json.Marshal(appRelease.ToStandardType())
		if err != nil {
			// TODO(jvatic): Ditto on returning error
			panic(err)
		}
	}
	if e.ObjectType == ct.EventTypeAppDeletion {
		var appDeletion *AppDeletion
		if err := json.Unmarshal(data, &appDeletion); err != nil {
			// TODO(jvatic): Refactor to allow this method to return an error
			panic(err)
		}
		var err error
		data, err = json.Marshal(appDeletion.ToStandardType())
		if err != nil {
			// TODO(jvatic): Ditto on returning error
			panic(err)
		}
	}
	if e.ObjectType == ct.EventTypeDeployment {
		var deploymentEvent *DeploymentEvent
		if err := json.Unmarshal(data, &deploymentEvent); err != nil {
			// TODO(jvatic): Refactor to allow this method to return an error
			panic(err)
		}
		var err error
		data, err = json.Marshal(deploymentEvent.ToStandardType())
		if err != nil {
			// TODO(jvatic): Ditto on returning error
			panic(err)
		}
	}
	if e.ObjectType == ct.EventTypeReleaseDeletion {
		var releaseDeletionEvent *ReleaseDeletionEvent
		if err := json.Unmarshal(data, &releaseDeletionEvent); err != nil {
			// TODO(jvatic): Refactor to allow this method to return an error
			panic(err)
		}
		var err error
		data, err = json.Marshal(releaseDeletionEvent.ToStandardType())
		if err != nil {
			// TODO(jvatic): Ditto on returning error
			panic(err)
		}
	}
	if e.ObjectType == ct.EventTypeRoute && data != nil {
		var route *Route
		if err := json.Unmarshal(data, &route); err != nil {
			// TODO(jvatic): Refactor to allow this method to return an error
			panic(err)
		}
		var err error
		data, err = json.Marshal(route.ToStandardType())
		if err != nil {
			// TODO(jvaitc): Ditto on returning error
			panic(err)
		}
	}
	return &ct.Event{
		ID:         e.ID,
		AppID:      appID,
		ObjectType: e.ObjectType,
		ObjectID:   e.ObjectID,
		Data:       data,
		CreatedAt:  e.CreatedAt,
	}
}
