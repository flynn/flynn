package main

import (
	"encoding/json"
	"fmt"
	"net/url"
	"os/exec"
	"time"

	ct "github.com/flynn/flynn/controller/types"
	c "github.com/flynn/go-check"
)

type DockerReceiveSuite struct {
	Helper
}

var _ = c.Suite(&DockerReceiveSuite{})

func (s *DockerReceiveSuite) TestPushImage(t *c.C) {
	// build a Docker image
	repo := "docker-receive-test-push"
	s.buildDockerImage(t, repo, "RUN echo foo > /foo.txt")

	// subscribe to artifact events
	client := s.controllerClient(t)
	events := make(chan *ct.Event)
	stream, err := client.StreamEvents(ct.StreamEventsOptions{
		ObjectTypes: []ct.EventType{ct.EventTypeArtifact},
	}, events)
	t.Assert(err, c.IsNil)
	defer stream.Close()

	// push the Docker image to docker-receive
	u, err := url.Parse(s.clusterConf(t).DockerPushURL)
	t.Assert(err, c.IsNil)
	tag := fmt.Sprintf("%s/%s:latest", u.Host, repo)
	t.Assert(run(t, exec.Command("docker", "tag", "--force", repo, tag)), Succeeds)
	t.Assert(run(t, exec.Command("docker", "push", tag)), Succeeds)

	// wait for an artifact to be created
	var artifact ct.Artifact
loop:
	for {
		select {
		case event, ok := <-events:
			if !ok {
				t.Fatalf("event stream closed unexpectedly: %s", stream.Err())
			}
			t.Assert(json.Unmarshal(event.Data, &artifact), c.IsNil)
			if artifact.Meta["docker-receive.repository"] == repo {
				break loop
			}
		case <-time.After(30 * time.Second):
			t.Fatal("timed out waiting for artifact")
		}
	}

	// create a release with the Docker artifact
	app := &ct.App{}
	t.Assert(client.CreateApp(app), c.IsNil)
	release := &ct.Release{ArtifactIDs: []string{artifact.ID}}
	t.Assert(client.CreateRelease(release), c.IsNil)
	t.Assert(client.SetAppRelease(app.ID, release.ID), c.IsNil)

	// check running a job uses the image
	t.Assert(flynn(t, "/", "-a", app.ID, "run", "cat", "/foo.txt"), SuccessfulOutputContains, "foo")
}
