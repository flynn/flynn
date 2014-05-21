package main

import (
	"testing"
	"time"

	"github.com/flynn/flynn-controller/client"
	tu "github.com/flynn/flynn-controller/testutils"
	ct "github.com/flynn/flynn-controller/types"
	"github.com/flynn/flynn-host/types"
	. "github.com/titanous/gocheck"
)

// Hook gocheck up to the "go test" runner
func Test(t *testing.T) { TestingT(t) }

type S struct{}

var _ = Suite(&S{})

type fakeControllerClient struct {
	releases   map[string]*ct.Release
	artifacts  map[string]*ct.Artifact
	formations map[formationKey]*ct.Formation
}

func (c *fakeControllerClient) GetRelease(releaseID string) (*ct.Release, error) {
	if release, ok := c.releases[releaseID]; ok {
		return release, nil
	}
	return nil, controller.ErrNotFound
}

func (c *fakeControllerClient) GetArtifact(artifactID string) (*ct.Artifact, error) {
	if artifact, ok := c.artifacts[artifactID]; ok {
		return artifact, nil
	}
	return nil, controller.ErrNotFound
}

func (c *fakeControllerClient) GetFormation(appID, releaseID string) (*ct.Formation, error) {
	if formation, ok := c.formations[formationKey{appID, releaseID}]; ok {
		return formation, nil
	}
	return nil, controller.ErrNotFound
}

func (c *fakeControllerClient) StreamFormations(since *time.Time) (<-chan *ct.ExpandedFormation, *error) {
	return make(chan *ct.ExpandedFormation), nil
}

func (s *S) TestSyncCluster(c *C) {
	appID := "app0"
	artifact := &ct.Artifact{ID: "artifact0"}
	release := &ct.Release{ID: "release0", ArtifactID: artifact.ID}
	processes := map[string]int{"web": 1}

	cc := &fakeControllerClient{
		releases:  map[string]*ct.Release{release.ID: release},
		artifacts: map[string]*ct.Artifact{artifact.ID: artifact},
		formations: map[formationKey]*ct.Formation{
			formationKey{appID, release.ID}: {AppID: appID, ReleaseID: release.ID, Processes: processes},
		},
	}

	cl := tu.NewFakeCluster()
	cl.SetHosts(map[string]host.Host{"host0": {
		ID: "host0",
		Jobs: []*host.Job{
			{ID: "job0", Attributes: map[string]string{"flynn-controller.app": appID, "flynn-controller.release": release.ID, "flynn-controller.type": "web"}},
		},
	}})

	cx := newContext(cc, cl)
	cx.syncCluster()

	c.Assert(cx.formations.Len(), Equals, 1)
	formation := cx.formations.Get(appID, release.ID)
	c.Assert(formation.AppID, Equals, appID)
	c.Assert(formation.Release, DeepEquals, release)
	c.Assert(formation.Artifact, DeepEquals, artifact)
	c.Assert(formation.Processes, DeepEquals, processes)
}
