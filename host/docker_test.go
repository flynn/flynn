package main

import (
	"errors"
	"strings"
	"testing"

	"github.com/flynn/flynn/Godeps/_workspace/src/github.com/flynn/go-dockerclient"
	"github.com/flynn/flynn/Godeps/_workspace/src/github.com/technoweenie/grohl"
	"github.com/flynn/flynn/host/ports"
	"github.com/flynn/flynn/host/types"
)

type nullLogger struct{}

func (nullLogger) Log(grohl.Data) error { return nil }

func init() { grohl.SetLogger(nullLogger{}) }

type fakeDockerClient struct {
	createErr error
	startErr  error
	pullErr   error
	created   *docker.Config
	pulled    string
	started   bool
	hostConf  *docker.HostConfig
	events    chan *docker.Event
}

func (c *fakeDockerClient) CreateContainer(config *docker.Config) (*docker.Container, error) {
	if c.createErr != nil {
		err := c.createErr
		c.createErr = nil
		return nil, err
	}
	c.created = config
	return &docker.Container{ID: "asdf"}, nil
}

func (c *fakeDockerClient) StartContainer(id string, config *docker.HostConfig) error {
	if id != "asdf" {
		return errors.New("Invalid ID")
	}
	if c.startErr != nil {
		return c.startErr
	}
	c.started = true
	c.hostConf = config
	return nil
}

func (c *fakeDockerClient) InspectContainer(id string) (*docker.Container, error) {
	if id == "1" {
		return &docker.Container{State: docker.State{ExitCode: 1}}, nil
	}
	container := &docker.Container{Volumes: make(map[string]string), NetworkSettings: &docker.NetworkSettings{}}
	for v := range c.created.Volumes {
		container.Volumes[v] = "/var/lib/docker/vfs/dir/" + strings.Replace(v, "/", "-", -1)
	}
	return container, nil
}

func (c *fakeDockerClient) PullImage(opts docker.PullImageOptions) error {
	if c.pullErr != nil {
		return c.pullErr
	}
	c.pulled = opts.Repository
	return nil
}

func (c *fakeDockerClient) Events() (*docker.EventStream, error) {
	return &docker.EventStream{Events: c.events}, nil
}

func (c *fakeDockerClient) StopContainer(string, uint) error {
	return nil
}

func (c *fakeDockerClient) ResizeContainerTTY(string, int, int) error {
	return nil
}

func (c *fakeDockerClient) AttachToContainer(docker.AttachToContainerOptions) error {
	return nil
}

func (c *fakeDockerClient) KillContainer(docker.KillContainerOptions) error {
	return nil
}

func (c *fakeDockerClient) ListContainers(docker.ListContainersOptions) ([]docker.APIContainers, error) {
	return nil, nil
}

func testDockerRun(job *host.Job, t *testing.T) (*State, *fakeDockerClient) {
	client := &fakeDockerClient{}
	return testDockerRunWithOpts(job, "", client, t), client
}

func testDockerRunWithOpts(job *host.Job, bindAddr string, client *fakeDockerClient, t *testing.T) *State {
	if job.Artifact.URI == "" {
		job.Artifact = host.Artifact{Type: "docker", URI: "https://registry.hub.docker.com/test/foo"}
	}
	if client == nil {
		client = &fakeDockerClient{}
	}

	state, err := dockerRunWithOpts(job, bindAddr, client)
	if err != nil {
		t.Errorf("run error: %s", err)
	}
	if client.created == nil {
		t.Error("job not created")
	}
	if !client.started {
		t.Error("job not started")
	}
	sjob := state.GetJob("a")
	if sjob == nil || sjob.StartedAt.IsZero() || sjob.Status != host.StatusRunning || sjob.ContainerID != "asdf" {
		t.Error("incorrect state")
	}

	return state
}

func testProcessWithError(job *host.Job, client *fakeDockerClient, expected error, t *testing.T) {
	if job.Artifact.URI == "" {
		job.Artifact = host.Artifact{Type: "docker", URI: "https://registry.hub.docker.com/test/foo"}
	}
	_, err := dockerRunWithOpts(job, "", client)
	if err != expected {
		t.Errorf("expected %s, got %s", expected, err)
	}
}

func dockerRunWithOpts(job *host.Job, bindAddr string, client *fakeDockerClient) (*State, error) {
	state := NewState()
	err := (&DockerBackend{
		bindAddr: bindAddr,
		docker:   client,
		state:    state,
		ports:    map[string]*ports.Allocator{"tcp": ports.NewAllocator(500, 550)},
	}).Run(job)
	return state, err
}

func TestProcessJob(t *testing.T) {
	testDockerRun(&host.Job{ID: "a"}, t)
}

