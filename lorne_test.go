package main

import (
	"errors"
	"io"
	"testing"

	"github.com/flynn/lorne/types"
	"github.com/flynn/sampi/types"
	"github.com/technoweenie/grohl"
	"github.com/titanous/go-dockerclient"
)

type nullLogger struct{}

func (nullLogger) Log(grohl.Data) error { return nil }

func init() { grohl.SetLogger(nullLogger{}) }

type dockerClient struct {
	created *docker.Config
	started bool
}

func (c *dockerClient) CreateContainer(config *docker.Config) (*docker.Container, error) {
	c.created = config
	return &docker.Container{ID: "asdf"}, nil
}

func (c *dockerClient) StartContainer(id string, config *docker.HostConfig) error {
	if id != "asdf" {
		return errors.New("Invalid ID")
	}
	c.started = true
	return nil
}

func (c *dockerClient) PullImage(opts docker.PullImageOptions, w io.Writer) error {
	return nil
}

func testProcessWith(job *sampi.Job, t *testing.T) (*State, *dockerClient) {
	jobs := make(chan *sampi.Job)
	done := make(chan struct{})
	client := &dockerClient{}
	state := NewState()
	go func() {
		processJobs(jobs, "", client, state)
		close(done)
	}()
	jobs <- job
	close(jobs)
	<-done

	if client.created != job.Config {
		t.Error("job not created")
	}
	if job.Config.Name != "flynn-a" {
		t.Error("job name not set")
	}
	if !client.started {
		t.Error("job not started")
	}
	sjob := state.GetJob("a")
	if sjob == nil || sjob.StartedAt.IsZero() || sjob.Status != lorne.StatusRunning || sjob.ContainerID != "asdf" {
		t.Error("incorrect state")
	}

	return state, client
}

func TestProcessJob(t *testing.T) {
	testProcessWith(&sampi.Job{ID: "a", Config: &docker.Config{}}, t)
}
