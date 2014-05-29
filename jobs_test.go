package main

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"sort"
	"strings"

	tu "github.com/flynn/flynn-controller/testutils"
	ct "github.com/flynn/flynn-controller/types"
	"github.com/flynn/flynn-controller/utils"
	"github.com/flynn/flynn-host/types"
	"github.com/flynn/go-flynn/cluster"
	. "github.com/titanous/gocheck"
)

func (s *S) createTestJob(c *C, in *ct.Job) *ct.Job {
	out := &ct.Job{}
	res, err := s.Put("/apps/"+in.AppID+"/jobs/"+in.ID, in, out)
	c.Assert(err, IsNil)
	c.Assert(res.StatusCode, Equals, 200)
	return out
}

func (s *S) TestJobList(c *C) {
	app := s.createTestApp(c, &ct.App{Name: "job-list"})
	release := s.createTestRelease(c, &ct.Release{})
	s.createTestFormation(c, &ct.Formation{ReleaseID: release.ID, AppID: app.ID})
	s.createTestJob(c, &ct.Job{ID: "host0-job0", AppID: app.ID, ReleaseID: release.ID, Type: "web", State: "starting"})

	var list []ct.Job
	res, err := s.Get("/apps/"+app.ID+"/jobs", &list)
	c.Assert(err, IsNil)
	c.Assert(res.StatusCode, Equals, 200)
	c.Assert(len(list), Equals, 1)
	job := list[0]
	c.Assert(job.ID, Equals, "host0-job0")
	c.Assert(job.AppID, Equals, app.ID)
	c.Assert(job.ReleaseID, Equals, release.ID)
}

func newFakeLog(r io.Reader) *fakeLog {
	return &fakeLog{r}
}

type fakeLog struct {
	io.Reader
}

func (l *fakeLog) Close() error      { return nil }
func (l *fakeLog) CloseWrite() error { return nil }
func (l *fakeLog) Write([]byte) (int, error) {
	return 0, io.ErrUnexpectedEOF
}

func (s *S) TestKillJob(c *C) {
	app := s.createTestApp(c, &ct.App{Name: "killjob"})
	hostID, jobID := utils.UUID(), utils.UUID()
	hc := tu.NewFakeHostClient(hostID)
	s.cc.SetHostClient(hostID, hc)

	res, err := s.Delete("/apps/" + app.ID + "/jobs/" + hostID + "-" + jobID)
	c.Assert(err, IsNil)
	c.Assert(res.StatusCode, Equals, 200)
	c.Assert(hc.IsStopped(jobID), Equals, true)
}

func (s *S) TestJobLog(c *C) {
	app := s.createTestApp(c, &ct.App{Name: "joblog"})
	hostID, jobID := utils.UUID(), utils.UUID()
	hc := tu.NewFakeHostClient(hostID)
	hc.SetAttach(jobID, newFakeLog(strings.NewReader("foo")))
	s.cc.SetHostClient(hostID, hc)

	req, err := http.NewRequest("GET", fmt.Sprintf("%s/apps/%s/jobs/%s-%s/log", s.srv.URL, app.ID, hostID, jobID), nil)
	c.Assert(err, IsNil)
	req.SetBasicAuth("", authKey)
	res, err := http.DefaultClient.Do(req)
	c.Assert(err, IsNil)
	var buf bytes.Buffer
	_, err = buf.ReadFrom(res.Body)
	res.Body.Close()
	c.Assert(err, IsNil)

	c.Assert(buf.String(), Equals, "foo")
}

func (s *S) TestJobLogSSE(c *C) {
	app := s.createTestApp(c, &ct.App{Name: "joblog-sse"})
	hostID, jobID := utils.UUID(), utils.UUID()
	hc := tu.NewFakeHostClient(hostID)
	logData, err := base64.StdEncoding.DecodeString("AQAAAAAAABNMaXN0ZW5pbmcgb24gNTUwMDcKAQAAAAAAAA1oZWxsbyBzdGRvdXQKAgAAAAAAAA1oZWxsbyBzdGRlcnIK")
	c.Assert(err, IsNil)
	hc.SetAttach(jobID, newFakeLog(bytes.NewReader(logData)))
	s.cc.SetHostClient(hostID, hc)

	req, err := http.NewRequest("GET", fmt.Sprintf("%s/apps/%s/jobs/%s-%s/log", s.srv.URL, app.ID, hostID, jobID), nil)
	c.Assert(err, IsNil)
	req.SetBasicAuth("", authKey)
	req.Header.Set("Accept", "text/event-stream")
	res, err := http.DefaultClient.Do(req)
	c.Assert(err, IsNil)

	var buf bytes.Buffer
	_, err = buf.ReadFrom(res.Body)
	res.Body.Close()
	c.Assert(err, IsNil)

	expected := "data: {\"stream\":\"stdout\",\"data\":\"Listening on 55007\\n\"}\n\ndata: {\"stream\":\"stdout\",\"data\":\"hello stdout\\n\"}\n\ndata: {\"stream\":\"stderr\",\"data\":\"hello stderr\\n\"}\n\nevent: eof\ndata: {}\n\n"

	c.Assert(buf.String(), Equals, expected)
}

type fakeAttachStream struct {
	io.Reader
	io.WriteCloser
}

func (l *fakeAttachStream) CloseWrite() error { return l.WriteCloser.Close() }
func (l *fakeAttachStream) Close() error      { return l.CloseWrite() }

