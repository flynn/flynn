package main

import (
	"encoding/base64"
	"time"

	c "github.com/flynn/flynn/Godeps/_workspace/src/gopkg.in/check.v1"
	"github.com/flynn/flynn/cli/config"
	"github.com/flynn/flynn/controller/client"
	ct "github.com/flynn/flynn/controller/types"
)

type SchedulerSuite struct {
	client *controller.Client
}

var _ = c.Suite(&SchedulerSuite{})

func (s *SchedulerSuite) SetUpSuite(t *c.C) {
	conf, err := config.ReadFile(flynnrc)
	t.Assert(err, c.IsNil)

	cluster := conf.Clusters[0]
	pin, err := base64.StdEncoding.DecodeString(cluster.TLSPin)
	t.Assert(err, c.IsNil)
	client, err := controller.NewClientWithPin(cluster.URL, cluster.Key, pin)
	t.Assert(err, c.IsNil)

	s.client = client
}

func processesEqual(expected, actual map[string]int) bool {
	for t, n := range expected {
		if actual[t] != n {
			return false
		}
	}
	return true
}

func waitForJobEvents(t *c.C, events chan *ct.JobEvent, diff map[string]int) error {
	actual := make(map[string]int)
	for {
		select {
		case event := <-events:
			switch event.State {
			case "up":
				actual[event.Type] += 1
			case "down":
				actual[event.Type] -= 1
			}
			if processesEqual(diff, actual) {
				return nil
			}
		case <-time.After(5 * time.Second):
			t.Fatal("timed out waiting for job events")
		}
	}
}

var busyboxID = "184af8860f22e7a87f1416bb12a32b20d0d2c142f719653d87809a6122b04663"

func (s *SchedulerSuite) TestScale(t *c.C) {
	app := &ct.App{}
	t.Assert(s.client.CreateApp(app), c.IsNil)

	artifact := &ct.Artifact{Type: "docker", URI: "https://registry.hub.docker.com/flynn/busybox?id=" + busyboxID}
	t.Assert(s.client.CreateArtifact(artifact), c.IsNil)

	release := &ct.Release{
		ArtifactID: artifact.ID,
		Processes: map[string]ct.ProcessType{
			"date": {Cmd: []string{"sh", "-c", "while true; do date; sleep 1; done"}},
			"work": {Cmd: []string{"sh", "-c", "while true; do echo work; sleep 1; done"}},
		},
	}
	t.Assert(s.client.CreateRelease(release), c.IsNil)
	t.Assert(s.client.SetAppRelease(app.ID, release.ID), c.IsNil)

	stream, err := s.client.StreamJobEvents(app.ID)
	t.Assert(err, c.IsNil)
	defer stream.Close()

	formation := &ct.Formation{
		AppID:     app.ID,
		ReleaseID: release.ID,
		Processes: make(map[string]int),
	}

	current := make(map[string]int)
	updates := []map[string]int{
		{"date": 2},
		{"date": 3, "work": 1},
		{"date": 1},
	}

	for _, procs := range updates {
		formation.Processes = procs
		t.Assert(s.client.PutFormation(formation), c.IsNil)

		diff := make(map[string]int)
		for t, n := range procs {
			diff[t] = n - current[t]
		}
		for t, n := range current {
			if _, ok := procs[t]; !ok {
				diff[t] = -n
			}
		}
		waitForJobEvents(t, stream.Events, diff)

		current = procs
	}
}
