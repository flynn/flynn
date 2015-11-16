package utils

import (
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
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

func JobConfig(f *ct.ExpandedFormation, name, hostID string) *host.Job {
	t := f.Release.Processes[name]
	env := make(map[string]string, len(f.Release.Env)+len(t.Env)+4)
	for k, v := range f.Release.Env {
		env[k] = v
	}
	for k, v := range t.Env {
		env[k] = v
	}
	id := cluster.GenerateJobID(hostID)
	env["FLYNN_APP_ID"] = f.App.ID
	env["FLYNN_APP_NAME"] = f.App.Name
	env["FLYNN_RELEASE_ID"] = f.Release.ID
	env["FLYNN_PROCESS_TYPE"] = name
	env["FLYNN_JOB_ID"] = id
	job := &host.Job{
		ID: id,
		Metadata: map[string]string{
			"flynn-controller.app":      f.App.ID,
			"flynn-controller.app_name": f.App.Name,
			"flynn-controller.release":  f.Release.ID,
			"flynn-controller.type":     name,
		},
		Artifact: host.Artifact{
			Type: f.Artifact.Type,
			URI:  f.Artifact.URI,
		},
		Config: host.ContainerConfig{
			Cmd:         t.Cmd,
			Env:         env,
			HostNetwork: t.HostNetwork,
		},
		Resurrect: t.Resurrect,
		Resources: t.Resources,
	}
	if len(t.Entrypoint) > 0 {
		job.Config.Entrypoint = t.Entrypoint
	}
	job.Config.Ports = make([]host.Port, len(t.Ports))
	for i, p := range t.Ports {
		job.Config.Ports[i].Proto = p.Proto
		job.Config.Ports[i].Port = p.Port
		job.Config.Ports[i].Service = p.Service
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

func ExpandFormation(c ControllerClient, f *ct.Formation) (*ct.ExpandedFormation, error) {
	app, err := c.GetApp(f.AppID)
	if err != nil {
		return nil, fmt.Errorf("error getting app: %s", err)
	}

	release, err := c.GetRelease(f.ReleaseID)
	if err != nil {
		return nil, fmt.Errorf("error getting release: %s", err)
	}

	artifact, err := c.GetArtifact(release.ArtifactID)
	if err != nil {
		return nil, fmt.Errorf("error getting artifact: %s", err)
	}

	procs := make(map[string]int)
	for typ, count := range f.Processes {
		procs[typ] = count
	}

	ef := &ct.ExpandedFormation{
		App:       app,
		Release:   release,
		Artifact:  artifact,
		Processes: procs,
		UpdatedAt: time.Now(),
	}
	if f.UpdatedAt != nil {
		ef.UpdatedAt = *f.UpdatedAt
	}
	return ef, nil
}

type VolumeCreator interface {
	CreateVolume(string) (*volume.Info, error)
}

type HostClient interface {
	VolumeCreator
	ID() string
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
	GetFormation(appID, releaseID string) (*ct.Formation, error)
	CreateApp(app *ct.App) error
	CreateRelease(release *ct.Release) error
	CreateArtifact(artifact *ct.Artifact) error
	PutFormation(formation *ct.Formation) error
	StreamFormations(since *time.Time, ch chan<- *ct.ExpandedFormation) (stream.Stream, error)
	AppList() ([]*ct.App, error)
	FormationListActive() ([]*ct.ExpandedFormation, error)
	PutJob(*ct.Job) error
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

func ParseBasicAuth(h http.Header) (username, password string, err error) {
	s := strings.SplitN(h.Get("Authorization"), " ", 2)

	if len(s) != 2 {
		return "", "", errors.New("failed to parse authentication string")
	}
	if s[0] != "Basic" {
		return "", "", fmt.Errorf("authorization scheme is %v, not Basic", s[0])
	}

	c, err := base64.StdEncoding.DecodeString(s[1])
	if err != nil {
		return "", "", errors.New("failed to parse base64 basic credentials")
	}

	s = strings.SplitN(string(c), ":", 2)
	if len(s) != 2 {
		return "", "", errors.New("failed to parse basic credentials")
	}

	return s[0], s[1], nil
}