func TestProcessJobWithImplicitPorts(t *testing.T) {
	job := &host.Job{
		ID: "a",
		Config: host.ContainerConfig{
			Ports: []host.Port{{Proto: "tcp"}, {Proto: "tcp"}},
		},
	}
	_, client := testDockerRun(job, t)

	if len(client.created.Env) == 0 || !sliceHasString(client.created.Env, "PORT=500") {
		t.Fatal("PORT env not set")
	}
	if !sliceHasString(client.created.Env, "PORT_0=500") {
		t.Error("PORT_0 env not set")
	}
	if !sliceHasString(client.created.Env, "PORT_1=501") {
		t.Error("PORT_1 env not set")
	}
	if _, ok := client.created.ExposedPorts["500/tcp"]; !ok {
		t.Error("exposed port 500 not set")
	}
	if _, ok := client.created.ExposedPorts["501/tcp"]; !ok {
		t.Error("exposed port 501 not set")
	}
	if b := client.hostConf.PortBindings["500/tcp"]; len(b) == 0 || b[0].HostPort != "500" {
		t.Error("port 500 binding not set")
	}
	if b := client.hostConf.PortBindings["501/tcp"]; len(b) == 0 || b[0].HostPort != "501" {
		t.Error("port 501 binding not set")
	}
}

func TestProcessWithImplicitPortsAndIP(t *testing.T) {
	job := &host.Job{
		ID: "a",
		Config: host.ContainerConfig{
			Ports: []host.Port{{Proto: "tcp"}, {Proto: "tcp"}},
		},
	}
	client := &fakeDockerClient{}
	testDockerRunWithOpts(job, "127.0.42.1", client, t)

	b := client.hostConf.PortBindings["500/tcp"]
	if b[0].HostIp != "127.0.42.1" {
		t.Error("host ip not 127.0.42.1")
	}
	if len(b) == 0 || b[0].HostPort != "500" {
		t.Error("port 8080 binding not set")
	}
}

func sliceHasString(slice []string, str string) bool {
	for _, s := range slice {
		if s == str {
			return true
		}
	}
	return false
}

func TestProcessWithPull(t *testing.T) {
	job := &host.Job{ID: "a"}
	client := &fakeDockerClient{createErr: docker.ErrNoSuchImage}
	testDockerRunWithOpts(job, "", client, t)

	if client.pulled != "test/foo" {
		t.Error("image not pulled")
	}
}

func TestProcessWithCreateFailure(t *testing.T) {
	job := &host.Job{ID: "a"}
	err := errors.New("undefined failure")
	client := &fakeDockerClient{createErr: err}
	testProcessWithError(job, client, err, t)
}

func TestProcessWithPullFailure(t *testing.T) {
	job := &host.Job{ID: "a"}
	err := errors.New("undefined failure")
	client := &fakeDockerClient{createErr: docker.ErrNoSuchImage, pullErr: err}
	testProcessWithError(job, client, err, t)
}

func TestProcessWithStartFailure(t *testing.T) {
	job := &host.Job{ID: "a"}
	err := errors.New("undefined failure")
	client := &fakeDockerClient{startErr: err}
	testProcessWithError(job, client, err, t)
}

type schedulerSyncClient struct {
	removeErr error
	removed   []string
}

func (s *schedulerSyncClient) RemoveJobs(jobs []string) error {
	if s.removeErr != nil {
		return s.removeErr
	}
	s.removed = append(s.removed, jobs...)
	return nil
}

func TestSyncScheduler(t *testing.T) {
	events := make(chan host.Event)
	client := &schedulerSyncClient{}
	done := make(chan struct{})
	go func() {
		syncScheduler(client, events)
		close(done)
	}()

	events <- host.Event{Event: "stop", JobID: "a"}
	close(events)
	<-done

	if len(client.removed) != 1 && client.removed[0] != "a" {
		t.Error("job not removed")
	}
}

func TestStreamEvents(t *testing.T) {
	events := make(chan *docker.Event)
	state := NewState()
	state.AddJob(&host.Job{ID: "a"})
	state.SetContainerID("a", "1")
	state.SetStatusRunning("a")

	done := make(chan struct{})
	go func() {
		(&DockerBackend{
			docker: &fakeDockerClient{events: events},
			state:  state,
			ports:  map[string]*ports.Allocator{"tcp": ports.NewAllocator(500, 550)},
		}).handleEvents()
		close(done)
	}()

	events <- &docker.Event{Status: "die", ID: "1"}
	close(events)
	<-done

	job := state.GetJob("a")
	if job.Status != host.StatusCrashed {
		t.Error("incorrect status")
	}
	if job.ExitStatus != 1 {
		t.Error("incorrect exit status")
	}
}
