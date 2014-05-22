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
	stream     chan *ct.ExpandedFormation
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
	return c.stream, nil
}

type formationUpdate struct {
	processes map[string]int
}

func (f *formationUpdate) jobCount() int {
	var count int
	for _, num := range f.processes {
		count += num
	}
	return count
}

func waitForFormationEvent(events <-chan *FormationEvent, c *C) {
	select {
	case <-events:
	case <-time.After(time.Second):
		c.Fatal("timed out waiting for Formation event")
	}
}

func (s *S) TestWatchFormations(c *C) {
	// Create a fake cluster with an existing running formation
	appID := "existing-app"
	artifact := &ct.Artifact{ID: "existing-artifact"}
	release := &ct.Release{
		ID:         "existing-release",
		ArtifactID: artifact.ID,
		Processes:  map[string]ct.ProcessType{"web": ct.ProcessType{Cmd: []string{"start", "web"}}},
	}
	processes := map[string]int{"web": 1}
	stream := make(chan *ct.ExpandedFormation)

	cc := &fakeControllerClient{
		releases:  map[string]*ct.Release{release.ID: release},
		artifacts: map[string]*ct.Artifact{artifact.ID: artifact},
		formations: map[formationKey]*ct.Formation{
			formationKey{appID, release.ID}: {AppID: appID, ReleaseID: release.ID, Processes: processes},
		},
		stream: stream,
	}

	hostID := "host0"
	jobID := "existing-job"
	existingJob := &host.Job{
		ID: jobID,
		Attributes: map[string]string{
			"flynn-controller.app":     appID,
			"flynn-controller.release": release.ID,
			"flynn-controller.type":    "web",
		},
	}

	cl := tu.NewFakeCluster()
	cl.SetHosts(map[string]host.Host{hostID: host.Host{ID: hostID, Jobs: []*host.Job{existingJob}}})
	cl.SetHostClient(hostID, tu.NewFakeHostClient(hostID))

	events := make(chan *FormationEvent)
	cx := newContext(cc, cl)
	go cx.watchFormations(events)

	// Give the scheduler chance to sync with the cluster, then check it's in sync
	waitForFormationEvent(events, c)
	c.Assert(cx.formations.Len(), Equals, 1)
	formation := cx.formations.Get(appID, release.ID)
	c.Assert(formation.AppID, Equals, appID)
	c.Assert(formation.Release, DeepEquals, release)
	c.Assert(formation.Artifact, DeepEquals, artifact)
	c.Assert(formation.Processes, DeepEquals, processes)
	c.Assert(cx.jobs.Len(), Equals, 1)
	job := cx.jobs.Get(hostID, jobID)
	c.Assert(job.Type, Equals, "web")

	f := &ct.ExpandedFormation{
		App: &ct.App{ID: "app0"},
		Release: &ct.Release{
			ID:         "release0",
			ArtifactID: "artifact0",
			Processes: map[string]ct.ProcessType{
				"web":    ct.ProcessType{Cmd: []string{"start", "web"}},
				"worker": ct.ProcessType{Cmd: []string{"start", "worker"}},
			},
		},
		Artifact: &ct.Artifact{ID: "artifact0", Type: "docker", URI: "docker://foo/bar"},
	}

	updates := []*formationUpdate{
		&formationUpdate{processes: map[string]int{"web": 2}},
		&formationUpdate{processes: map[string]int{"web": 3, "worker": 1}},
		&formationUpdate{processes: map[string]int{"web": 1}},
	}

	for _, u := range updates {
		f.Processes = u.processes
		stream <- f
		waitForFormationEvent(events, c)

		c.Assert(cx.formations.Len(), Equals, 2)
		formation = cx.formations.Get(f.App.ID, f.Release.ID)
		c.Assert(formation.AppID, Equals, f.App.ID)
		c.Assert(formation.Release, DeepEquals, f.Release)
		c.Assert(formation.Artifact, DeepEquals, f.Artifact)
		c.Assert(formation.Processes, DeepEquals, f.Processes)

		c.Assert(cx.jobs.Len(), Equals, u.jobCount()+1)
		host := cl.GetHost(hostID)
		c.Assert(len(host.Jobs), Equals, u.jobCount()+1)

		processes := make(map[string]int, len(u.processes))
		for _, job := range host.Jobs {
			jobType := job.Attributes["flynn-controller.type"]
			processes[jobType]++
		}
		// Ignore the existing web job
		processes["web"]--
		c.Assert(processes, DeepEquals, u.processes)
	}
}
