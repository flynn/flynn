package testutils

import (
	"github.com/flynn/flynn/controller/client"
	ct "github.com/flynn/flynn/controller/types"
	"github.com/flynn/flynn/controller/utils"
)

type FakeControllerClient struct {
	releases   map[string]*ct.Release
	artifacts  map[string]*ct.Artifact
	formations map[utils.FormationKey]*ct.Formation
	jobs       map[string]*ct.Job
}

func NewFakeControllerClient(appID string, release *ct.Release, artifact *ct.Artifact, processes map[string]int) *FakeControllerClient {
	return &FakeControllerClient{
		releases:  map[string]*ct.Release{release.ID: release},
		artifacts: map[string]*ct.Artifact{artifact.ID: artifact},
		formations: map[utils.FormationKey]*ct.Formation{
			utils.NewFormationKey(appID, release.ID): {AppID: appID, ReleaseID: release.ID, Processes: processes},
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
	if formation, ok := c.formations[utils.NewFormationKey(appID, releaseID)]; ok {
		return formation, nil
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
