package main

import (
	"io"
	"io/ioutil"
	"strings"

	. "github.com/flynn/flynn/Godeps/_workspace/src/github.com/flynn/go-check"
	tu "github.com/flynn/flynn/controller/testutils"
	ct "github.com/flynn/flynn/controller/types"
	"github.com/flynn/flynn/host/types"
	"github.com/flynn/flynn/pkg/cluster"
	"github.com/flynn/flynn/pkg/random"
)

func (s *S) createTestJob(c *C, in *ct.Job) *ct.Job {
	c.Assert(s.c.PutJob(in), IsNil)
	return in
}

func (s *S) TestJobList(c *C) {
	app := s.createTestApp(c, &ct.App{Name: "job-list"})
	release := s.createTestRelease(c, &ct.Release{})
	s.createTestFormation(c, &ct.Formation{ReleaseID: release.ID, AppID: app.ID})
	s.createTestJob(c, &ct.Job{ID: "host0-job0", AppID: app.ID, ReleaseID: release.ID, Type: "web", State: "starting", Meta: map[string]string{"some": "info"}})

	list, err := s.c.JobList(app.ID)
	c.Assert(err, IsNil)
	c.Assert(len(list), Equals, 1)
	job := list[0]
	c.Assert(job.ID, Equals, "host0-job0")
	c.Assert(job.AppID, Equals, app.ID)
	c.Assert(job.ReleaseID, Equals, release.ID)
	c.Assert(job.Meta, DeepEquals, map[string]string{"some": "info"})
}

func (s *S) TestJobGet(c *C) {
	app := s.createTestApp(c, &ct.App{Name: "job-get"})
	release := s.createTestRelease(c, &ct.Release{})
	s.createTestFormation(c, &ct.Formation{ReleaseID: release.ID, AppID: app.ID})
	jobID := s.createTestJob(c, &ct.Job{ID: "host0-job1", AppID: app.ID, ReleaseID: release.ID, Type: "web", State: "starting", Meta: map[string]string{"some": "info"}}).ID

	job, err := s.c.GetJob(app.ID, jobID)
	c.Assert(err, IsNil)
	c.Assert(job.ID, Equals, "host0-job1")
	c.Assert(job.AppID, Equals, app.ID)
	c.Assert(job.ReleaseID, Equals, release.ID)
	c.Assert(job.Meta, DeepEquals, map[string]string{"some": "info"})
}

func (s *S) TestJobStateTransition(c *C) {
	app := s.createTestApp(c, &ct.App{Name: "job-state-transition"})
	release := s.createTestRelease(c, &ct.Release{})
	s.createTestFormation(c, &ct.Formation{ReleaseID: release.ID, AppID: app.ID})
	job := s.createTestJob(c, &ct.Job{ID: "host0-job2", AppID: app.ID, ReleaseID: release.ID, Type: "web", State: "starting"})

	job.State = "up"
	c.Assert(s.c.PutJob(job), IsNil)

	// duplicates are ignored
	c.Assert(s.c.PutJob(job), IsNil)

	job.State = "starting"
	c.Assert(s.c.PutJob(job), ErrorMatches, ".*invalid job state transition.*")

	job.State = "down"
	c.Assert(s.c.PutJob(job), IsNil)

	job.State = "up"
	c.Assert(s.c.PutJob(job), ErrorMatches, ".*invalid job state transition.*")
}

func newFakeLog(r io.Reader) *fakeLog {
	return &fakeLog{r}
}

type fakeLog struct {
	io.Reader
}

func (l *fakeLog) Close() error { return nil }
func (l *fakeLog) Write([]byte) (int, error) {
	return 0, io.ErrUnexpectedEOF
}

func (s *S) TestKillJob(c *C) {
	app := s.createTestApp(c, &ct.App{Name: "killjob"})
	hostID, jobID := random.UUID(), random.UUID()
	hc := tu.NewFakeHostClient(hostID)
	s.cc.AddHost(hc)

	c.Assert(s.c.DeleteJob(app.ID, hostID+"-"+jobID), IsNil)
	c.Assert(hc.IsStopped(jobID), Equals, true)
}

