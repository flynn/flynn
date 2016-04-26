package utils

import (
	"fmt"
	"regexp"
	"strings"
	"time"

	ct "github.com/flynn/flynn/controller/types"
	"github.com/flynn/flynn/discoverd/client"
	"github.com/flynn/flynn/host/types"
	"github.com/flynn/flynn/host/volume"
	"github.com/flynn/flynn/pkg/cluster"
	"github.com/flynn/flynn/pkg/stream"
)

func JobConfigFormation(f *ct.ExpandedFormation, name, hostID string, uuid string) *host.Job {
	t := f.Release.Processes[name]
	env := make(map[string]string, len(f.Release.Env)+len(t.Env)+5)
	for k, v := range f.Release.Env {
		env[k] = v
	}
	for k, v := range t.Env {
		env[k] = v
	}
	id := cluster.GenerateJobID(hostID, uuid)
	env["FLYNN_APP_ID"] = f.App.ID
	env["FLYNN_APP_NAME"] = f.App.Name
	env["FLYNN_RELEASE_ID"] = f.Release.ID
	env["FLYNN_PROCESS_TYPE"] = name
	env["FLYNN_JOB_ID"] = id
	metadata := make(map[string]string, len(f.App.Meta)+4)
	for k, v := range f.App.Meta {
		metadata[k] = v
	}
	metadata["flynn-controller.app"] = f.App.ID
	metadata["flynn-controller.app_name"] = f.App.Name
	metadata["flynn-controller.release"] = f.Release.ID
	metadata["flynn-controller.formation"] = "true"
	metadata["flynn-controller.type"] = name
	job := &host.Job{
		ID:       id,
		Metadata: metadata,
		Config: host.ContainerConfig{
			Cmd:         t.Cmd,
			Env:         env,
			HostNetwork: t.HostNetwork,
		},
		Resurrect: t.Resurrect,
		Resources: t.Resources,
	}
	if f.App.Meta["flynn-system-app"] == "true" {
		job.Partition = "system"
	}
	if len(t.Entrypoint) > 0 {
		job.Config.Entrypoint = t.Entrypoint
	}
	if f.ImageArtifact != nil {
		job.ImageArtifact = f.ImageArtifact.HostArtifact()
	}
	if len(f.FileArtifacts) > 0 {
		job.FileArtifacts = make([]*host.Artifact, len(f.FileArtifacts))
		for i, artifact := range f.FileArtifacts {
			job.FileArtifacts[i] = artifact.HostArtifact()
		}
	}
	job.Config.Ports = make([]host.Port, len(t.Ports))
	for i, p := range t.Ports {
		job.Config.Ports[i].Proto = p.Proto
		job.Config.Ports[i].Port = p.Port
		job.Config.Ports[i].Service = p.Service
	}
	return job
}

func JobConfigRequest(req *ct.ExpandedJobRequest, hostID string) *host.Job {
	config := req.Config
	if config == nil {
		config = &ct.JobConfig{}
	}
	env := make(map[string]string, len(req.Release.Env)+len(config.Env)+5)
	if config.ReleaseEnv {
		for k, v := range req.Release.Env {
			env[k] = v
		}
	}
	for k, v := range config.Env {
		env[k] = v
	}
	id := cluster.GenerateJobID(hostID, req.JobID)
	env["FLYNN_APP_ID"] = req.App.ID
	env["FLYNN_APP_NAME"] = req.App.Name
	env["FLYNN_RELEASE_ID"] = req.Release.ID
	env["FLYNN_PROCESS_TYPE"] = config.Type
	env["FLYNN_JOB_ID"] = id
	metadata := make(map[string]string, len(req.App.Meta)+5)
	for k, v := range req.App.Meta {
		metadata[k] = v
	}
	metadata["flynn-controller.app"] = req.App.ID
	metadata["flynn-controller.app_name"] = req.App.Name
	metadata["flynn-controller.release"] = req.Release.ID
	metadata["flynn-controller.type"] = config.Type
	metadata["flynn-controller.job_request"] = req.ID
	job := &host.Job{
		ID:       id,
		Metadata: metadata,
		Config: host.ContainerConfig{
			Cmd:           config.Cmd,
			Env:           env,
			TTY:           config.TTY,
			Stdin:         config.Attach,
			DisableLog:    config.DisableLog,
			RequireAttach: config.Attach,
		},
		Resources: config.Resources,
	}
	if req.App.Meta["flynn-system-app"] == "true" {
		job.Partition = "system"
	}
	if len(config.Entrypoint) > 0 {
		job.Config.Entrypoint = config.Entrypoint
	}
	if artifact := req.ImageArtifact(); artifact != nil {
		job.ImageArtifact = artifact.HostArtifact()
	}
	if fileArtifacts := req.FileArtifacts(); len(fileArtifacts) > 0 {
		job.FileArtifacts = make([]*host.Artifact, len(fileArtifacts))
		for i, artifact := range fileArtifacts {
			job.FileArtifacts[i] = artifact.HostArtifact()
		}
	}
	return job
}

func ProvisionVolume(h VolumeCreator, job *host.Job) error {
	vol, err := h.CreateVolume("default")
	if err != nil {
		return err
	}
	job.Config.Volumes = []host.VolumeBinding{{
		Target:    "/data",
		VolumeID:  vol.ID,
		Writeable: true,
	}}
	return nil
}

func JobMetaFromMetadata(metadata map[string]string) map[string]string {
	jobMeta := make(map[string]string, len(metadata))
	for k, v := range metadata {
		if strings.HasPrefix(k, "flynn-controller.") {
			continue
		}
		jobMeta[k] = v
	}
	return jobMeta
}

