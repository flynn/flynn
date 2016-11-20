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

func JobConfig(f *ct.ExpandedFormation, name, hostID string, uuid string) *host.Job {
	t := f.Release.Processes[name]

	var entrypoint ct.ImageEntrypoint
	if e := GetEntrypoint(f.Artifacts, name); e != nil {
		entrypoint = *e
	}

	env := make(map[string]string, len(entrypoint.Env)+len(f.Release.Env)+len(t.Env)+5)
	for k, v := range entrypoint.Env {
		env[k] = v
	}
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
	metadata := make(map[string]string, len(f.App.Meta)+5)
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
			Args:             entrypoint.Args,
			Env:              env,
			WorkingDir:       entrypoint.WorkingDir,
			Uid:              entrypoint.Uid,
			Gid:              entrypoint.Gid,
			HostNetwork:      t.HostNetwork,
			Mounts:           t.Mounts,
			WriteableCgroups: t.WriteableCgroups,
		},
		Resurrect: t.Resurrect,
		Resources: t.Resources,
	}
	if len(t.Args) > 0 {
		job.Config.Args = t.Args
	}
	if len(t.LinuxCapabilities) > 0 {
		job.Config.LinuxCapabilities = &t.LinuxCapabilities
	}
	if len(t.AllowedDevices) > 0 {
		job.Config.AllowedDevices = &t.AllowedDevices
	}

	// job.Config.Args may be empty if restoring from an old backup which
	// still uses the deprecated Entrypoint / Cmd fields
	if len(job.Config.Args) == 0 {
		job.Config.Args = append(t.DeprecatedEntrypoint, t.DeprecatedCmd...)
	}

	SetupMountspecs(job, f.Artifacts)
	if f.App.Meta["flynn-system-app"] == "true" {
		job.Partition = "system"
	}
	job.Config.Ports = make([]host.Port, len(t.Ports))
	for i, p := range t.Ports {
		job.Config.Ports[i].Proto = p.Proto
		job.Config.Ports[i].Port = p.Port
		job.Config.Ports[i].Service = p.Service
	}
	return job
}

// GetEntrypoint returns an image entrypoint for a process type from a list of
// artifacts, first iterating through them and returning any entrypoint having
// the exact type, then iterating through them and returning the artifact's
// default entrypoint if it has one.
//
// The artifacts are traversed in reverse order so that entrypoints in the
// image being overlayed at the top are considered first.
func GetEntrypoint(artifacts []*ct.Artifact, typ string) *ct.ImageEntrypoint {
	for i := len(artifacts) - 1; i >= 0; i-- {
		artifact := artifacts[i]
		if artifact.Type != ct.ArtifactTypeFlynn {
			continue
		}
		if e, ok := artifact.Manifest().Entrypoints[typ]; ok {
			return e
		}
	}
	for i := len(artifacts) - 1; i >= 0; i-- {
		artifact := artifacts[i]
		if artifact.Type != ct.ArtifactTypeFlynn {
			continue
		}
		if e := artifact.Manifest().DefaultEntrypoint(); e != nil {
			return e
		}
	}
	return nil
}

// SetupMountspecs populates job.Mountspecs using the layers from a list of
// Flynn image artifacts, expecting each artifact to have a single rootfs entry
// containing squashfs layers
func SetupMountspecs(job *host.Job, artifacts []*ct.Artifact) {
	for _, artifact := range artifacts {
		if artifact.Type != ct.ArtifactTypeFlynn {
			continue
		}
		if len(artifact.Manifest().Rootfs) != 1 {
			continue
		}
		rootfs := artifact.Manifest().Rootfs[0]
		for _, layer := range rootfs.Layers {
			if layer.Type != ct.ImageLayerTypeSquashfs {
				continue
			}
			job.Mountspecs = append(job.Mountspecs, &host.Mountspec{
				Type:   host.MountspecTypeSquashfs,
				ID:     layer.ID,
				URL:    artifact.LayerURL(layer),
				Size:   layer.Length,
				Hashes: layer.Hashes,
				Meta:   artifact.Meta,
			})
		}
	}
}

func ProvisionVolume(req *ct.VolumeReq, h VolumeCreator, job *host.Job) (*volume.Info, error) {
	vol, err := h.CreateVolume("default")
	if err != nil {
		return nil, err
	}
	job.Config.Volumes = []host.VolumeBinding{{
		Target:       req.Path,
		VolumeID:     vol.ID,
		Writeable:    true,
		DeleteOnStop: req.DeleteOnStop,
	}}
	return vol, nil
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

	artifacts := make([]*ct.Artifact, len(release.ArtifactIDs))
	for i, artifactID := range release.ArtifactIDs {
		artifact, err := c.GetArtifact(artifactID)
		if err != nil {
			return nil, fmt.Errorf("error getting file artifact: %s", err)
		}
		artifacts[i] = artifact
	}

	procs := make(map[string]int)
	for typ, count := range f.Processes {
		procs[typ] = count
	}

	ef := &ct.ExpandedFormation{
		App:       app,
		Release:   release,
		Artifacts: artifacts,
		Processes: procs,
		Tags:      f.Tags,
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
	Tags() map[string]string
	AddJob(*host.Job) error
	GetJob(id string) (*host.ActiveJob, error)
	Attach(*host.AttachReq, bool) (cluster.AttachClient, error)
	StopJob(string) error
	DiscoverdDeregisterJob(string) error
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
	GetExpandedFormation(appID, releaseID string) (*ct.ExpandedFormation, error)
	CreateApp(app *ct.App) error
	CreateRelease(release *ct.Release) error
	CreateArtifact(artifact *ct.Artifact) error
	PutFormation(formation *ct.Formation) error
	StreamFormations(since *time.Time, ch chan<- *ct.ExpandedFormation) (stream.Stream, error)
	AppList() ([]*ct.App, error)
	FormationListActive() ([]*ct.ExpandedFormation, error)
	PutJob(*ct.Job) error
	JobListActive() ([]*ct.Job, error)
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