func (s *S) TestRunJobDetached(c *C) {
	app := s.createTestApp(c, &ct.App{Name: "run-detached"})

	hostID := random.UUID()
	host := tu.NewFakeHostClient(hostID)
	s.cc.AddHost(host)

	artifact := s.createTestArtifact(c, &ct.Artifact{Type: "docker", URI: "docker://foo/bar"})
	release := s.createTestRelease(c, &ct.Release{
		ArtifactID: artifact.ID,
		Env:        map[string]string{"RELEASE": "true", "FOO": "bar"},
	})

	cmd := []string{"foo", "bar"}
	req := &ct.NewJob{
		ReleaseID:  release.ID,
		ReleaseEnv: true,
		Cmd:        cmd,
		Env:        map[string]string{"JOB": "true", "FOO": "baz"},
		Meta:       map[string]string{"foo": "baz"},
	}
	res, err := s.c.RunJobDetached(app.ID, req)
	c.Assert(err, IsNil)
	c.Assert(res.ID, Not(Equals), "")
	c.Assert(res.ReleaseID, Equals, release.ID)
	c.Assert(res.Type, Equals, "")
	c.Assert(res.Cmd, DeepEquals, cmd)

	job := host.Jobs[0]
	c.Assert(res.ID, Equals, hostID+"-"+job.ID)
	c.Assert(job.Metadata, DeepEquals, map[string]string{
		"flynn-controller.app":      app.ID,
		"flynn-controller.app_name": app.Name,
		"flynn-controller.release":  release.ID,
		"foo": "baz",
	})
	c.Assert(job.Config.Cmd, DeepEquals, []string{"foo", "bar"})
	c.Assert(job.Config.Env, DeepEquals, map[string]string{
		"FLYNN_APP_ID":       app.ID,
		"FLYNN_RELEASE_ID":   release.ID,
		"FLYNN_PROCESS_TYPE": "",
		"FLYNN_JOB_ID":       hostID + "-" + job.ID,
		"FOO":                "baz",
		"JOB":                "true",
		"RELEASE":            "true",
	})
	c.Assert(job.Config.Stdin, Equals, false)
}

func (s *S) TestRunJobAttached(c *C) {
	app := s.createTestApp(c, &ct.App{Name: "run-attached"})
	hostID := random.UUID()
	hc := tu.NewFakeHostClient(hostID)
	s.cc.AddHost(hc)

	done := make(chan struct{})
	var jobID string
	hc.SetAttachFunc("*", func(req *host.AttachReq, wait bool) (cluster.AttachClient, error) {
		c.Assert(wait, Equals, true)
		c.Assert(req.JobID, Not(Equals), "")
		c.Assert(req, DeepEquals, &host.AttachReq{
			JobID:  req.JobID,
			Flags:  host.AttachFlagStdout | host.AttachFlagStderr | host.AttachFlagStdin | host.AttachFlagStream,
			Height: 20,
			Width:  10,
		})
		jobID = req.JobID
		pipeR, pipeW := io.Pipe()
		go func() {
			stdin, err := ioutil.ReadAll(pipeR)
			c.Assert(err, IsNil)
			c.Assert(string(stdin), Equals, "test in")
			close(done)
		}()
		return cluster.NewAttachClient(struct {
			io.Reader
			io.WriteCloser
		}{strings.NewReader("test out"), pipeW}), nil
	})

	artifact := s.createTestArtifact(c, &ct.Artifact{Type: "docker", URI: "docker://foo/bar"})
	release := s.createTestRelease(c, &ct.Release{
		ArtifactID: artifact.ID,
		Env:        map[string]string{"RELEASE": "true", "FOO": "bar"},
	})

	data := &ct.NewJob{
		ReleaseID:  release.ID,
		ReleaseEnv: true,
		Cmd:        []string{"foo", "bar"},
		Env:        map[string]string{"JOB": "true", "FOO": "baz"},
		Meta:       map[string]string{"foo": "baz"},
		TTY:        true,
		Columns:    10,
		Lines:      20,
	}
	rwc, err := s.c.RunJobAttached(app.ID, data)
	c.Assert(err, IsNil)

	_, err = rwc.Write([]byte("test in"))
	c.Assert(err, IsNil)
	rwc.CloseWrite()
	stdout, err := ioutil.ReadAll(rwc)
	c.Assert(err, IsNil)
	c.Assert(string(stdout), Equals, "test out")
	rwc.Close()

	job := hc.Jobs[0]
	c.Assert(job.ID, Equals, jobID)
	c.Assert(job.Metadata, DeepEquals, map[string]string{
		"flynn-controller.app":      app.ID,
		"flynn-controller.app_name": app.Name,
		"flynn-controller.release":  release.ID,
		"foo": "baz",
	})
	c.Assert(job.Config.Cmd, DeepEquals, []string{"foo", "bar"})
	c.Assert(job.Config.Env, DeepEquals, map[string]string{
		"FLYNN_APP_ID":       app.ID,
		"FLYNN_RELEASE_ID":   release.ID,
		"FLYNN_PROCESS_TYPE": "",
		"FLYNN_JOB_ID":       hostID + "-" + job.ID,
		"FOO":                "baz",
		"JOB":                "true",
		"RELEASE":            "true",
	})
	c.Assert(job.Config.Stdin, Equals, true)
}
