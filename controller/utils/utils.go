package utils

import (
	"strings"

	ct "github.com/flynn/flynn/controller/types"
	"github.com/flynn/flynn/host/types"
	"github.com/flynn/flynn/host/volume"
	"github.com/flynn/flynn/pkg/cluster"
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
}

type ClusterClient interface {
	Host(string) (HostClient, error)
	Hosts() ([]HostClient, error)
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
	AppList() ([]*ct.App, error)
	FormationList(appID string) ([]*ct.Formation, error)
	PutJob(*ct.Job) error
}
