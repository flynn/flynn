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

var _ = c.ConcurrentSuite(&DockerReceiveSuite{})

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
	t.Assert(client.CreateRelease(app.ID, release), c.IsNil)
	t.Assert(client.SetAppRelease(app.ID, release.ID), c.IsNil)

	// check running a job uses the image
	t.Assert(flynn(t, "/", "-a", app.ID, "run", "cat", "/foo.txt"), SuccessfulOutputContains, "foo")
}

// TestConvertWhitouts ensures that AUFS whiteouts are converted to OverlayFS
// whiteouts and have the same effect (i.e. hiding removed files)
func (s *DockerReceiveSuite) TestConvertWhiteouts(t *c.C) {
	// build a Docker image with whiteouts
	repo := "docker-receive-test-whiteouts"
	s.buildDockerImage(t, repo,
		"RUN echo foo > /foo.txt",
		"RUN rm /foo.txt",
		"RUN mkdir /opaque && touch /opaque/file.txt",
		"RUN rm -rf /opaque && mkdir /opaque",
	)

	// create app
	client := s.controllerClient(t)
	app := &ct.App{Name: repo}
	t.Assert(client.CreateApp(app), c.IsNil)

	// flynn docker push image
	t.Assert(flynn(t, "/", "-a", app.Name, "docker", "push", repo), Succeeds)

	// check the whiteouts are effective
	t.Assert(flynn(t, "/", "-a", app.Name, "run", "sh", "-c", "[[ ! -f /foo.txt ]]"), Succeeds)
	t.Assert(flynn(t, "/", "-a", app.Name, "run", "sh", "-c", "[[ ! -f /opaque/file.txt ]]"), Succeeds)
}

// TestReleaseDeleteImageLayers ensures that deleting a release which uses an
// image which has shared layers does not delete the shared layers
func (s *DockerReceiveSuite) TestReleaseDeleteImageLayers(t *c.C) {
	// build Docker images with shared layers and push them to two
	// different apps
	app1 := "docker-receive-test-delete-layers-1"
	s.buildDockerImage(t, app1,
		"RUN echo shared-layer > /shared.txt",
		"RUN echo app1-layer > /app1.txt",
	)
	t.Assert(flynn(t, "/", "create", "--remote", "", app1), Succeeds)
	t.Assert(flynn(t, "/", "-a", app1, "docker", "push", app1), Succeeds)

	app2 := "docker-receive-test-delete-layers-2"
	s.buildDockerImage(t, app2,
		"RUN echo shared-layer > /shared.txt",
		"RUN echo app2-layer > /app2.txt",
	)
	t.Assert(flynn(t, "/", "create", "--remote", "", app2), Succeeds)
	t.Assert(flynn(t, "/", "-a", app2, "docker", "push", app2), Succeeds)

	// get the two images
	client := s.controllerClient(t)
	release1, err := client.GetAppRelease(app1)
	t.Assert(err, c.IsNil)
	image1, err := client.GetArtifact(release1.ArtifactIDs[0])
	t.Assert(err, c.IsNil)
	release2, err := client.GetAppRelease(app2)
	t.Assert(err, c.IsNil)
	image2, err := client.GetArtifact(release2.ArtifactIDs[0])
	t.Assert(err, c.IsNil)

	// check that the two apps have some common image layers but different
	// artifacts
	image1Layers := make(map[string]struct{}, len(image1.Manifest().Rootfs[0].Layers))
	for _, layer := range image1.Manifest().Rootfs[0].Layers {
		image1Layers[layer.ID] = struct{}{}
	}
	image2Layers := make(map[string]struct{}, len(image2.Manifest().Rootfs[0].Layers))
	for _, layer := range image2.Manifest().Rootfs[0].Layers {
		image2Layers[layer.ID] = struct{}{}
	}
	commonLayers := make(map[string]struct{})
	distinctLayers := make(map[string]struct{})
	for id := range image1Layers {
		if _, ok := image2Layers[id]; ok {
			commonLayers[id] = struct{}{}
		} else {
			distinctLayers[id] = struct{}{}
		}
	}
	t.Assert(commonLayers, c.Not(c.HasLen), 0)
	t.Assert(distinctLayers, c.Not(c.HasLen), 0)
	t.Assert(image1.ID, c.Not(c.Equals), image2.ID)

	// check all the layers exist at the paths we expect in the blobstore
	getLayer := func(id string) *CmdResult {
		url := fmt.Sprintf("http://blobstore.discoverd/docker-receive/layers/%s.squashfs", id)
		return flynn(t, "/", "-a", "blobstore", "run", "curl", "-fsSLo", "/dev/null", "--write-out", "%{http_code}", url)
	}
	assertExist := func(layers map[string]struct{}) {
		for id := range layers {
			res := getLayer(id)
			t.Assert(res, Succeeds)
			t.Assert(res, Outputs, "200")
		}
	}
	assertNotExist := func(layers map[string]struct{}) {
		for id := range layers {
			res := getLayer(id)
			t.Assert(res, c.Not(Succeeds))
			t.Assert(res, OutputContains, "404 Not Found")
		}
	}
	assertExist(commonLayers)
	assertExist(distinctLayers)

	// delete app1 and check the distinct layers were deleted but the
	// common layers still exist
	t.Assert(flynn(t, "/", "-a", app1, "delete", "--yes"), Succeeds)
	assertNotExist(distinctLayers)
	assertExist(commonLayers)

	// delete app2 and check we can push app1's image to a new app and have
	// the layers regenerated (which checks docker-receive cache invalidation)
	t.Assert(flynn(t, "/", "-a", app2, "delete", "--yes"), Succeeds)
	app3 := "docker-receive-test-delete-layers-3"
	t.Assert(flynn(t, "/", "create", "--remote", "", app3), Succeeds)
	t.Assert(flynn(t, "/", "-a", app3, "docker", "push", app1), Succeeds)
	t.Assert(flynn(t, "/", "-a", app3, "run", "test", "-f", "/app1.txt"), Succeeds)
}
