package main

import (
	"fmt"
	"os"
	"time"

	. "github.com/flynn/flynn/Godeps/_workspace/src/github.com/flynn/go-check"
	ct "github.com/flynn/flynn/controller/types"
	"github.com/flynn/flynn/host/types"
	"github.com/flynn/flynn/pkg/random"
)

func (s *S) TestFormationStreaming(c *C) {
	before := time.Now()
	release := s.createTestRelease(c, &ct.Release{})
	app := s.createTestApp(c, &ct.App{Name: "streamtest-existing"})
	s.createTestFormation(c, &ct.Formation{ReleaseID: release.ID, AppID: app.ID})

	updates := make(chan *ct.ExpandedFormation)
	streamCtrl, connectErr := s.c.StreamFormations(&before, updates)
	c.Assert(connectErr, IsNil)
	defer streamCtrl.Close()

	var existingFound bool
	for f := range updates {
		if f.App == nil {
			break
		}
		if f.Release.ID == release.ID {
			existingFound = true
		}
	}
	c.Assert(streamCtrl.Err(), IsNil)
	c.Assert(existingFound, Equals, true)

	release = s.createTestRelease(c, &ct.Release{
		Processes: map[string]ct.ProcessType{"foo": {}},
	})
	app = s.createTestApp(c, &ct.App{Name: "streamtest"})
	formation := s.createTestFormation(c, &ct.Formation{
		ReleaseID: release.ID,
		AppID:     app.ID,
		Processes: map[string]int{"foo": 1},
	})
	defer s.deleteTestFormation(formation)

	var out *ct.ExpandedFormation
	select {
	case out = <-updates:
	case <-time.After(time.Second):
		c.Fatal("timed out waiting for create")
	}
	c.Assert(streamCtrl.Err(), IsNil)
	c.Assert(out.Release, DeepEquals, release)
	c.Assert(out.App, DeepEquals, app)
	c.Assert(out.Processes, DeepEquals, formation.Processes)
	c.Assert(out.ImageArtifact.CreatedAt, Not(IsNil))
	c.Assert(out.ImageArtifact.ID, Equals, release.ImageArtifactID())

	c.Assert(s.c.DeleteFormation(app.ID, release.ID), IsNil)

	select {
	case out = <-updates:
	case <-time.After(time.Second):
		c.Fatal("timed out waiting for delete")
	}
	c.Assert(streamCtrl.Err(), IsNil)
	c.Assert(out.Release, DeepEquals, release)
	c.Assert(out.App, DeepEquals, app)
	c.Assert(out.Processes, IsNil)
}

func (s *S) TestFormationListActive(c *C) {
	app1 := s.createTestApp(c, &ct.App{})
	app2 := s.createTestApp(c, &ct.App{})
	imageArtifact := s.createTestArtifact(c, &ct.Artifact{Type: host.ArtifactTypeDocker})
	fileArtifact := s.createTestArtifact(c, &ct.Artifact{Type: host.ArtifactTypeFile})

	createFormation := func(app *ct.App, procs map[string]int) *ct.ExpandedFormation {
		release := &ct.Release{
			ArtifactIDs: []string{imageArtifact.ID, fileArtifact.ID},
			Processes:   make(map[string]ct.ProcessType, len(procs)),
		}
		for typ := range procs {
			release.Processes[typ] = ct.ProcessType{}
		}
		s.createTestRelease(c, release)
		s.createTestFormation(c, &ct.Formation{
			AppID:     app.ID,
			ReleaseID: release.ID,
			Processes: procs,
		})
		return &ct.ExpandedFormation{
			App:           app,
			Release:       release,
			ImageArtifact: imageArtifact,
			FileArtifacts: []*ct.Artifact{fileArtifact},
			Processes:     procs,
		}
	}

	formations := []*ct.ExpandedFormation{
		createFormation(app1, map[string]int{"web": 0}),
		createFormation(app1, map[string]int{"web": 1}),
		createFormation(app2, map[string]int{"web": 0, "worker": 0}),
		createFormation(app2, map[string]int{"web": 0, "worker": 1}),
		createFormation(app2, map[string]int{"web": 1, "worker": 2}),
	}

	list, err := s.c.FormationListActive()
	c.Assert(err, IsNil)
	c.Assert(list, HasLen, 3)

	// check that we only get the formations with a non-zero process count,
	// most recently updated first
	expected := []*ct.ExpandedFormation{formations[4], formations[3], formations[1]}
	for i, f := range expected {
		actual := list[i]
		c.Assert(actual.App.ID, Equals, f.App.ID)
		c.Assert(actual.Release.ID, Equals, f.Release.ID)
		c.Assert(actual.ImageArtifact.ID, Equals, f.ImageArtifact.ID)
		c.Assert(actual.FileArtifacts, DeepEquals, f.FileArtifacts)
		c.Assert(actual.Processes, DeepEquals, f.Processes)
	}
}

func (s *S) TestFormationStreamingInterrupted(c *C) {
	before := time.Now()
	appRepo := NewAppRepo(s.hc.db, os.Getenv("DEFAULT_ROUTE_DOMAIN"), s.hc.rc)
	releaseRepo := NewReleaseRepo(s.hc.db)
	artifactRepo := NewArtifactRepo(s.hc.db)
	formationRepo := NewFormationRepo(s.hc.db, appRepo, releaseRepo, artifactRepo)

	artifact := &ct.Artifact{Type: host.ArtifactTypeDocker, URI: fmt.Sprintf("https://example.com/%s", random.String(8))}
	c.Assert(artifactRepo.Add(artifact), IsNil)

	release := &ct.Release{ArtifactIDs: []string{artifact.ID}}
	c.Assert(releaseRepo.Add(release), IsNil)

	app := &ct.App{Name: "streamtest-interrupted"}
	c.Assert(appRepo.Add(app), IsNil)

	formation := &ct.Formation{ReleaseID: release.ID, AppID: app.ID}
	c.Assert(formationRepo.Add(formation), IsNil)

	ch := make(chan *ct.ExpandedFormation)
	updated := make(chan struct{})

	_, err := formationRepo.Subscribe(ch, before, updated)
	c.Assert(err, IsNil)

	// simulate scenario where we have not completed `sendUpdatedSince` but the channel for a subscription
	// is closed, by example due to an error listening on table `formations` that triggered `unsubscribeAll`.
	formationRepo.unsubscribeAll()

	// wait until `sendUpdateSince` finishes at which point it will not longer send to the (now closed) channel.
	select {
	case <-updated:
	case <-time.After(5 * time.Second):
		c.Fatal("timed out waiting for sendUpdatedSince to finish")
	}
}
