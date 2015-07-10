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
	apps       []*ct.App
}

func NewFakeControllerClient(appID string, release *ct.Release, artifact *ct.Artifact, processes map[string]int) *FakeControllerClient {
	return &FakeControllerClient{
		releases:  map[string]*ct.Release{release.ID: release},
		artifacts: map[string]*ct.Artifact{artifact.ID: artifact},
		formations: map[string]map[string]*ct.Formation{
			appID: {
				release.ID: {AppID: appID, ReleaseID: release.ID, Processes: processes},
			},
		},
		apps: []*ct.App{
			{
				ID:   appID,
				Name: appID,
			},
		},
		jobs: make(map[string]*ct.Job),
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

func (c *FakeControllerClient) AppList() ([]*ct.App, error) {
	return c.apps, nil
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
