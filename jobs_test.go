package main

import (
	"bytes"
	"io"
	"net/http"
	"strings"

	ct "github.com/flynn/flynn-controller/types"
	"github.com/flynn/flynn-host/types"
	"github.com/flynn/go-dockerclient"
	"github.com/flynn/go-flynn/cluster"
	. "launchpad.net/gocheck"
)

func newFakeCluster() *fakeCluster {
	return &fakeCluster{hostClients: make(map[string]cluster.Host)}
}

type fakeCluster struct {
	hosts       map[string]host.Host
	hostClients map[string]cluster.Host
}

func (c *fakeCluster) ListHosts() (map[string]host.Host, error) {
	return c.hosts, nil
}

func (c *fakeCluster) ConnectHost(id string) (cluster.Host, error) {
	client, ok := c.hostClients[id]
	if !ok {
		return nil, ErrNotFound
	}
	return client, nil
}

func (c *fakeCluster) setHosts(h map[string]host.Host) {
	c.hosts = h
}

func (c *fakeCluster) setHostClient(id string, h cluster.Host) {
	c.hostClients[id] = h
}

func (s *S) TestJobList(c *C) {
	app := s.createTestApp(c, &ct.App{Name: "jobList"})
	s.cc.setHosts(map[string]host.Host{"host0": {
		ID: "host0",
		Jobs: []*host.Job{
			{ID: "job0", Attributes: map[string]string{"flynn-controller.app": app.ID, "flynn-controller.release": "release0", "flynn-controller.type": "web"}},
			{ID: "job1", Attributes: map[string]string{"flynn-controller.app": app.ID}, Config: &docker.Config{Cmd: []string{"bash"}}},
			{ID: "job2", Attributes: map[string]string{"flynn-controller.app": "otherApp"}},
			{ID: "job3"},
		},
	}})

	expected := []ct.Job{
		{ID: "host0:job0", Type: "web", ReleaseID: "release0"},
		{ID: "host0:job1", Cmd: []string{"bash"}},
	}

	var actual []ct.Job
	res, err := s.Get("/apps/"+app.ID+"/jobs", &actual)
	c.Assert(err, IsNil)
	c.Assert(res.StatusCode, Equals, 200)
	c.Assert(actual, DeepEquals, expected)
}

func newFakeHostClient() *fakeHostClient {
	return &fakeHostClient{
		stopped: make(map[string]bool),
		attach:  make(map[string]cluster.ReadWriteCloser),
	}
}

type fakeHostClient struct {
	stopped map[string]bool
	attach  map[string]cluster.ReadWriteCloser
}

func (c *fakeHostClient) ListJobs() (map[string]host.ActiveJob, error)        { return nil, nil }
func (c *fakeHostClient) GetJob(id string) (*host.ActiveJob, error)           { return nil, nil }
func (c *fakeHostClient) StreamEvents(id string, ch chan<- host.Event) *error { return nil }
func (c *fakeHostClient) Close() error                                        { return nil }
func (c *fakeHostClient) Attach(req *host.AttachReq, wait bool) (cluster.ReadWriteCloser, func() error, error) {
	return c.attach[req.JobID], nil, nil
}

func (c *fakeHostClient) StopJob(id string) error {
	c.stopped[id] = true
	return nil
}

func (c *fakeHostClient) isStopped(id string) bool {
	return c.stopped[id]
}

func (c *fakeHostClient) setAttach(id string, a cluster.ReadWriteCloser) {
	c.attach[id] = a
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
	hc := newFakeHostClient()
	hostID, jobID := uuid(), uuid()
	s.cc.setHostClient(hostID, hc)

	res, err := s.Delete("/apps/" + app.ID + "/jobs/" + hostID + ":" + jobID)
	c.Assert(err, IsNil)
	c.Assert(res.StatusCode, Equals, 200)
	c.Assert(hc.isStopped(jobID), Equals, true)
}

func (s *S) TestJobLogs(c *C) {
	app := s.createTestApp(c, &ct.App{Name: "joblogs"})
	hc := newFakeHostClient()
	hostID, jobID := uuid(), uuid()
	hc.setAttach(jobID, newFakeLog(strings.NewReader("foo")))
	s.cc.setHostClient(hostID, hc)

	res, err := http.Get(s.srv.URL + "/apps/" + app.ID + "/jobs/" + hostID + ":" + jobID + "/logs")
	c.Assert(err, IsNil)
	var buf bytes.Buffer
	_, err = buf.ReadFrom(res.Body)
	res.Body.Close()
	c.Assert(err, IsNil)

	c.Assert(buf.String(), Equals, "foo")
}
