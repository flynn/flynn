package main

import (
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/flynn/flynn-host/ports"
	"github.com/flynn/flynn-host/types"
	"github.com/flynn/go-dockerclient"
	"github.com/technoweenie/grohl"
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
	container := &docker.Container{Volumes: make(map[string]string)}
	for v := range c.created.Volumes {
		container.Volumes[v] = "/var/lib/docker/vfs/dir/" + strings.Replace(v, "/", "-", -1)
	}
	return container, nil
}

func (c *fakeDockerClient) PullImage(opts docker.PullImageOptions, w io.Writer) error {
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

func (c *fakeDockerClient) KillContainer(string) error {
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
	if client == nil {
		client = &fakeDockerClient{}
	}
	state := dockerRunWithOpts(job, bindAddr, client)

	if client.created != job.Config {
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

func testProcessWithError(job *host.Job, client *fakeDockerClient, err error, t *testing.T) *State {
	state := dockerRunWithOpts(job, "", client)

	sjob := state.GetJob(job.ID)
	if sjob.Status != host.StatusFailed {
		t.Errorf("expected status failed, got %s", sjob.Status)
	}
	if *sjob.Error != err.Error() {
		t.Error("error not saved")
	}
	return state
}

func dockerRunWithOpts(job *host.Job, bindAddr string, client *fakeDockerClient) *State {
	state := NewState()
	(&DockerBackend{
		bindAddr: bindAddr,
		docker:   client,
		state:    state,
		ports:    ports.NewAllocator(500, 550),
	}).Run(job)
	return state
}

func TestProcessJob(t *testing.T) {
	testDockerRun(&host.Job{ID: "a", Config: &docker.Config{}}, t)
}

func TestProcessJobWithImplicitPorts(t *testing.T) {
	job := &host.Job{TCPPorts: 2, ID: "a", Config: &docker.Config{}}
	_, client := testDockerRun(job, t)

	if len(job.Config.Env) == 0 || !sliceHasString(job.Config.Env, "PORT=500") {
		t.Fatal("PORT env not set")
	}
	if !sliceHasString(job.Config.Env, "PORT_0=500") {
		t.Error("PORT_0 env not set")
	}
	if !sliceHasString(job.Config.Env, "PORT_1=501") {
		t.Error("PORT_1 env not set")
	}
	if _, ok := job.Config.ExposedPorts["500/tcp"]; !ok {
		t.Error("exposed port 500 not set")
	}
	if _, ok := job.Config.ExposedPorts["501/tcp"]; !ok {
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
	job := &host.Job{ID: "a", TCPPorts: 2, Config: &docker.Config{}}
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

func TestProcessJobWithExplicitPorts(t *testing.T) {
	hostConfig := &docker.HostConfig{
		PortBindings: make(map[string][]docker.PortBinding, 2),
	}
	hostConfig.PortBindings["80/tcp"] = []docker.PortBinding{{HostPort: "8080"}}
	hostConfig.PortBindings["443/tcp"] = []docker.PortBinding{{HostPort: "8081"}}

	job := &host.Job{ID: "a", Config: &docker.Config{}, HostConfig: hostConfig}
	_, client := testDockerRun(job, t)

	if b := client.hostConf.PortBindings["80/tcp"]; len(b) == 0 || b[0].HostPort != "8080" {
		t.Error("port 8080 binding not set")
	}
	if b := client.hostConf.PortBindings["443/tcp"]; len(b) == 0 || b[0].HostPort != "8081" {
		t.Error("port 8081 binding not set")
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
	job := &host.Job{ID: "a", Config: &docker.Config{Image: "test/foo"}}
	client := &fakeDockerClient{createErr: docker.ErrNoSuchImage}
	testDockerRunWithOpts(job, "", client, t)

	if client.pulled != "test/foo" {
		t.Error("image not pulled")
	}
}

func TestProcessWithCreateFailure(t *testing.T) {
	job := &host.Job{ID: "a", Config: &docker.Config{}}
	err := errors.New("undefined failure")
	client := &fakeDockerClient{createErr: err}
	testProcessWithError(job, client, err, t)
}

func TestProcessWithPullFailure(t *testing.T) {
	job := &host.Job{ID: "a", Config: &docker.Config{}}
	err := errors.New("undefined failure")
	client := &fakeDockerClient{createErr: docker.ErrNoSuchImage, pullErr: err}
	testProcessWithError(job, client, err, t)
}

func TestProcessWithStartFailure(t *testing.T) {
	job := &host.Job{ID: "a", Config: &docker.Config{}}
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
			ports:  ports.NewAllocator(500, 550),
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
	if job.ExitCode != 1 {
		t.Error("incorrect exit code")
	}
}
