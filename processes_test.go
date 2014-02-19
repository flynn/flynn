package main

import (
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
	return c.hostClients[id], nil
}

func (c *fakeCluster) setHosts(h map[string]host.Host) {
	c.hosts = h
}

func (c *fakeCluster) setHostClient(id string, h cluster.Host) {
	c.hostClients[id] = h
}

func (s *S) TestProcessList(c *C) {
	app := s.createTestApp(c, &ct.App{Name: "processList"})
	s.cc.setHosts(map[string]host.Host{"host0": {
		ID: "host0",
		Jobs: []*host.Job{
			{ID: "job0", Attributes: map[string]string{"flynn-controller.app": app.ID, "flynn-controller.release": "release0", "flynn-controller.type": "web"}},
			{ID: "job1", Attributes: map[string]string{"flynn-controller.app": app.ID}, Config: &docker.Config{Cmd: []string{"bash"}}},
			{ID: "job2", Attributes: map[string]string{"flynn-controller.app": "otherApp"}},
			{ID: "job3"},
		},
	}})

	expected := []ct.Process{
		{ID: "host0:job0", Type: "web", ReleaseID: "release0"},
		{ID: "host0:job1", Cmd: []string{"bash"}},
	}

	var actual []ct.Process
	res, err := s.Get("/apps/"+app.ID+"/processes", &actual)
	c.Assert(err, IsNil)
	c.Assert(res.StatusCode, Equals, 200)
	c.Assert(actual, DeepEquals, expected)
}
