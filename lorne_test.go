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
	createErr error
	startErr  error
	pullErr   error
	created   *docker.Config
	pulled    string
	started   bool
	hostConf  *docker.HostConfig
}

func (c *dockerClient) CreateContainer(config *docker.Config) (*docker.Container, error) {
	if c.createErr != nil {
		err := c.createErr
		c.createErr = nil
		return nil, err
	}
	c.created = config
	return &docker.Container{ID: "asdf"}, nil
}

func (c *dockerClient) StartContainer(id string, config *docker.HostConfig) error {
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

func (c *dockerClient) PullImage(opts docker.PullImageOptions, w io.Writer) error {
	if c.pullErr != nil {
		return c.pullErr
	}
	c.pulled = opts.Repository
	return nil
}

func testProcess(job *sampi.Job, t *testing.T) (*State, *dockerClient) {
	client := &dockerClient{}
	return testProcessWithOpts(job, "", client, t), client
}

func testProcessWithOpts(job *sampi.Job, extAddr string, client *dockerClient, t *testing.T) *State {
	if client == nil {
		client = &dockerClient{}
	}
	state := processWithOpts(job, extAddr, client)

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

	return state
}

func testProcessWithError(job *sampi.Job, client *dockerClient, err error, t *testing.T) *State {
	state := processWithOpts(job, "", client)

	sjob := state.GetJob(job.ID)
	if sjob.Status != lorne.StatusFailed {
		t.Errorf("expected status failed, got %s", sjob.Status)
	}
	if sjob.Error != err {
		t.Error("error not saved")
	}
	return state
}

func processWithOpts(job *sampi.Job, extAddr string, client *dockerClient) *State {
	jobs := make(chan *sampi.Job)
	done := make(chan struct{})
	ports := make(chan int)
	state := NewState()
	go allocatePorts(ports, 500, 501)
	go func() {
		processJobs(jobs, extAddr, client, state, ports)
		close(done)
	}()
	jobs <- job
	close(jobs)
	<-done
	return state
}

func TestProcessJob(t *testing.T) {
	testProcess(&sampi.Job{ID: "a", Config: &docker.Config{}}, t)
}

func TestProcessJobWithPort(t *testing.T) {
	job := &sampi.Job{TCPPorts: 1, ID: "a", Config: &docker.Config{}}
	_, client := testProcess(job, t)

	if len(job.Config.Env) == 0 || job.Config.Env[len(job.Config.Env)-1] != "PORT=500" {
		t.Error("port env not set")
	}
	if _, ok := job.Config.ExposedPorts["500/tcp"]; !ok {
		t.Error("exposed port not set")
	}
	if b := client.hostConf.PortBindings["500/tcp"]; len(b) == 0 || b[0].HostPort != "500" {
		t.Error("port binding not set")
	}
}

func TestProcessWithExtAddr(t *testing.T) {
	job := &sampi.Job{ID: "a", Config: &docker.Config{}}
	testProcessWithOpts(job, "10.10.10.1", nil, t)

	if !sliceHasString(job.Config.Env, "EXTERNAL_IP=10.10.10.1") {
		t.Error("EXTERNAL_IP not set")
	}
	if !sliceHasString(job.Config.Env, "DISCOVERD=10.10.10.1:1111") {
		t.Error("DISCOVERD not set")
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
	job := &sampi.Job{ID: "a", Config: &docker.Config{Image: "test/foo"}}
	client := &dockerClient{createErr: docker.ErrNoSuchImage}
	testProcessWithOpts(job, "", client, t)

	if client.pulled != "test/foo" {
		t.Error("image not pulled")
	}
}

func TestProcessWithCreateFailure(t *testing.T) {
	job := &sampi.Job{ID: "a", Config: &docker.Config{}}
	err := errors.New("undefined failure")
	client := &dockerClient{createErr: err}
	testProcessWithError(job, client, err, t)
}

func TestProcessWithPullFailure(t *testing.T) {
	job := &sampi.Job{ID: "a", Config: &docker.Config{}}
	err := errors.New("undefined failure")
	client := &dockerClient{createErr: docker.ErrNoSuchImage, pullErr: err}
	testProcessWithError(job, client, err, t)
}

func TestProcessWithStartFailure(t *testing.T) {
	job := &sampi.Job{ID: "a", Config: &docker.Config{}}
	err := errors.New("undefined failure")
	client := &dockerClient{startErr: err}
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
	events := make(chan lorne.Event)
	client := &schedulerSyncClient{}
	done := make(chan struct{})
	go func() {
		syncScheduler(client, events)
		close(done)
	}()

	events <- lorne.Event{Event: "stop", JobID: "a"}
	close(events)
	<-done

	if len(client.removed) != 1 && client.removed[0] != "a" {
		t.Error("job not removed")
	}
}

type streamClient struct {
	events chan *docker.Event
}

func (s *streamClient) Events() (*docker.EventStream, error) {
	return &docker.EventStream{Events: s.events}, nil
}

func (s *streamClient) InspectContainer(id string) (*docker.Container, error) {
	if id != "1" {
		return nil, errors.New("incorrect id")
	}
	return &docker.Container{State: docker.State{ExitCode: 1}}, nil
}

func TestStreamEvents(t *testing.T) {
	events := make(chan *docker.Event)
	state := NewState()
	state.AddJob(&sampi.Job{ID: "a"})
	state.SetContainerID("a", "1")
	state.SetStatusRunning("a")

	done := make(chan struct{})
	go func() {
		streamEvents(&streamClient{events}, state)
		close(done)
	}()

	events <- &docker.Event{Status: "die", ID: "1"}
	close(events)
	<-done

	job := state.GetJob("a")
	if job.Status != lorne.StatusCrashed {
		t.Error("incorrect status")
	}
	if job.ExitCode != 1 {
		t.Error("incorrect exit code")
	}
}
