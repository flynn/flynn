package testutils

import (
	"github.com/flynn/flynn/controller/client"
	ct "github.com/flynn/flynn/controller/types"
)

type FakeControllerClient struct {
	releases   map[string]*ct.Release
	artifacts  map[string]*ct.Artifact
	formations map[string]map[string]*ct.Formation
	jobs       map[string]*ct.Job
	apps       map[string]*ct.App
}

func NewFakeControllerClient() *FakeControllerClient {
	return &FakeControllerClient{
		releases:   make(map[string]*ct.Release),
		artifacts:  make(map[string]*ct.Artifact),
		formations: make(map[string]map[string]*ct.Formation),
		apps:       make(map[string]*ct.App),
		jobs:       make(map[string]*ct.Job),
	}
}

func (c *FakeControllerClient) GetRelease(releaseID string) (*ct.Release, error) {
	if release, ok := c.releases[releaseID]; ok {
		return release, nil
	}
	return nil, controller.ErrNotFound
}

func (c *FakeControllerClient) GetArtifact(artifactID string) (*ct.Artifact, error) {
	if artifact, ok := c.artifacts[artifactID]; ok {
		return artifact, nil
	}
	return nil, controller.ErrNotFound
}

func (c *FakeControllerClient) GetFormation(appID, releaseID string) (*ct.Formation, error) {
	if releases, ok := c.formations[appID]; ok {
		if formation, ok := releases[releaseID]; ok {
			return formation, nil
		}
	}
	return nil, controller.ErrNotFound
}

func (c *FakeControllerClient) GetApp(appID string) (*ct.App, error) {
	if app, ok := c.apps[appID]; ok {
		return app, nil
	}
	return nil, controller.ErrNotFound
}

func (c *FakeControllerClient) CreateApp(app *ct.App) error {
	c.apps[app.ID] = app
	return nil
}

func (c *FakeControllerClient) CreateRelease(release *ct.Release) error {
	c.releases[release.ID] = release
	return nil
}

func (c *FakeControllerClient) CreateArtifact(artifact *ct.Artifact) error {
	c.artifacts[artifact.ID] = artifact
	return nil
}

func (c *FakeControllerClient) PutFormation(formation *ct.Formation) error {
	releases, ok := c.formations[formation.AppID]
	if !ok {
		releases = make(map[string]*ct.Formation)
		c.formations[formation.AppID] = releases
	}
	releases[formation.ReleaseID] = formation

	return nil
}

func (c *FakeControllerClient) AppList() ([]*ct.App, error) {
	apps := make([]*ct.App, 0, len(c.apps))
	for _, app := range c.apps {
		apps = append(apps, app)
	}
	return apps, nil
}

func (c *FakeControllerClient) FormationList(appID string) ([]*ct.Formation, error) {
	if releases, ok := c.formations[appID]; ok {
		formations := make([]*ct.Formation, 0, len(releases))
		for _, f := range releases {
			formations = append(formations, f)
		}
		return formations, nil
	}
	return nil, controller.ErrNotFound
}

func (c *FakeControllerClient) PutJob(job *ct.Job) error {
	c.jobs[job.ID] = job
	return nil
}

func NewRelease(id string, artifact *ct.Artifact, processes map[string]int) *ct.Release {
	processTypes := make(map[string]ct.ProcessType, len(processes))
	for t := range processes {
		processTypes[t] = ct.ProcessType{Cmd: []string{"start", t}}
	}

	return &ct.Release{
		ID:         id,
		ArtifactID: artifact.ID,
		Processes:  processTypes,
	}
}
