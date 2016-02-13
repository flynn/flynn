package testutils

import (
	"errors"
	"time"

	"github.com/flynn/flynn/controller/client"
	ct "github.com/flynn/flynn/controller/types"
	"github.com/flynn/flynn/controller/utils"
	"github.com/flynn/flynn/pkg/stream"
)

type FakeControllerClient struct {
	releases         map[string]*ct.Release
	artifacts        map[string]*ct.Artifact
	formations       map[string]map[string]*ct.Formation
	formationStreams map[chan<- *ct.ExpandedFormation]struct{}
	jobs             map[string]*ct.Job
	apps             map[string]*ct.App
}

func NewFakeControllerClient() *FakeControllerClient {
	return &FakeControllerClient{
		releases:         make(map[string]*ct.Release),
		artifacts:        make(map[string]*ct.Artifact),
		formations:       make(map[string]map[string]*ct.Formation),
		formationStreams: make(map[chan<- *ct.ExpandedFormation]struct{}),
		apps:             make(map[string]*ct.App),
		jobs:             make(map[string]*ct.Job),
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

func (c *FakeControllerClient) GetExpandedFormation(appID, releaseID string) (*ct.ExpandedFormation, error) {
	app, ok := c.apps[appID]
	if !ok {
		return nil, controller.ErrNotFound
	}
	release, ok := c.releases[releaseID]
	if !ok {
		return nil, controller.ErrNotFound
	}
	releases, ok := c.formations[appID]
	if !ok {
		return nil, controller.ErrNotFound
	}
	formation, ok := releases[releaseID]
	if !ok {
		return nil, controller.ErrNotFound
	}
	procs := make(map[string]int, len(formation.Processes))
	for typ, n := range formation.Processes {
		procs[typ] = n
	}
	return &ct.ExpandedFormation{
		App:           app,
		Release:       release,
		ImageArtifact: c.artifacts[release.ImageArtifactID],
		Processes:     procs,
	}, nil
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

	for ch := range c.formationStreams {
		ef, err := utils.ExpandFormation(c, formation)
		if err == nil {
			ch <- ef
		}
	}

	return nil
}

func (c *FakeControllerClient) AppList() ([]*ct.App, error) {
	apps := make([]*ct.App, 0, len(c.apps))
	for _, app := range c.apps {
		apps = append(apps, app)
	}
	return apps, nil
}

func (c *FakeControllerClient) FormationListActive() ([]*ct.ExpandedFormation, error) {
	var formations []*ct.ExpandedFormation
	for appID, releases := range c.formations {
		app, ok := c.apps[appID]
		if !ok {
			continue
		}
		for releaseID, formation := range releases {
			count := 0
			procs := make(map[string]int, len(formation.Processes))
			for typ, n := range formation.Processes {
				count += n
				procs[typ] = n
			}
			if count == 0 {
				continue
			}
			release, ok := c.releases[releaseID]
			if !ok {
				continue
			}
			artifact := c.artifacts[release.ImageArtifactID]
			formations = append(formations, &ct.ExpandedFormation{
				App:           app,
				Release:       release,
				ImageArtifact: artifact,
				Processes:     procs,
			})
		}
	}
	return formations, nil
}

func (c *FakeControllerClient) StreamFormations(since *time.Time, ch chan<- *ct.ExpandedFormation) (stream.Stream, error) {
	if _, ok := c.formationStreams[ch]; ok {
		return nil, errors.New("Already streaming to that channel")
	}

	for _, releases := range c.formations {
		for _, f := range releases {
			ef, err := utils.ExpandFormation(c, f)
			if err == nil {
				ch <- ef
			}
		}
	}
	ch <- &ct.ExpandedFormation{}

	c.formationStreams[ch] = struct{}{}
	return &FormationStream{
		cc: c,
		ch: ch,
	}, nil
}

func (c *FakeControllerClient) PutJob(job *ct.Job) error {
	c.jobs[job.ID] = job
	return nil
}

func (c *FakeControllerClient) JobListActive() ([]*ct.Job, error) {
	list := make([]*ct.Job, 0, len(c.jobs))
	for _, job := range c.jobs {
		if job.State == ct.JobStateStarting || job.State == ct.JobStateUp {
			list = append(list, job)
		}
	}
	return list, nil
}

func NewRelease(id string, artifact *ct.Artifact, processes map[string]int) *ct.Release {
	return NewReleaseOmni(id, artifact, processes, false)
}

func NewReleaseOmni(id string, artifact *ct.Artifact, processes map[string]int, omni bool) *ct.Release {
	processTypes := make(map[string]ct.ProcessType, len(processes))
	for t := range processes {
		processTypes[t] = ct.ProcessType{
			Cmd:  []string{"start", t},
			Omni: omni,
		}
	}

	return &ct.Release{
		ID:              id,
		ImageArtifactID: artifact.ID,
		Processes:       processTypes,
	}
}

type FormationStream struct {
	cc *FakeControllerClient
	ch chan<- *ct.ExpandedFormation
}

func (fs *FormationStream) Close() error {
	delete(fs.cc.formationStreams, fs.ch)
	close(fs.ch)
	return nil
}

func (fs *FormationStream) Err() error {
	return nil
}