type FormationKey struct {
	AppID, ReleaseID string
}

func NewFormationKey(appID, releaseID string) FormationKey {
	return FormationKey{AppID: appID, ReleaseID: releaseID}
}

func (f FormationKey) String() string {
	return fmt.Sprintf("%s:%s", f.AppID, f.ReleaseID)
}

func ExpandFormation(c ControllerClient, f *ct.Formation) (*ct.ExpandedFormation, error) {
	app, err := c.GetApp(f.AppID)
	if err != nil {
		return nil, fmt.Errorf("error getting app: %s", err)
	}

	release, err := c.GetRelease(f.ReleaseID)
	if err != nil {
		return nil, fmt.Errorf("error getting release: %s", err)
	}

	imageArtifact, err := c.GetArtifact(release.ImageArtifactID())
	if err != nil {
		return nil, fmt.Errorf("error getting image artifact: %s", err)
	}

	fileArtifacts := make([]*ct.Artifact, len(release.FileArtifactIDs()))
	for i, fileArtifactID := range release.FileArtifactIDs() {
		artifact, err := c.GetArtifact(fileArtifactID)
		if err != nil {
			return nil, fmt.Errorf("error getting file artifact: %s", err)
		}
		fileArtifacts[i] = artifact
	}

	procs := make(map[string]int)
	for typ, count := range f.Processes {
		procs[typ] = count
	}

	ef := &ct.ExpandedFormation{
		App:           app,
		Release:       release,
		ImageArtifact: imageArtifact,
		FileArtifacts: fileArtifacts,
		Processes:     procs,
		Tags:          f.Tags,
		UpdatedAt:     time.Now(),
	}
	if f.UpdatedAt != nil {
		ef.UpdatedAt = *f.UpdatedAt
	}
	return ef, nil
}

func ExpandJobRequest(c ControllerClient, req *ct.JobRequest) (*ct.ExpandedJobRequest, error) {
	app, err := c.GetApp(req.AppID)
	if err != nil {
		return nil, fmt.Errorf("error getting app: %s", err)
	}

	release, err := c.GetRelease(req.ReleaseID)
	if err != nil {
		return nil, fmt.Errorf("error getting release: %s", err)
	}

	artifacts, err := c.GetArtifacts(req.ArtifactIDs...)
	if err != nil {
		return nil, fmt.Errorf("error getting artifacts: %s", err)
	}

	return &ct.ExpandedJobRequest{
		ID:        req.ID,
		JobID:     req.JobID,
		State:     req.State,
		Config:    req.Config,
		App:       app,
		Release:   release,
		Artifacts: artifacts,
	}, nil
}

type VolumeCreator interface {
	CreateVolume(string) (*volume.Info, error)
}

type HostClient interface {
	VolumeCreator
	ID() string
	Tags() map[string]string
	AddJob(*host.Job) error
	GetJob(id string) (*host.ActiveJob, error)
	Attach(*host.AttachReq, bool) (cluster.AttachClient, error)
	StopJob(string) error
	ListJobs() (map[string]host.ActiveJob, error)
	StreamEvents(id string, ch chan *host.Event) (stream.Stream, error)
	GetStatus() (*host.HostStatus, error)
}

type ClusterClient interface {
	Host(string) (HostClient, error)
	Hosts() ([]HostClient, error)
	StreamHostEvents(chan *discoverd.Event) (stream.Stream, error)
}

type ControllerClient interface {
	GetApp(appID string) (*ct.App, error)
	GetRelease(releaseID string) (*ct.Release, error)
	GetArtifact(artifactID string) (*ct.Artifact, error)
	GetArtifacts(artifactIDs ...string) ([]*ct.Artifact, error)
	GetExpandedFormation(appID, releaseID string) (*ct.ExpandedFormation, error)
	CreateApp(app *ct.App) error
	CreateRelease(release *ct.Release) error
	CreateArtifact(artifact *ct.Artifact) error
	PutFormation(formation *ct.Formation) error
	StreamFormations(since *time.Time, ch chan<- *ct.ExpandedFormation) (stream.Stream, error)
	StreamJobRequests(ch chan *ct.JobRequest) (stream.Stream, error)
	AppList() ([]*ct.App, error)
	FormationListActive() ([]*ct.ExpandedFormation, error)
	PutJob(*ct.Job) error
	JobListActive() ([]*ct.Job, error)
	JobRequestListPending() ([]*ct.ExpandedJobRequest, error)
}

func ClusterClientWrapper(c *cluster.Client) clusterClientWrapper {
	return clusterClientWrapper{c}
}

type clusterClientWrapper struct {
	*cluster.Client
}

func (c clusterClientWrapper) Host(id string) (HostClient, error) {
	return c.Client.Host(id)
}

func (c clusterClientWrapper) Hosts() ([]HostClient, error) {
	hosts, err := c.Client.Hosts()
	if err != nil {
		return nil, err
	}
	res := make([]HostClient, len(hosts))
	for i, h := range hosts {
		res[i] = h
	}
	return res, nil
}

func (c clusterClientWrapper) StreamHostEvents(ch chan *discoverd.Event) (stream.Stream, error) {
	return c.Client.StreamHostEvents(ch)
}

var AppNamePattern = regexp.MustCompile(`^[a-z\d]+(-[a-z\d]+)*$`)

func FormationTagsEqual(a, b map[string]map[string]string) bool {
	if len(a) != len(b) {
		return false
	}
	for typ, tags := range b {
		for k, v := range tags {
			if w, ok := a[typ][k]; !ok || w != v {
				return false
			}
		}
	}
	return true
}