func (s *S) TestRunJobDetached(c *C) {
	app := s.createTestApp(c, &ct.App{Name: "run-detached"})

	hostID := utils.UUID()
	s.cc.SetHosts(map[string]host.Host{hostID: host.Host{}})

	artifact := s.createTestArtifact(c, &ct.Artifact{Type: "docker", URI: "docker://foo/bar"})
	release := s.createTestRelease(c, &ct.Release{
		ArtifactID: artifact.ID,
		Env:        map[string]string{"RELEASE": "true", "FOO": "bar"},
	})

	cmd := []string{"foo", "bar"}
	req := &ct.NewJob{
		ReleaseID: release.ID,
		Cmd:       cmd,
		Env:       map[string]string{"JOB": "true", "FOO": "baz"},
	}
	res := &ct.Job{}
	_, err := s.Post(fmt.Sprintf("/apps/%s/jobs", app.ID), req, res)
	c.Assert(err, IsNil)
	c.Assert(res.ID, Not(Equals), "")
	c.Assert(res.ReleaseID, Equals, release.ID)
	c.Assert(res.Type, Equals, "")
	c.Assert(res.Cmd, DeepEquals, cmd)

	job := s.cc.GetHost(hostID).Jobs[0]
	c.Assert(res.ID, Equals, hostID+"-"+job.ID)
	c.Assert(job.Attributes, DeepEquals, map[string]string{
		"flynn-controller.app":     app.ID,
		"flynn-controller.release": release.ID,
	})
	c.Assert(job.Config.Cmd, DeepEquals, []string{"foo", "bar"})
	sort.Strings(job.Config.Env)
	c.Assert(job.Config.Env, DeepEquals, []string{"FOO=baz", "JOB=true", "RELEASE=true"})
	c.Assert(job.Config.AttachStdout, Equals, true)
	c.Assert(job.Config.AttachStderr, Equals, true)
	c.Assert(job.Config.AttachStdin, Equals, false)
	c.Assert(job.Config.StdinOnce, Equals, false)
	c.Assert(job.Config.OpenStdin, Equals, false)
}

func (s *S) TestRunJobAttached(c *C) {
	app := s.createTestApp(c, &ct.App{Name: "run-attached"})
	hostID := utils.UUID()
	hc := tu.NewFakeHostClient(hostID)

	done := make(chan struct{})
	var jobID string
	hc.SetAttachFunc("*", func(req *host.AttachReq, wait bool) (cluster.ReadWriteCloser, func() error, error) {
		c.Assert(wait, Equals, true)
		c.Assert(req.JobID, Not(Equals), "")
		c.Assert(req, DeepEquals, &host.AttachReq{
			JobID:  req.JobID,
			Flags:  host.AttachFlagStdout | host.AttachFlagStderr | host.AttachFlagStdin | host.AttachFlagStream,
			Height: 20,
			Width:  10,
		})
		jobID = req.JobID
		piper, pipew := io.Pipe()
		go func() {
			stdin, err := ioutil.ReadAll(piper)
			c.Assert(err, IsNil)
			c.Assert(string(stdin), Equals, "test in")
			close(done)
		}()
		return &fakeAttachStream{strings.NewReader("test out"), pipew}, func() error { return nil }, nil
	})

	s.cc.SetHostClient(hostID, hc)
	s.cc.SetHosts(map[string]host.Host{hostID: host.Host{}})

	artifact := s.createTestArtifact(c, &ct.Artifact{Type: "docker", URI: "docker://foo/bar"})
	release := s.createTestRelease(c, &ct.Release{
		ArtifactID: artifact.ID,
		Env:        map[string]string{"RELEASE": "true", "FOO": "bar"},
	})

	data, _ := json.Marshal(&ct.NewJob{
		ReleaseID: release.ID,
		Cmd:       []string{"foo", "bar"},
		Env:       map[string]string{"JOB": "true", "FOO": "baz"},
		TTY:       true,
		Columns:   10,
		Lines:     20,
	})
	req, err := http.NewRequest("POST", s.srv.URL+"/apps/"+app.ID+"/jobs", bytes.NewBuffer(data))
	c.Assert(err, IsNil)
	req.SetBasicAuth("", authKey)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/vnd.flynn.attach")
	_, rwc, err := utils.HijackRequest(req, nil)
	c.Assert(err, IsNil)

	_, err = rwc.Write([]byte("test in"))
	c.Assert(err, IsNil)
	rwc.CloseWrite()
	stdout, err := ioutil.ReadAll(rwc)
	c.Assert(err, IsNil)
	c.Assert(string(stdout), Equals, "test out")
	rwc.Close()

	job := s.cc.GetHost(hostID).Jobs[0]
	c.Assert(job.ID, Equals, jobID)
	c.Assert(job.Attributes, DeepEquals, map[string]string{
		"flynn-controller.app":     app.ID,
		"flynn-controller.release": release.ID,
	})
	c.Assert(job.Config.Cmd, DeepEquals, []string{"foo", "bar"})
	sort.Strings(job.Config.Env)
	c.Assert(job.Config.Env, DeepEquals, []string{"FOO=baz", "JOB=true", "RELEASE=true"})
	c.Assert(job.Config.AttachStdout, Equals, true)
	c.Assert(job.Config.AttachStderr, Equals, true)
	c.Assert(job.Config.AttachStdin, Equals, true)
	c.Assert(job.Config.StdinOnce, Equals, true)
	c.Assert(job.Config.OpenStdin, Equals, true)
}
